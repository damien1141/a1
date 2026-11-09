package llm

import "encoding/json"

// RequestBytes measures the serialized size of a request in bytes — the
// exact yardstick a 413/Payload-Too-Large rejection actually judged, and
// therefore the only honest progress ruler for overflow recovery. Token
// estimates are byte-blind to images (hermes #88960: two image results were
// 96.6% of a request's bytes but ~3.7% of its token estimate, so
// token-scored progress reported shrinking on requests that had not shrunk
// and the session wedged).
//
// The measure is relative, not wire-exact: providers serialize requests in
// their own envelopes, so the absolute number means nothing across
// providers. But for the same session across consecutive recovery attempts
// the envelope shape is constant, and a ≥5% byte drop is a real drop.
// Returns 0 when a message cannot be marshaled — callers must treat 0 as
// "unmeasurable", never as progress.
func RequestBytes(msgs []Message) int64 {
	var total int64
	for i := range msgs {
		b, err := json.Marshal(msgs[i])
		if err != nil {
			return 0
		}
		total += int64(len(b))
	}
	return total
}
