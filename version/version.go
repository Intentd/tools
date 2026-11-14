// Package version reports a build's version, commit and date, set at build
// time. Every binary built on tools sets the same three variables:
//
//	go build -ldflags "\
//	  -X github.com/intentd/tools/version.Version=v1.0.0 \
//	  -X github.com/intentd/tools/version.Commit=abc1234 \
//	  -X github.com/intentd/tools/version.Date=2026-03-19T10:00:00Z"
//
// A variable not set keeps its default. How a CLI prints them is its own.
package version

import "fmt"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns the full version: version, commit and build date.
func String() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, Date)
}

// Short returns just the version.
func Short() string { return Version }
