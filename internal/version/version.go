// Package version carries build identity, stamped by the linker at release time.
package version

// These are overridden at build time with:
//
//	-ldflags "-X <pkg>/internal/version.Version=hub-v1.2.3 -X ...Commit=abc123 -X ...Date=2026-09-08"
//
// A plain `go build` (dev) leaves the defaults below.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String is a one-line human identifier.
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
