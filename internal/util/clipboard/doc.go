// Package clipboard reads and writes the system clipboard (plain text and images).
//
// Image reads use platform tools: wl-paste / xclip on Linux, osascript or
// pngpaste on macOS, PowerShell on Windows, with a WSL fallback to the Windows
// clipboard when Linux tools see no image data.
package clipboard
