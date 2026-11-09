package job

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	metaFile   = "meta.json"
	eventsFile = "events.jsonl"
	resultFile = "result.md"
)

type store struct {
	root string
	db   *sql.DB // nil = files only (degraded, no vault)
}

func newStore(root string, db *sql.DB) (*store, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: root directory is required", ErrInvalid)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	if err := EnsureJobRows(db); err != nil {
		return nil, err
	}
	return &store{root: root, db: db}, nil
}

func (s *store) create(meta Meta) (Meta, error) {
	dir := filepath.Join(s.root, meta.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Meta{}, err
	}
	meta.Dir = dir
	meta.ResultPath = filepath.Join(dir, resultFile)
	meta.EventsPath = filepath.Join(dir, eventsFile)
	if err := s.writeMeta(meta); err != nil {
		return Meta{}, err
	}
	f, err := os.OpenFile(meta.EventsPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Meta{}, err
	}
	_ = f.Close()
	return meta, nil
}

func (s *store) writeMeta(meta Meta) error {
	path := filepath.Join(meta.Dir, metaFile)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	// Mirror into the vault's jobs table (Bet 01): meta.json stays the
	// working store; the row is the durable, queryable record.
	return PutJobRow(s.db, meta)
}

// atomicWrite lands data at path via unique-temp + rename: a reader sees
// one whole version or another, never a splice. The temp name is unique
// per write — two concurrent writers sharing one fixed temp path can
// interleave their truncate+write cycles into mixed bytes (valid JSON
// prefix + a stale tail), which reads back as corruption. Unique names
// keep each write atomic; CreateTemp also opens O_EXCL, so the rename is
// always of this write's own bytes. Shared by meta.json and the outbox
// delivery rows.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	// CreateTemp files are 0600; these files were historically
	// 0644-readable.
	if err := os.Chmod(tmpName, 0o644); err != nil { //nolint:gosec // G302: job meta is local tooling state
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func (s *store) readMeta(id string) (Meta, error) {
	path := filepath.Join(s.root, id, metaFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Meta{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return Meta{}, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

func (*store) appendEvent(meta Meta, msg string) error {
	ev := Event{Time: time.Now().UTC(), Message: msg}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(meta.EventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

func (*store) readEvents(meta Meta, limit int) ([]Event, error) {
	data, err := os.ReadFile(meta.EventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]Event, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (*store) writeResult(meta Meta, summary string) error {
	//nolint:gosec // G306: job results are local tooling state
	return os.WriteFile(meta.ResultPath, []byte(summary), 0o644)
}

func (*store) readResult(meta Meta) (string, error) {
	data, err := os.ReadFile(meta.ResultPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (s *store) listIDs() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		// The outbox directory lives alongside job dirs; it is
		// delivery state, not a job.
		if e.IsDir() && e.Name() != outboxDirName {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// remove deletes a job dir outright — used when a spawn is rejected after
// the dir was created (queue overflow), so no orphaned non-terminal meta
// survives on disk for Wait to trip over. The vault row goes with it.
func (s *store) remove(id string) error {
	if err := DeleteJobRow(s.db, id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.root, id))
}

func newJobID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("job_%s_%s", time.Now().UTC().Format("20060102T150405"), hex.EncodeToString(b[:])), nil
}
