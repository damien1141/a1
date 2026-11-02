package memstore

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSoulParams parses a fenced ```soul block from a persona's system
// prompt into Params (Bet 06). The persona prompt IS the soul: the §11
// temperament keys — d, tau, sigma, k, gamma, beta, lambda, w_kwd, mu,
// m, vec_top, fts_top, diversity, tref — one "key: value" per line. A
// nostalgic persona (d 0.35) forgets slowly; an eager one (tau -1.5)
// recalls more. Absent keys keep their defaults; an absent block keeps
// DefaultParams entirely, so every existing persona is unchanged.
//
// The parse is strict on keys and lenient on whitespace: unknown keys
// are ignored (forward compatibility), malformed values keep the
// default for that key.
func ParseSoulParams(prompt string) (Params, bool) {
	block, ok := extractSoulBlock(prompt)
	if !ok {
		return DefaultParams(), false
	}
	p := DefaultParams()
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			continue
		}
		switch key {
		case "d":
			p.D = f
		case "tau":
			p.Tau = f
		case "sigma":
			p.Sigma = f
		case "k":
			p.K = int(f)
		case "gamma":
			p.Gamma = f
		case "beta":
			p.Beta = f
		case "lambda":
			p.Lambda = f
		case "w_kwd":
			p.WKwd = f
		case "mu":
			p.Mu = f
		case "m":
			p.M = int(f)
		case "vec_top":
			p.VecTop = int(f)
		case "fts_top":
			p.FtsTop = int(f)
		case "diversity":
			p.Diversity = f
		case "tref":
			p.TRef = f
		}
	}
	return p, true
}

// extractSoulBlock pulls the first fenced ```soul block's body.
func extractSoulBlock(prompt string) (string, bool) {
	opener := "```soul"
	start := strings.Index(prompt, opener)
	if start < 0 {
		return "", false
	}
	rest := prompt[start+len(opener):]
	if len(rest) > 0 && rest[0] != '\n' && rest[0] != '\r' && rest[0] != ' ' && rest[0] != '\t' {
		// "```soulsong" is not a soul block.
		return "", false
	}
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	} else {
		return "", false
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:end]), true
}

// Decay increments cool_passes for every memory not rehearsed since the
// cutoff — the background pass that finally drives the salience-cooling
// term σ·salience·γ^coolPasses (Bet 06; the counter was dead code
// before). Memories with no rehearsal at all cool only when they were
// created before the cutoff, so fresh memories never cool on their
// first interval. cutoff is RFC3339.
func (s *Store) Decay(cutoff string) error {
	if s == nil {
		return nil
	}
	_, err := s.db.Exec(fmt.Sprintf(
		`UPDATE %s SET cool_passes = cool_passes + 1
                 WHERE created_at < ?
                   AND id NOT IN (SELECT memory_id FROM %s WHERE at > ?)`,
		s.table("memories"), s.table("rehearsals")), cutoff, cutoff)
	return err
}
