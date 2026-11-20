// Package buildinfo carries the version stamped in at build time with
// -ldflags "-X .../buildinfo.Version=...".
package buildinfo

// Set by the linker.
var (
	Version = "dev"
	Commit  = "none"
)
