package sandbox

import (
	"fmt"
	"strings"
)

// seatbeltProfile compiles the manifest into a sandbox-exec profile.
// Semantics: everything allowed by default, then writes denied, then the
// RW directories re-allowed (seatbelt's last matching rule wins), then
// network denied wholesale when the profile says so.
//
// Kept as a pure string builder so every platform can test the compilation
// even though only macOS executes it.
func seatbeltProfile(p *Profile) string {
	var sb strings.Builder
	sb.WriteString("(version 1)\n")
	sb.WriteString("(allow default)\n")
	sb.WriteString("(deny file-write*)\n")
	for _, dir := range p.ReadWrite {
		sb.WriteString(fmt.Sprintf("(allow file-write* (subpath %q))\n", dir))
	}
	if !p.Network {
		sb.WriteString("(deny network*)\n")
	}
	return sb.String()
}
