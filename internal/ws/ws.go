// Package ws is the websocket half of the chat bridges: a hand-rolled
// RFC 6455 client. gorilla/websocket et al. are fine libraries, but the
// bridges need exactly one role — a client speaking TEXT frames over TLS
// to stoat's event stream and Discord's gateway — and the dependency
// budget is spent on purpose elsewhere (the roadmap note that locked it:
// "keep the dependency budget at zero new deps if the protocol is plain
// HTTP/WS").
//
// The subset spoken, stated plainly: client role only (outgoing frames
// are masked per RFC 6455 §5.3), text frames only (binary is refused —
// both event protocols are JSON, and msgpack is opt-in and unwanted),
// fragmentation assembled (a sender may split one message across
// frames), ping answered with pong, close surfaced as a CloseError. No
// compression, no subprotocol negotiation, no extensions: a server
// setting RSV bits without negotiation is a protocol violation and the
// connection is refused, not guessed at.
//
// Liveness is two-layered by design: the caller's heartbeat (stoat Ping,
// Discord op-1) keeps the server talking, and this package enforces a
// hard idle deadline — no frame from the peer within IdleTimeout tears
// the connection down with an error, because a silently-dead TCP
// connection is the one failure a write can never reveal.
package ws

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GUID is the RFC 6455 handshake constant every accept key is salted
// with. Exported for the server-side fakes in test files elsewhere.
const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// ErrClosed is the terminal error for a connection closed by the local
// side (Close). A server-initiated close surfaces as *CloseError instead.
var ErrClosed = errors.New("ws: connection closed")

// ErrSlowConsumer is the terminal error when the caller stops calling
// Receive while the peer keeps sending: the reader may not block (it
// would stop answering pings), so a full buffer tears the connection
// down loudly instead of stalling quietly.
var ErrSlowConsumer = errors.New("ws: undelivered messages exceeded the buffer")

// writeTimeout bounds one outgoing frame. A websocket send is a
// hand-full of bytes; thirty seconds means the TCP connection is gone.
const writeTimeout = 30 * time.Second

// Opcodes (RFC 6455 §5.2).
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// Options configures [Dial]. The zero value is the production default.
type Options struct {
	// TLSClientConfig configures wss:// connections (nil = system roots).
	// Tests use InsecureSkipVerify against their throwaway certificates.
	TLSClientConfig *tls.Config
	// IdleTimeout bounds the gap between received frames before the
	// connection is torn down as dead. Zero = 90s, which sits well above
	// both event protocols' heartbeat intervals (stoat ~20s, Discord
	// ~41s) while still failing inside two missed rounds.
	IdleTimeout time.Duration
	// MaxMessageBytes bounds one assembled message. Zero = 16 MiB —
	// Discord's READY payload scales with guild count and a bridge bot
	// in a handful of servers stays far below it.
	MaxMessageBytes int64
}

// Conn is one established websocket connection. Safe for concurrent use:
// frames are written under a mutex (Send, the automatic pong, and the
// close handshake all share it), and messages are delivered to exactly
// one Receive caller.
type Conn struct {
	conn net.Conn
	br   *bufio.Reader

	// wmu serializes frame writes.
	wmu sync.Mutex

	// msgc carries complete text messages to the Receive caller. The
	// reader goroutine never blocks on it: a full buffer is a dead
	// caller, and the connection is torn down (ErrSlowConsumer) rather
	// than letting pings go unanswered.
	msgc chan []byte

	// done is closed exactly once, when the connection is terminal;
	// err holds the reason (written before the close, read after).
	done     chan struct{}
	doneOnce sync.Once
	err      atomic.Value // error

	idle   time.Duration
	maxMsg int64
}

// Dial opens a websocket connection to rawURL (ws:// or wss://) and
// performs the RFC 6455 opening handshake. header carries additional
// request headers (none of the handshake's own keys — those are set
// here and cannot be overridden). The returned Conn is live: the reader
// goroutine is running, and the first Receive call is already able to
// return a message.
func Dial(ctx context.Context, rawURL string, header http.Header, opts *Options) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ws: parse url: %w", err)
	}
	var secure bool
	switch u.Scheme {
	case "ws":
	case "wss":
		secure = true
	default:
		return nil, fmt.Errorf("ws: url scheme must be ws:// or wss://, got %q", u.Scheme)
	}

	if opts == nil {
		opts = &Options{}
	}
	idle := opts.IdleTimeout
	if idle <= 0 {
		idle = 90 * time.Second
	}
	maxMsg := opts.MaxMessageBytes
	if maxMsg <= 0 {
		maxMsg = 16 << 20
	}

	host := u.Host
	if u.Port() == "" {
		if secure {
			host = net.JoinHostPort(host, "443")
		} else {
			host = net.JoinHostPort(host, "80")
		}
	}

	var netConn net.Conn
	d := &net.Dialer{}
	if secure {
		cfg := opts.TLSClientConfig
		if cfg == nil {
			cfg = &tls.Config{}
		}
		if cfg.ServerName == "" {
			c := cfg.Clone()
			c.ServerName = u.Hostname()
			cfg = c
		}
		td := &tls.Dialer{NetDialer: d, Config: cfg}
		netConn, err = td.DialContext(ctx, "tcp", host)
	} else {
		netConn, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("ws: dial %s: %w", host, err)
	}

	br := bufio.NewReader(netConn)
	if err := handshake(ctx, netConn, br, u, header); err != nil {
		_ = netConn.Close()
		return nil, err
	}

	c := &Conn{
		conn:   netConn,
		br:     br,
		msgc:   make(chan []byte, 64),
		done:   make(chan struct{}),
		idle:   idle,
		maxMsg: maxMsg,
	}
	go c.pump()
	return c, nil
}

// handshake writes the upgrade request and validates the response. On
// failure the connection is left for the caller to close. br wraps the
// same connection the frames will flow over — early frames the server
// sends behind the handshake response must not be thrown away with a
// second reader.
func handshake(ctx context.Context, conn net.Conn, br *bufio.Reader, u *url.URL, header http.Header) error {
	// The request must finish inside the dial context; the connection
	// deadline covers the round trip, then is released to the frame path.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	_ = conn.SetDeadline(deadline)
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		return fmt.Errorf("ws: handshake key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes[:])

	uri := u.RequestURI()
	if uri == "" {
		uri = "/"
	}
	var req bytes.Buffer
	fmt.Fprintf(&req, "GET %s HTTP/1.1\r\n", uri)
	fmt.Fprintf(&req, "Host: %s\r\n", u.Host)
	req.WriteString("Upgrade: websocket\r\n")
	req.WriteString("Connection: Upgrade\r\n")
	fmt.Fprintf(&req, "Sec-WebSocket-Key: %s\r\n", key)
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	for k, vs := range header {
		if isHandshakeHeader(k) {
			continue
		}
		for _, v := range vs {
			fmt.Fprintf(&req, "%s: %s\r\n", k, v)
		}
	}
	req.WriteString("\r\n")
	if _, err := conn.Write(req.Bytes()); err != nil {
		return fmt.Errorf("ws: write handshake: %w", err)
	}

	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return fmt.Errorf("ws: read handshake response: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("ws: handshake: unexpected status %d (%s)", resp.StatusCode, resp.Status)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		return errors.New("ws: handshake: response is not a websocket upgrade")
	}
	sum := sha1.Sum([]byte(key + GUID))
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != base64.StdEncoding.EncodeToString(sum[:]) {
		return errors.New("ws: handshake: Sec-WebSocket-Accept mismatch")
	}
	return nil
}

// isHandshakeHeader reports whether the key is one the handshake owns.
// Callers may add their own headers; they may not rewrite the protocol.
func isHandshakeHeader(k string) bool {
	switch strings.ToLower(k) {
	case "host", "upgrade", "connection", "sec-websocket-key", "sec-websocket-version", "sec-websocket-accept":
		return true
	}
	return false
}

// Send writes one text message as a single masked frame. It is an error
// to send on a terminal connection (the terminal error is returned).
func (c *Conn) Send(b []byte) error {
	if err := c.terminal(); err != nil {
		return err
	}
	if int64(len(b)) > c.maxMsg {
		return fmt.Errorf("ws: send: message of %d bytes exceeds the %d byte cap", len(b), c.maxMsg)
	}
	return c.writeFrame(opText, b)
}

// Receive returns the next complete text message. It returns when a
// message arrives, when ctx is done, or when the connection is terminal
// (close frame, read error, idle deadline, local Close — the reason is
// the error). A connection torn down may drop messages that were
// received but not yet delivered; callers that care drain before closing.
func (c *Conn) Receive(ctx context.Context) ([]byte, error) {
	if err := c.terminal(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.terminalError()
	case b := <-c.msgc:
		return b, nil
	}
}

// Close performs a best-effort close handshake (one close frame, normal
// closure) and tears the connection down. Receiving and sending on a
// closed Conn return ErrClosed. Close is idempotent.
func (c *Conn) Close() error {
	if c.terminal() != nil {
		return nil // already terminal; Close is idempotent
	}
	// Best effort: the close frame, then teardown. Waiting for the
	// server's echoing close frame buys nothing a bridge needs.
	c.wmu.Lock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, _ = c.conn.Write(maskedControl(opClose, 1000, "bye"))
	c.wmu.Unlock()
	c.fail(ErrClosed)
	return nil
}

// terminal returns the terminal error, or nil while the connection is
// live.
func (c *Conn) terminal() error {
	select {
	case <-c.done:
		return c.terminalError()
	default:
		return nil
	}
}

func (c *Conn) terminalError() error {
	if err, ok := c.err.Load().(error); ok && err != nil {
		return err
	}
	return ErrClosed
}

// fail makes the connection terminal with err (first call wins) and
// closes the socket so the reader goroutine cannot hang on a dead peer.
func (c *Conn) fail(err error) {
	c.doneOnce.Do(func() {
		c.err.Store(err)
		close(c.done)
		_ = c.conn.Close()
	})
}

// ---- the reader goroutine ----

// frame is one parsed websocket frame.
type frame struct {
	fin     bool
	opcode  byte
	payload []byte
}

// pump reads frames until the connection dies. It runs for the lifetime
// of the Conn: answering pings cannot wait on the caller's Receive loop.
func (c *Conn) pump() {
	var pending []byte
	inMessage := false
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.idle))
		f, err := readFrame(c.br)
		if err != nil {
			c.fail(fmt.Errorf("ws: read: %w", err))
			return
		}
		switch f.opcode {
		case opText:
			if inMessage {
				c.fail(errors.New("ws: read: new text frame inside an unfinished message"))
				return
			}
			if int64(len(f.payload)) > c.maxMsg {
				c.fail(fmt.Errorf("ws: read: message of %d bytes exceeds the %d byte cap", len(f.payload), c.maxMsg))
				return
			}
			if f.fin {
				c.deliver(f.payload)
			} else {
				pending = append([]byte(nil), f.payload...)
				inMessage = true
			}
		case opContinuation:
			if !inMessage {
				c.fail(errors.New("ws: read: continuation frame with no message in progress"))
				return
			}
			pending = append(pending, f.payload...)
			if int64(len(pending)) > c.maxMsg {
				c.fail(fmt.Errorf("ws: read: assembled message exceeds the %d byte cap", c.maxMsg))
				return
			}
			if f.fin {
				inMessage = false
				msg := pending
				pending = nil
				c.deliver(msg)
			}
		case opBinary:
			c.fail(errors.New("ws: read: binary frames are not spoken here"))
			return
		case opPing:
			// The pong must leave even while the caller is busy — the
			// peer's liveness judgment depends on it.
			if err := c.writeFrame(opPong, f.payload); err != nil {
				c.fail(fmt.Errorf("ws: send pong: %w", err))
				return
			}
		case opPong:
			// Liveness implied; nothing to surface.
		case opClose:
			code, reason := parseClose(f.payload)
			c.wmu.Lock()
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			_, _ = c.conn.Write(maskedControl(opClose, code, ""))
			c.wmu.Unlock()
			c.fail(&CloseError{Code: code, Reason: reason})
			return
		default:
			c.fail(fmt.Errorf("ws: read: unknown opcode 0x%x", f.opcode))
			return
		}
	}
}

// deliver hands one complete message to the Receive caller. A full
// buffer means the caller is gone or stuck: tear down loudly.
func (c *Conn) deliver(b []byte) {
	select {
	case c.msgc <- b:
	case <-c.done:
	default:
		c.fail(ErrSlowConsumer)
	}
}

// ---- frame I/O ----

// writeFrame writes one masked client data frame (RFC 6455 §5.3: client
// frames MUST be masked).
func (c *Conn) writeFrame(opcode byte, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := c.terminal(); err != nil {
		return err
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))

	var hdr [14]byte
	hdr[0] = 0x80 | opcode // FIN: data sends are never fragmented
	n := len(payload)
	pos := 2
	switch {
	case n < 126:
		hdr[1] = 0x80 | byte(n)
	case n <= 0xFFFF:
		hdr[1] = 0x80 | 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(n))
		pos = 4
	default:
		hdr[1] = 0x80 | 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(n))
		pos = 10
	}
	var key [4]byte
	if _, err := rand.Read(key[:]); err != nil {
		return fmt.Errorf("ws: mask key: %w", err)
	}
	copy(hdr[pos:pos+4], key[:])

	masked := make([]byte, n)
	for i, b := range payload {
		masked[i] = b ^ key[i%4]
	}
	if _, err := c.conn.Write(hdr[:pos+4]); err != nil {
		return fmt.Errorf("ws: write frame header: %w", err)
	}
	if _, err := c.conn.Write(masked); err != nil {
		return fmt.Errorf("ws: write frame payload: %w", err)
	}
	return nil
}

// maxServerFrameBytes bounds one server frame on the wire (assembled
// messages are bounded separately by Options.MaxMessageBytes).
const maxServerFrameBytes = 16 << 20

// readFrame reads one frame. Server frames must be unmasked (RFC 6455
// §5.1); a masked server frame is a protocol violation, not a puzzle.
func readFrame(r *bufio.Reader) (frame, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return frame{}, err
	}
	if rsv := h[0] & 0x70; rsv != 0 {
		return frame{}, fmt.Errorf("rsv bits set (0x%x) — no extension was negotiated", rsv)
	}
	f := frame{fin: h[0]&0x80 != 0, opcode: h[0] & 0x0F}
	masked := h[1]&0x80 != 0
	length := int64(h[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return frame{}, err
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return frame{}, err
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
		if length < 0 {
			return frame{}, errors.New("64-bit length with the top bit set")
		}
	}
	control := f.opcode&0x8 != 0
	if control && (!f.fin || length > 125) {
		return frame{}, errors.New("control frame must be final and ≤125 bytes")
	}
	if masked {
		return frame{}, errors.New("server frames must not be masked")
	}
	if length > maxServerFrameBytes {
		return frame{}, fmt.Errorf("frame of %d bytes exceeds the %d byte cap", length, maxServerFrameBytes)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return frame{}, err
	}
	f.payload = payload
	return f, nil
}

// CloseError is a server-initiated websocket close: the status code and
// the (possibly empty) reason.
type CloseError struct {
	Code   uint16
	Reason string
}

func (e *CloseError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("ws: closed by server: status %d", e.Code)
	}
	return fmt.Sprintf("ws: closed by server: status %d: %s", e.Code, e.Reason)
}

// parseClose splits a close frame payload into code + reason. A
// truncated payload degrades to code 1005 (no status) rather than
// guessing.
func parseClose(p []byte) (uint16, string) {
	if len(p) < 2 {
		return 1005, ""
	}
	code := binary.BigEndian.Uint16(p[:2])
	reason := string(p[2:])
	if len(reason) > 123 {
		reason = reason[:123]
	}
	return code, reason
}

// maskedControl builds one masked control frame (close replies and the
// local close handshake — control frames are ≤125 bytes, so the 7-bit
// length form is the only one). Callers hold wmu and the write deadline.
func maskedControl(opcode byte, code uint16, reason string) []byte {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload[:2], code)
	copy(payload[2:], reason)
	var hdr [6]byte
	hdr[0] = 0x80 | opcode
	hdr[1] = 0x80 | byte(len(payload))
	var key [4]byte
	_, _ = rand.Read(key[:])
	copy(hdr[2:6], key[:])
	out := make([]byte, 0, 6+len(payload))
	out = append(out, hdr[:6]...)
	for i, b := range payload {
		out = append(out, b^key[i%4])
	}
	return out
}
