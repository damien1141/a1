package memstore

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // driver name "sqlite"; FTS5 included; pure Go
)

// ---------- embeddings ----------

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
	Dim() int
}

// FakeEmbedder — deterministic token-hash vectors for network-free tests.
type FakeEmbedder struct{ DimCount int }

func (f FakeEmbedder) Model() string { return "fake-v1" }
func (f FakeEmbedder) Dim() int      { return f.DimCount }
func (f FakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	d := f.DimCount
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, d)
		for _, tok := range strings.Fields(strings.ToLower(t)) {
			h := uint32(2166136261)
			for _, c := range tok {
				h = (h ^ uint32(c)) * 16777619
			}
			v[h%uint32(d)] += 1
			v[(h>>8)%uint32(d)] += 0.5
		}
		out[i] = Normalize(v)
	}
	return out, nil
}

func Normalize(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	if s == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(s))
	for i := range v {
		v[i] *= inv
	}
	return v
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func dot(a, b []float32) float64 {
	var s float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// ---------- types ----------

type EncodingCtx struct {
	Project string  `json:"project"`
	Tod     float64 `json:"tod"`
	Valence float64 `json:"valence"`
}

type Trace struct {
	ID         int64
	Content    string
	Kind       string
	Scope      string
	Project    sql.NullString
	Thread     sql.NullString
	SessionID  sql.NullInt64
	Valence    sql.NullFloat64
	Salience   float64
	CoolPasses int
	EmbedModel string
	Embed      []float32
	Verbatim   sql.NullString
	Ctx        EncodingCtx
	CreatedAt  time.Time
	Rehearsals []time.Time // includes encode time
}

// ScoredTrace carries the A-components — the input to the §11 calibration
// loop (log every injection; label felt-right/intrusive; regress on these).
type ScoredTrace struct {
	Trace
	B    float64
	Cos  float64
	CtxM float64
	Kwd  bool
	A    float64
	P    float64
}

type Mode int

const (
	ModeCompanion Mode = iota
	ModeProject
)

// scopeWhere — §3.7, hard isolation. The one WHERE that must never leak.
func scopeWhere(m Mode, gitRoot string) (string, []any) {
	if m == ModeProject {
		return `status='active'
                  AND (scope='global' OR (scope='project' AND project_path=?))
                  AND kind NOT IN ('identity','beat','affect','bit')`, []any{gitRoot}
	}
	return `status='active'
          AND (scope='user' OR kind IN ('identity','beat','affect','bit'))`, nil
}

// ---------- store ----------

type Store struct {
	db     *sql.DB
	ownsDB bool // Open (file) owns the handle; OpenIn (vault) does not
	ns     string
	emb    Embedder
	params Params
	Merge  func(old, nw string) string
	writes chan writeOp
	done   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

// writeOp is one serialized unit of work for the writer goroutine. res is nil
// for fire-and-forget writes (rehearsal ticks).
type writeOp struct {
	fn  func(tx *sql.Tx) error
	res chan error
}

func Open(path string, emb Embedder, p Params) (*Store, error) {
	db, err := sql.Open("sqlite",
		"file:"+filepath.Clean(path)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{
		db: db, ownsDB: true, emb: emb, params: p,
		writes: make(chan writeOp, 256),
		done:   make(chan struct{}),
		Merge:  unionSentences,
	}
	s.wg.Add(2)
	go s.writer()
	go s.decayLoop()
	return s, nil
}

// OpenIn opens a namespaced store inside an existing database handle (the
// vault, Bet 08): table family mem_<ns>_*, isolated per namespace, one
// SQLite file for every persona plus the shared engine bank. The caller
// owns the *sql.DB — Close stops this store's writer but never closes the
// shared handle.
func OpenIn(db *sql.DB, ns string, emb Embedder, p Params) (*Store, error) {
	if db == nil {
		return nil, errors.New("memstore: OpenIn requires a db handle")
	}
	if ns == "" {
		return nil, errors.New("memstore: OpenIn requires a namespace")
	}
	if _, err := db.Exec(SchemaFor(ns)); err != nil {
		return nil, err
	}
	s := &Store{
		db: db, ns: ns, emb: emb, params: p,
		writes: make(chan writeOp, 256),
		done:   make(chan struct{}),
		Merge:  unionSentences,
	}
	s.wg.Add(2)
	go s.writer()
	go s.decayLoop()
	return s, nil
}

// table returns the namespaced name for one of the store's tables
// ("memories", "rehearsals", "corrections", "memories_fts").
func (s *Store) table(name string) string {
	if s.ns == "" {
		return name
	}
	return "mem_" + s.ns + "_" + name
}

func (s *Store) writer() {
	defer s.wg.Done()
	for op := range s.writes {
		// The ack must land AFTER Commit, not after the statements run:
		// readers (Retrieve, Insert's dedupe scan) run on other pooled
		// connections, and a WAL reader cannot see an uncommitted row.
		// Acking inside op.fn let a reader scheduled between "statements
		// done" and "committed" miss the write entirely — the parallel
		// test runs hit exactly that window.
		tx, err := s.db.Begin()
		if err != nil {
			op.ack(err)
			continue
		}
		if ferr := op.fn(tx); ferr != nil {
			_ = tx.Rollback()
			op.ack(ferr)
		} else {
			op.ack(tx.Commit())
		}
	}
}

func (op writeOp) ack(err error) {
	if op.res != nil {
		op.res <- err
	}
}

// decayInterval is how often the background decay pass runs (Bet 06):
// unrehearsed memories gain a cool_pass each interval, driving the
// salience-cooling term that was dead code before.
const decayInterval = 24 * time.Hour

// decayLoop runs the periodic cool_passes increment. Failures are
// swallowed — a missed pass only means salience cools one interval
// later.
func (s *Store) decayLoop() {
	defer s.wg.Done()
	t := time.NewTicker(decayInterval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			cutoff := time.Now().UTC().Add(-decayInterval).Format(time.RFC3339)
			_ = s.Decay(cutoff)
		}
	}
}

func (s *Store) doWrite(fn func(*sql.Tx) error) error {
	op := writeOp{fn: fn, res: make(chan error, 1)}
	s.writes <- op
	return <-op.res
}

func (s *Store) Close() error {
	s.once.Do(func() {
		close(s.writes)
		close(s.done)
	})
	s.wg.Wait()
	if !s.ownsDB {
		return nil // the vault owns the handle
	}
	return s.db.Close()
}

// ---------- retrieval (§3) ----------

func (s *Store) Retrieve(ctx context.Context, query string, mode Mode,
	cues Cues, now time.Time,
) ([]ScoredTrace, error) {
	vecs, err := s.emb.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	q := Normalize(vecs[0])
	where, args := scopeWhere(mode, cues.Project)

	ftsIDs, err := s.ftsLeg(query)
	if err != nil {
		return nil, err
	}
	vecIDs, err := s.vecLeg(where, args, q)
	if err != nil {
		return nil, err
	}
	pool := rrfFuse(s.params.M, ftsIDs, vecIDs)
	traces, err := s.loadTraces(where, args, pool)
	if err != nil {
		return nil, err
	}

	ftsSet := make(map[int64]bool, len(ftsIDs))
	for _, id := range ftsIDs {
		ftsSet[id] = true
	}
	scored := make([]ScoredTrace, 0, len(traces))
	for _, t := range traces {
		a := Activation(s.params, t, q, cues, ftsSet[t.ID], now)
		scored = append(scored, ScoredTrace{
			Trace: *t,
			B:     BaseActivation(s.params, t.Rehearsals, now),
			Cos:   dot(q, t.Embed),
			CtxM:  ContextMatch(cues, t.Ctx),
			Kwd:   ftsSet[t.ID],
			A:     a,
			P:     Probability(a, s.params.Tau),
		})
	}
	sel := s.selectBundle(scored, cues.Valence)
	s.recordRehearsals(idsOf(sel))
	return sel, nil
}

var ftsTok = regexp.MustCompile(`[A-Za-z0-9_]{2,}`)

func ftsQueryString(q string) string {
	seen := map[string]bool{}
	var parts []string
	for _, t := range ftsTok.FindAllString(strings.ToLower(q), -1) {
		if !seen[t] {
			seen[t] = true
			parts = append(parts, `"`+t+`"`)
		}
	}
	return strings.Join(parts, " OR ")
}

func (s *Store) ftsLeg(query string) ([]int64, error) {
	qs := ftsQueryString(query)
	if qs == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		fmt.Sprintf(
			`SELECT rowid FROM %s WHERE %s MATCH ? ORDER BY rank LIMIT ?`,
			s.table("memories_fts"),
			s.table("memories_fts"),
		),
		qs,
		s.params.FtsTop,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) vecLeg(where string, args []any, q []float32) ([]int64, error) {
	rows, err := s.db.Query(
		fmt.Sprintf(`SELECT id, embed_model, embedding FROM %s WHERE `, s.table("memories"))+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type hit struct {
		id int64
		d  float64
	}
	var (
		hits  []hit
		stale []int64
	)
	for rows.Next() {
		var id int64
		var model string
		var blob []byte
		if err := rows.Scan(&id, &model, &blob); err != nil {
			return nil, err
		}
		hits = append(hits, hit{id, dot(q, decodeVec(blob))})
		if model != s.emb.Model() {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].d != hits[j].d {
			return hits[i].d > hits[j].d
		}
		return hits[i].id < hits[j].id
	})
	n := s.params.VecTop
	if n > len(hits) {
		n = len(hits)
	}
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = hits[i].id
	}
	if len(stale) > 0 {
		go s.reembedStale(stale)
	}
	return ids, nil
}

func (s *Store) reembedStale(ids []int64) {
	ctx := context.Background()
	for _, id := range ids {
		var content string
		if err := s.db.QueryRow(
			fmt.Sprintf(`SELECT content FROM %s WHERE id=?`, s.table("memories")), id).Scan(&content); err != nil {
			continue
		}
		vs, err := s.emb.Embed(ctx, []string{content})
		if err != nil {
			continue
		}
		v := Normalize(vs[0])
		_ = s.doWrite(func(tx *sql.Tx) error {
			if tx == nil {
				return errors.New("memstore: tx unavailable")
			}
			_, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET embedding=?, embed_model=? WHERE id=?`, s.table("memories")),
				encodeVec(v), s.emb.Model(), id)
			return err
		})
	}
}

func rrfFuse(M int, legs ...[]int64) []int64 {
	score := map[int64]float64{}
	for _, leg := range legs {
		for r, id := range leg {
			score[id] += 1.0 / float64(60+r+1)
		}
	}
	ids := make([]int64, 0, len(score))
	for id := range score {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if score[ids[i]] != score[ids[j]] {
			return score[ids[i]] > score[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if len(ids) > M {
		ids = ids[:M]
	}
	return ids
}

func (s *Store) loadTraces(where string, args []any, ids []int64) ([]*Trace, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := make([]string, len(ids))
	va := make([]any, 0, len(ids)+len(args))
	for i, id := range ids {
		ph[i] = "?"
		va = append(va, id)
	}
	va = append(va, args...)
	rows, err := s.db.Query(fmt.Sprintf(`SELECT id, content, kind, scope, project_path, thread,
                session_id, valence, salience, cool_passes, embed_model, embedding,
                verbatim_ref, encoding_ctx, created_at
                FROM %s WHERE id IN (`, s.table("memories"))+strings.Join(ph, ",")+`) AND `+where, va...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Trace
	for rows.Next() {
		t := &Trace{}
		var ctxJSON, created string
		var emb []byte
		if err := rows.Scan(&t.ID, &t.Content, &t.Kind, &t.Scope, &t.Project,
			&t.Thread, &t.SessionID, &t.Valence, &t.Salience, &t.CoolPasses,
			&t.EmbedModel, &emb, &t.Verbatim, &ctxJSON, &created); err != nil {
			return nil, err
		}
		t.Embed = decodeVec(emb)
		_ = json.Unmarshal([]byte(ctxJSON), &t.Ctx)
		if t.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// rehearsals
	if len(out) > 0 {
		rph := make([]string, len(out))
		rva := make([]any, len(out))
		for i, t := range out {
			rph[i] = "?"
			rva[i] = t.ID
		}
		rrows, err := s.db.Query(fmt.Sprintf(`SELECT memory_id, at FROM %s
                WHERE memory_id IN (`, s.table("rehearsals"))+strings.Join(rph, ",")+`)`, rva...)
		if err != nil {
			return nil, err
		}
		defer rrows.Close()
		byID := map[int64]*Trace{}
		for _, t := range out {
			byID[t.ID] = t
		}
		for rrows.Next() {
			var id int64
			var at string
			if err := rrows.Scan(&id, &at); err != nil {
				return nil, err
			}
			if ts, err := time.Parse(time.RFC3339, at); err == nil {
				if t := byID[id]; t != nil {
					t.Rehearsals = append(t.Rehearsals, ts)
				}
			}
		}
	}
	return out, nil
}

func (s *Store) selectBundle(scored []ScoredTrace, sessionVal float64) []ScoredTrace {
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].A != scored[j].A {
			return scored[i].A > scored[j].A
		}
		return scored[i].ID < scored[j].ID
	})
	out := make([]ScoredTrace, 0, s.params.K)
	for i := range scored {
		if scored[i].A <= s.params.Tau {
			break
		}
		if maxCosTo(scored[i].Embed, out) >= s.params.Diversity {
			continue
		}
		out = append(out, scored[i])
		if len(out) == s.params.K {
			break
		}
	}
	// Mood-congruency override (§3.5 step 4)
	if sessionVal < -0.3 {
		need := (s.params.K + 2) / 3
		pos := func(t ScoredTrace) bool {
			return t.Valence.Valid && t.Valence.Float64 > 0.2
		}
		have := 0
		for _, t := range out {
			if pos(t) {
				have++
			}
		}
		inOut := func(id int64) bool {
			for _, t := range out {
				if t.ID == id {
					return true
				}
			}
			return false
		}
		for have < need {
			var cand *ScoredTrace
			for i := range scored {
				if pos(scored[i]) && !inOut(scored[i].ID) {
					cand = &scored[i]
					break
				}
			}
			if cand == nil {
				break
			}
			// Replace the LAST non-positive slot: out is in descending
			// activation order, so its final non-positive member is the
			// weakest one — the override must cost the bundle's weakest
			// slot, not displace the top scorer at rank 0.
			replaced := false
			for i := len(out) - 1; i >= 0; i-- {
				if !pos(out[i]) {
					out[i] = *cand
					replaced = true
					break
				}
			}
			if !replaced {
				out = append(out, *cand)
			}
			have++
		}
	}
	return out
}

func maxCosTo(v []float32, bundle []ScoredTrace) float64 {
	m := 0.0
	for i := range bundle {
		if c := dot(v, bundle[i].Embed); c > m {
			m = c
		}
	}
	return m
}

func idsOf(ts []ScoredTrace) []int64 {
	ids := make([]int64, len(ts))
	for i := range ts {
		ids[i] = ts[i].ID
	}
	return ids
}

func (s *Store) recordRehearsals(ids []int64) {
	if len(ids) == 0 {
		return
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	fn := func(tx *sql.Tx) error {
		if tx == nil {
			return nil
		}
		for _, id := range ids {
			if _, err := tx.Exec(
				fmt.Sprintf(`INSERT OR IGNORE INTO %s(memory_id, at) VALUES(?,?)`, s.table("rehearsals")), id, ts); err != nil {
				return err
			}
		}
		return nil
	}
	select {
	case s.writes <- writeOp{fn: fn}:
	default:
	}
}

// ---------- encoding (§4) ----------

type NewTrace struct {
	Content, Kind, Scope string
	Project, Thread      *string
	SessionID            *int64
	Valence              *float64
	Salience             float64
	Verbatim             *string
	Ctx                  EncodingCtx
}

func (s *Store) Insert(ctx context.Context, in NewTrace) (int64, bool, error) {
	vecs, err := s.emb.Embed(ctx, []string{in.Content})
	if err != nil {
		return 0, false, err
	}
	v := Normalize(vecs[0])

	type dup struct {
		id      int64
		cos     float64
		content string
		sal     float64
		created time.Time
	}
	best := dup{id: -1}
	rows, err := s.db.Query(fmt.Sprintf(`SELECT id, content, salience, created_at, embedding
                FROM %s WHERE status='active' AND scope=? AND kind=?`, s.table("memories")), in.Scope, in.Kind)
	if err != nil {
		return 0, false, err
	}
	for rows.Next() {
		var id int64
		var content string
		var sal float64
		var created string
		var blob []byte
		if err := rows.Scan(&id, &content, &sal, &created, &blob); err != nil {
			rows.Close()
			return 0, false, err
		}
		if c := dot(v, decodeVec(blob)); c > best.cos {
			ct, _ := time.Parse(time.RFC3339, created)
			best = dup{id, c, content, sal, ct}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, err
	}

	now := time.Now().UTC()
	if best.id >= 0 && best.cos >= 0.90 {
		merged := s.Merge(best.content, in.Content)
		mvs, err := s.emb.Embed(ctx, []string{merged})
		if err != nil {
			return 0, false, err
		}
		mv := Normalize(mvs[0])
		sal := math.Max(best.sal, in.Salience)
		err = s.doWrite(func(tx *sql.Tx) error {
			if tx == nil {
				return errors.New("memstore: tx unavailable")
			}
			if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET content=?, salience=?,
                                created_at=?, embedding=?, embed_model=? WHERE id=?`, s.table("memories")),
				merged, sal, best.created.Format(time.RFC3339),
				encodeVec(mv), s.emb.Model(), best.id); err != nil {
				return err
			}
			_, err := tx.Exec(fmt.Sprintf(`INSERT OR IGNORE INTO %s(memory_id, at)
                                VALUES(?,?)`, s.table("rehearsals")), best.id, now.Format(time.RFC3339))
			return err
		})
		return best.id, true, err
	}

	ctxJSON, _ := json.Marshal(in.Ctx)
	var newID int64
	err = s.doWrite(func(tx *sql.Tx) error {
		if tx == nil {
			return errors.New("memstore: tx unavailable")
		}
		res, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s
                        (content, kind, scope, project_path, thread, session_id, valence,
                         salience, embed_model, embedding, verbatim_ref, encoding_ctx, created_at)
                        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.table("memories")),
			in.Content, in.Kind, in.Scope, in.Project, in.Thread, in.SessionID,
			in.Valence, in.Salience, s.emb.Model(), encodeVec(v), in.Verbatim,
			string(ctxJSON), now.Format(time.RFC3339))
		if err != nil {
			return err
		}
		if newID, err = res.LastInsertId(); err != nil {
			return err
		}
		_, err = tx.Exec(fmt.Sprintf(`INSERT OR IGNORE INTO %s(memory_id, at)
                        VALUES(?,?)`, s.table("rehearsals")), newID, now.Format(time.RFC3339))
		return err
	})
	return newID, false, err
}

func (s *Store) Revise(ctx context.Context, id int64, newContent, reason string) error {
	var old string
	if err := s.db.QueryRow(fmt.Sprintf(`SELECT content FROM %s WHERE id=?`, s.table("memories")), id).Scan(&old); err != nil {
		return err
	}
	vecs, err := s.emb.Embed(ctx, []string{newContent})
	if err != nil {
		return err
	}
	v := Normalize(vecs[0])
	now := time.Now().UTC().Format(time.RFC3339)
	return s.doWrite(func(tx *sql.Tx) error {
		if tx == nil {
			return errors.New("memstore: tx unavailable")
		}
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET content=?, embedding=?,
                        embed_model=? WHERE id=?`, s.table("memories")), newContent, encodeVec(v), s.emb.Model(), id); err != nil {
			return err
		}
		_, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s
                        (memory_id, old_content, new_content, at, source) VALUES (?,?,?,?,?)`, s.table("corrections")),
			id, old, newContent, now, reason)
		return err
	})
}

func (s *Store) Forget(ids ...int64) error {
	return s.doWrite(func(tx *sql.Tx) error {
		if tx == nil {
			return errors.New("memstore: tx unavailable")
		}
		for _, id := range ids {
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE id=?`, s.table("memories")), id); err != nil {
				return err
			}
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE memory_id=?`, s.table("rehearsals")), id); err != nil {
				return err
			}
			if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE memory_id=?`, s.table("corrections")), id); err != nil {
				return err
			}
		}
		return nil
	})
}

func unionSentences(old, nw string) string {
	seen := map[string]bool{}
	var out []string
	for _, s := range splitSentences(old + " " + nw) {
		key := strings.ToLower(strings.TrimSpace(s))
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, strings.TrimSpace(s))
		}
	}
	return strings.Join(out, " ")
}

func splitSentences(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '!' || r == '?' })
	out := make([]string, 0, len(f))
	for _, p := range f {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return out
}

// Suppress unused import warning for rand (used by FakeEmbedder indirectly
// via the deterministic hash — not actually random, but go vet is picky).
var _ = rand.Intn
