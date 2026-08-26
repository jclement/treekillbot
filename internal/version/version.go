// Package version carries the build stamp and answers "am I still current?".
//
// The three variables below are set at link time with -X flags. They live in
// their own package rather than in main because main is not importable: the
// PDF /Producer string, the diagnostics header and the update checker all need
// the version, and none of them can reach into package main to get it.
//
// The cost of that choice is a long, fragile -X path that three files have to
// agree on. mise.toml holds it once, in $VERSION_PKG, and .goreleaser.yml
// spells it out; if you move or rename this package, both need editing or the
// binary silently reports "dev" forever.
package version

import (
	"fmt"
	"runtime"
)

// Name is the binary's name. It appears in version output, in the User-Agent
// sent to GitHub, and in the release archive names the updater looks for, so
// it has to match the binary name in .goreleaser.yml exactly.
const Name = "treekillbot"

// The build stamp. These defaults are what a plain `go build` or `go test`
// produces. "dev" is load-bearing rather than cosmetic — the update check
// refuses to run against an unstamped binary, because there is no release to
// compare an unreleased build against.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// IsDev reports whether this binary was built without a release stamp.
func IsDev() bool { return Version == "dev" }

// String is the one-line form shown by --version:
//
//	treekillbot v1.2.3 (abc1234, 2026-01-15T10:30:00Z)
func String() string {
	return fmt.Sprintf("%s %s (%s, %s)", Name, Tag(), Commit, BuildDate)
}

// Detailed is String plus the toolchain and platform, for the `version`
// subcommand. It is what a bug report should paste, so it is deliberately
// multi-line and aligned rather than dense.
func Detailed() string {
	return fmt.Sprintf("%s\n  go:       %s\n  platform: %s/%s",
		String(), runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Tag renders Version the way the git tag spells it. GoReleaser strips the
// leading "v" from {{.Version}}, so the stamp reads "1.2.3" while every
// human-facing surface — and the tag it came from — says "v1.2.3". Unstamped
// and pseudo-version builds ("dev", "1.2.3-4-gabc1234-dirty") are passed
// through unchanged if they do not start with a digit.
func Tag() string {
	if Version == "" {
		return "dev"
	}
	if c := Version[0]; c >= '0' && c <= '9' {
		return "v" + Version
	}
	return Version
}
