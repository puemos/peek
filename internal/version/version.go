// Package version holds build-time version info injected via -ldflags.
package version

// These variables are set at build time via:
//   -ldflags "-X github.com/puemos/peek/internal/version.Version=<ver> \
//             -X github.com/puemos/peek/internal/version.Commit=<sha> \
//             -X github.com/puemos/peek/internal/version.BuildDate=<date>"
// If not set, sensible defaults are used.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// String returns a human-readable version string.
func String() string {
	return Version + " (" + Commit + ", built " + BuildDate + ")"
}
