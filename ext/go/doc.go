// Package ext is the public API surface for Phi extensions.
//
// Extensions are native binaries that speak the PXB protocol (package ext/pxb)
// over stdin/stdout. Prefer the author SDK in package ext/a1:
//
//	m := a1.New("greet", "1.0.0")
//	m.RegisterTool(...)
//	m.Run()
//
// Ship a a1.yaml next to the binary under ~/.a1/extensions/<name>/.
// See doc/extensions.md.
package ext
