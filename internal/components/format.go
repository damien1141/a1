package components

import "strconv"

// FormatTokens renders a token count compactly: 999, 1.2k, 15k, 1.5M.
// Shared by the composer token readout and the compaction marker so both
// round the same way.
func FormatTokens(count int) string {
	if count < 0 {
		count = 0
	}
	if count < 1000 {
		return strconv.Itoa(count)
	}
	if count < 10000 {
		return strconv.FormatFloat(float64(count)/1000, 'f', 1, 64) + "k"
	}
	if count < 1000000 {
		return strconv.Itoa(count/1000) + "k"
	}
	if count < 10000000 {
		return strconv.FormatFloat(float64(count)/1000000, 'f', 1, 64) + "M"
	}
	return strconv.Itoa(count/1000000) + "M"
}
