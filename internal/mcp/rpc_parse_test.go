package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHTTPOrSSEBodySkipsNotifications(t *testing.T) {
	// A server may stream a log notification (no id, method set) before the
	// JSON-RPC response frame. The response frame must win.
	var b strings.Builder
	b.WriteString("event: message\n")
	b.WriteString("data: " + `{"jsonrpc":"2.0","method":"notifications/message",` +
		`"params":{"level":"info","data":"Search successful"}}` + "\n\n")
	b.WriteString("event: message\n")
	b.WriteString("data: " + `{"jsonrpc":"2.0","id":3,"result":` +
		`{"content":[{"type":"text","text":"ok"}]}}` + "\n\n")

	rpc, err := parseHTTPOrSSEBody([]byte(b.String()))
	require.NoError(t, err)
	require.Empty(t, rpc.Method)
	require.NotNil(t, rpc.ID)

	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(rpc.Result, &res))
	require.Len(t, res.Content, 1)
	require.Equal(t, "ok", res.Content[0].Text)
}

func TestParseHTTPOrSSEBodySkipsNotificationsForErrorResponse(t *testing.T) {
	// Same interleaving, but the response frame carries an error: it must
	// still win over the earlier notification.
	body := []byte(
		"data: " + `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}` + "\n\n" +
			"data: " + `{"jsonrpc":"2.0","id":7,"error":{"code":-32601,"message":"no such tool"}}` + "\n\n")

	rpc, err := parseHTTPOrSSEBody(body)
	require.NoError(t, err)
	require.Empty(t, rpc.Method)
	require.NotNil(t, rpc.ID)
	require.Equal(t, -32601, rpc.Error.Code)
	require.Equal(t, "no such tool", rpc.Error.Message)
}

func TestParseHTTPOrSSEBodyFirstFrameKeptWhenNoResponse(t *testing.T) {
	// Only notifications in the body: keep the first parseable frame
	// (previous behavior) rather than erroring out.
	body := []byte("data: " +
		`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}` + "\n\n")

	rpc, err := parseHTTPOrSSEBody(body)
	require.NoError(t, err)
	require.Equal(t, "notifications/message", rpc.Method)
}

func TestParseHTTPOrSSEBodyPlainJSON(t *testing.T) {
	rpc, err := parseHTTPOrSSEBody([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	require.NoError(t, err)
	require.NotNil(t, rpc.ID)
	require.Empty(t, rpc.Method)
}
