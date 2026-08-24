// Package version reports which build of ssh-cv is running.
//
// Two audiences, two formats, and the split is load-bearing. Short is one
// token and nothing else, because scripts/install.sh compares it against a
// release tag with a string equality test - anything else in that output and
// the updater sees a difference on every run and reinstalls forever. String
// is for a person reading journald, and says everything the toolchain knows.
package version

import "runtime/debug"

// Dev is what Version holds when nothing stamped it.
const Dev = "dev"

// Version is the release this binary was built from: a tag like "v1.4.0",
// stamped at link time by .github/workflows/release-ssh-cv.yml.
//
//	-ldflags "-X github.com/riptone/tonil/apps/ssh-cv/internal/version.Version=v1.4.0"
//
// A plain `go build` leaves it at Dev, which is both the honest answer and
// the safe one: the updater treats anything that is not a release tag as "not
// a release", so it can never read somebody's working copy as a version
// number and decide it is newer than what is published.
var Version = Dev

// Short is the version and nothing else - "v1.4.0", or "dev".
//
// This is what `ssh-cv --version` prints, and the updater's whole comparison.
func Short() string {
	if Version == "" {
		return Dev
	}
	return Version
}

// IsRelease reports whether this binary was stamped from a release tag.
func IsRelease() bool {
	return Short() != Dev
}

// String is Short for a release, and Short plus whatever the toolchain
// recorded about the checkout for anything else: "dev (a1b2c3d, modified)".
//
// The commit is the useful half of a dev build's identity - "dev" alone
// cannot tell you which dev - and `modified` is the half worth knowing
// before you trust a bug report: it means the tree had uncommitted changes
// when this was linked, so the commit named does not describe it.
func String() string {
	if IsRelease() {
		return Short()
	}

	revision, modified := vcs()
	if revision == "" {
		return Dev
	}
	if modified {
		return Dev + " (" + revision + ", modified)"
	}
	return Dev + " (" + revision + ")"
}

// vcs digs the commit out of the build info the Go toolchain embeds.
//
// Absent for a build made outside a git checkout, or one built with
// -buildvcs=false, which is why every caller treats "" as "unknown" rather
// than as an error. The revision is cut to the first 7 characters: this is
// the same abbreviation git itself prints, and a full 40-character hash in a
// log line is noise.
func vcs() (revision string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
			if len(revision) > 7 {
				revision = revision[:7]
			}
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return revision, modified
}
