package version

// Version is the current a1 release shown on the splash screen and used by
// `a1 update`. Override at build time with:
//
//	go build -ldflags="-X github.com/damien1141/a1/internal/version.Version=v0.2.0"
var Version = "v0.28"
