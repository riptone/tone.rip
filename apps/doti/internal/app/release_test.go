package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func releaseServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("per_page = %q, want 100 - one page has to be enough", got)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "github") {
			t.Errorf("Accept = %q, want the GitHub media type", got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The rule install.sh uses: releases come back newest first, and only this
// app's tags count. A repository that also releases ssh-cv must not offer an
// ssh-cv tag as a doti update.
func TestLatestTakesTheFirstDotiTag(t *testing.T) {
	srv := releaseServer(t, `[
		{"tag_name": "ssh-cv/v0.2.0"},
		{"tag_name": "doti/v0.1.1"},
		{"tag_name": "doti/v0.1.0"},
		{"tag_name": "ssh-cv/v0.1.0"}
	]`, http.StatusOK)

	got, err := Releases{BaseURL: srv.URL}.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.1.1" {
		t.Errorf("Latest = %q, want v0.1.1", got)
	}
}

// A real state rather than a failure: ssh-cv was released from this repository
// before doti existed.
func TestLatestIsEmptyWhenNothingIsThisApp(t *testing.T) {
	srv := releaseServer(t, `[{"tag_name": "ssh-cv/v0.1.0"}]`, http.StatusOK)
	got, err := Releases{BaseURL: srv.URL}.Latest(context.Background())
	if err != nil {
		t.Fatalf("no doti release is not an error: %v", err)
	}
	if got != "" {
		t.Errorf("Latest = %q, want empty", got)
	}
}

func TestLatestReportsAFailedRequest(t *testing.T) {
	srv := releaseServer(t, `nope`, http.StatusForbidden)
	if _, err := (Releases{BaseURL: srv.URL}).Latest(context.Background()); err == nil {
		t.Fatal("a 403 should be an error - rate limiting is the common one")
	}
}

func TestLatestReportsUnreadableJSON(t *testing.T) {
	srv := releaseServer(t, `{"not": "a list"}`, http.StatusOK)
	if _, err := (Releases{BaseURL: srv.URL}).Latest(context.Background()); err == nil {
		t.Fatal("want an error for a body that is not a release list")
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		name               string
		current, candidate string
		want               bool
	}{
		{"a later patch", "v0.1.0", "v0.1.1", true},
		{"a later minor", "v0.1.9", "v0.2.0", true},
		{"a later major", "v0.9.9", "v1.0.0", true},
		{"the same release", "v1.2.3", "v1.2.3", false},
		{"an earlier release", "v1.2.3", "v1.2.2", false},
		// The reason this is not a string comparison: "v0.10.0" < "v0.9.0"
		// lexically, and offering a downgrade is worse than offering nothing.
		{"ten is later than nine", "v0.9.0", "v0.10.0", true},
		{"nine is not later than ten", "v0.10.0", "v0.9.0", false},
		// A working copy must not be told to replace the binary being worked
		// on with a release.
		{"a dev build is never behind", "dev", "v9.9.9", false},
		{"nothing to offer", "v0.1.0", "", false},
		{"a pre-release is refused rather than guessed at", "v0.1.0", "v0.2.0-rc1", false},
		{"junk is refused", "v0.1.0", "banana", false},
		{"junk on the left is refused", "banana", "v0.1.0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Newer(tc.current, tc.candidate); got != tc.want {
				t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
			}
		})
	}
}

// The URLs SelfUpdate pipes into a shell and the API the check asks all name
// the same repository. They used to spell it out separately.
func TestOneRepositoryConstant(t *testing.T) {
	for _, url := range []string{installerURL, installerPS1} {
		if !strings.Contains(url, RepoSlug) {
			t.Errorf("%s does not name %s", url, RepoSlug)
		}
	}
	if !strings.HasPrefix(TagPrefix, "doti") {
		t.Errorf("TagPrefix = %q, want this app's namespace", TagPrefix)
	}
}

// Build metadata takes no part in precedence, and every locally built binary
// carries some: `bun run install-local` stamps "<tag>+dev.<sha>", which split
// into four parts on the dot and was refused - so a working copy could never be
// told about a release, whatever was released.
func TestBuildMetadataDoesNotBlockTheOffer(t *testing.T) {
	for _, tc := range []struct {
		current, candidate string
		want               bool
	}{
		{"v0.1.1+dev.c23f48f", "v0.1.2", true},
		{"v0.1.1+dev.c23f48f", "v0.2.0", true},
		{"v0.1.1+dev.c23f48f", "v1.0.0", true},
		// The same release, built locally: nothing newer to offer.
		{"v0.1.2+dev.c23f48f", "v0.1.2", false},
		// Ahead of the release, which is the normal state of a working copy
		// between tags.
		{"v0.1.3+dev.c23f48f", "v0.1.2", false},
		// A release build, unchanged behaviour.
		{"v0.1.1", "v0.1.2", true},
		{"v0.1.2", "v0.1.1", false},
		// A binary with no version at all is still never offered anything: it
		// is somebody's `go build`, and replacing it is not this tool's call.
		{"dev", "v0.1.2", false},
		// Metadata on both sides, in case a release ever carries some.
		{"v0.1.1+build.1", "v0.1.2+build.9", true},
		// A pre-release is still refused rather than guessed at.
		{"v0.1.1-rc.1", "v0.1.2", false},
		{"v0.1.1", "v0.1.2-rc.1", false},
	} {
		if got := Newer(tc.current, tc.candidate); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v",
				tc.current, tc.candidate, got, tc.want)
		}
	}
}

// And the metadata is dropped rather than parsed into the numbers.
func TestParseVersionDropsBuildMetadata(t *testing.T) {
	got, ok := parseVersion("v0.1.1+dev.c23f48f")
	if !ok {
		t.Fatal("refused a stamped local build")
	}
	if got != [3]int{0, 1, 1} {
		t.Errorf("parsed %v", got)
	}
}
