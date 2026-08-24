package version

import (
	"strings"
	"testing"
)

// stamp sets Version for one test the way the linker would, and puts it back
// afterwards - it is a package variable, and a test that leaks it would
// decide the result of every test that runs after it.
func stamp(t *testing.T, value string) {
	t.Helper()
	previous := Version
	t.Cleanup(func() { Version = previous })
	Version = value
}

func TestAStampedBuildReportsItsTag(t *testing.T) {
	stamp(t, "v1.4.0")

	if got := Short(); got != "v1.4.0" {
		t.Errorf("Short() = %q, want %q", got, "v1.4.0")
	}
	// The whole point of Short: one token, so the updater's string comparison
	// against a release tag is the entire check.
	if strings.ContainsAny(Short(), " ()") {
		t.Errorf("Short() = %q, which the updater cannot compare", Short())
	}
	if !IsRelease() {
		t.Error("IsRelease() = false for a stamped build")
	}
	if got := String(); got != "v1.4.0" {
		t.Errorf("String() = %q, want the bare tag for a release", got)
	}
}

func TestAnUnstampedBuildIsNotAVersion(t *testing.T) {
	stamp(t, Dev)

	if IsRelease() {
		t.Error("IsRelease() = true for an unstamped build")
	}
	// The updater compares against release tags, so a working copy has to be
	// distinguishable from one. If this ever returned something tag-shaped,
	// a dev binary on the box would look like a release and never update.
	if strings.HasPrefix(Short(), "v") {
		t.Errorf("Short() = %q, which reads as a release tag", Short())
	}
}

// An empty -X value is a plausible mistake in a build script: the flag is
// there, the shell variable behind it is empty. Reporting "" as the running
// version would make the updater see a difference on every single run and
// reinstall the same binary forever.
func TestAnEmptyStampFallsBackToDev(t *testing.T) {
	stamp(t, "")

	if got := Short(); got != Dev {
		t.Errorf("Short() = %q, want %q", got, Dev)
	}
	if IsRelease() {
		t.Error("IsRelease() = true for an empty stamp")
	}
}

// String is the line a person reads in journald, so it has to start with the
// same answer Short gives - and then may add to it.
func TestTheLongFormBeginsWithTheShortOne(t *testing.T) {
	for _, value := range []string{"v2.0.0", Dev} {
		stamp(t, value)
		if !strings.HasPrefix(String(), Short()) {
			t.Errorf("String() = %q does not begin with Short() = %q",
				String(), Short())
		}
	}
}

// `go test` builds from a git checkout, so the toolchain should have recorded
// a commit. This is the one assertion that can legitimately be skipped: build
// info is absent outside a checkout and with -buildvcs=false, and both are
// real ways to run this suite.
func TestADevBuildNamesItsCommit(t *testing.T) {
	stamp(t, Dev)

	revision, _ := vcs()
	if revision == "" {
		t.Skip("no VCS stamp in this build; nothing to assert")
	}
	if len(revision) > 7 {
		t.Errorf("revision %q was not abbreviated", revision)
	}
	if !strings.Contains(String(), revision) {
		t.Errorf("String() = %q omits the commit %q", String(), revision)
	}
}
