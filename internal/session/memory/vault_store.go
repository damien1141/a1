package memory

import (
	"fmt"
	"time"

	"github.com/damien1141/a1/internal/vault"
)

// vaultStore is the Bet 08 row backing: the bank's entries live in the
// one durable store's memory_banks table instead of a JSONL file. The
// API and semantics (index handles, lazy load, delete-rewrite) are
// identical to the file backing — only the address changes.
type vaultStore struct {
	v    *vault.Vault
	bank string
}

// OpenVaultBank opens (creating if needed) a bank stored as rows in the
// vault. bank is the namespace key ("persona:<id>" for persona banks,
// the raw session id for session banks).
func OpenVaultBank(bank string, v *vault.Vault) (*Bank, error) {
	if v == nil {
		return nil, fmt.Errorf("memory: vault is required")
	}
	if bank == "" {
		return nil, fmt.Errorf("memory: bank key is required")
	}
	return &Bank{store: vaultStore{v: v, bank: bank}}, nil
}

func (s vaultStore) ID() string        { return s.bank }
func (s vaultStore) PathLabel() string { return "vault:" + s.bank }

func (s vaultStore) Append(entry Entry) error {
	return s.v.AppendBankEntry(s.bank, vault.BankEntry{
		Type:      string(entry.Type),
		Content:   entry.Content,
		Timestamp: entry.Timestamp,
		SessionID: entry.SessionID,
	})
}

func (s vaultStore) Load() ([]Entry, error) {
	rows, err := s.v.LoadBank(s.bank)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, Entry{
			SessionID: r.SessionID,
			Timestamp: r.Timestamp,
			Type:      EntryType(r.Type),
			Content:   r.Content,
		})
	}
	return out, nil
}

func (s vaultStore) ReplaceAll(entries []Entry) error {
	rows := make([]vault.BankEntry, 0, len(entries))
	for _, e := range entries {
		ts := e.Timestamp
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		rows = append(rows, vault.BankEntry{
			Type:      string(e.Type),
			Content:   e.Content,
			Timestamp: ts,
			SessionID: e.SessionID,
		})
	}
	return s.v.ReplaceBank(s.bank, rows)
}
