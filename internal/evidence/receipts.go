package evidence

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/vault"
)

// jsonlReceipts is the original file backing: one JSON line per receipt,
// appended to evidence.jsonl. The file is closed between writes so a
// prune rewrite (temp-file + rename) never races an open handle.
type jsonlReceipts struct {
	path string
}

func (j jsonlReceipts) Append(line []byte) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (j jsonlReceipts) LoadAll() ([][]byte, error) {
	f, err := os.Open(j.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		lines = append(lines, append([]byte(nil), raw...))
	}
	return lines, sc.Err()
}

func (j jsonlReceipts) ReplaceAll(lines [][]byte) error {
	var buf bytes.Buffer
	for _, line := range lines {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, j.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// vaultReceipts is the Bet 08 backing: receipt lines as rows in the one
// durable store, keyed by the project scope. Same append/load/replace
// contract; the ratchet and prune logic above are unchanged.
type vaultReceipts struct {
	v     *vault.Vault
	scope string
}

func (r vaultReceipts) Append(line []byte) error {
	return r.v.AppendEvidenceReceipt(r.scope, strings.TrimSpace(string(line)))
}

func (r vaultReceipts) LoadAll() ([][]byte, error) {
	payloads, err := r.v.LoadEvidenceReceipts(r.scope)
	if err != nil {
		return nil, err
	}
	lines := make([][]byte, 0, len(payloads))
	for _, p := range payloads {
		lines = append(lines, []byte(p))
	}
	return lines, nil
}

func (r vaultReceipts) ReplaceAll(lines [][]byte) error {
	payloads := make([]string, 0, len(lines))
	for _, line := range lines {
		payloads = append(payloads, strings.TrimSpace(string(line)))
	}
	return r.v.ReplaceEvidenceReceipts(r.scope, payloads)
}
