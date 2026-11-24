package memstore

import (
	"encoding/json"
	"fmt"
	"time"
)

// List returns the store's active memories, newest first, capped at limit
// (limit <= 0 means the default 200). It is the read the persona manager's
// memory panel needs: Retrieve answers "what is relevant to this query" and
// ranks by activation, which is the wrong question for a list a human is
// going to scroll and edit. Nothing here is scored or filtered — every active
// row comes back with its id, so List is the exact inverse of Forget/Revise.
func (s *Store) List(limit int) ([]Trace, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(fmt.Sprintf(`SELECT id, content, kind, scope, project_path, thread,
                session_id, valence, salience, cool_passes, embed_model, verbatim_ref,
                encoding_ctx, created_at
                FROM %s WHERE status='active' ORDER BY created_at DESC, id DESC LIMIT ?`,
		s.table("memories")), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Trace
	for rows.Next() {
		var t Trace
		var ctxJSON, created string
		if err := rows.Scan(&t.ID, &t.Content, &t.Kind, &t.Scope, &t.Project,
			&t.Thread, &t.SessionID, &t.Valence, &t.Salience, &t.CoolPasses,
			&t.EmbedModel, &t.Verbatim, &ctxJSON, &created); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if ctxJSON != "" {
			_ = json.Unmarshal([]byte(ctxJSON), &t.Ctx)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
