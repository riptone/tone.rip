package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Is there a newer release than the one running?
//
// The same question scripts/install.sh asks, against the same endpoint and by
// the same rule: releases come back newest first, and tags are namespaced by
// app - which is what makes "the newest doti release" a different question from
// "the newest release" in a repository that also releases ssh-cv.
//
// What is deliberately *not* here is the download. SelfUpdate re-runs the
// installer, which already resolves the release, verifies it against
// SHA256SUMS and asks the binary its own version before trusting it. A second
// implementation of that in Go would be the one that goes stale.

// RepoSlug is the repository every release, installer and raw URL here refers
// to. One constant, because three of them used to spell it out.
const RepoSlug = "riptone/tone.rip"

// TagPrefix namespaces this app's tags inside a repository that releases more
// than one binary.
const TagPrefix = "doti/"

// Releases answers "what is the newest release".
type Releases struct {
	// BaseURL overrides the API endpoint - for a mirror, and for the test
	// that proves the parsing without reaching GitHub.
	BaseURL string
	// Client is the HTTP client. A nil one gets a short-deadline default,
	// because this runs while somebody is waiting to see a menu: an update
	// check that hangs is worse than no update check.
	Client *http.Client
}

// checkTimeout is how long the menu will wait to learn about an update.
//
// Short on purpose. The result is an optional footer hint; nothing downstream
// depends on it, so a slow or captive network should cost a couple of seconds
// and then be forgotten.
const checkTimeout = 3 * time.Second

func (r Releases) base() string {
	if r.BaseURL != "" {
		return r.BaseURL
	}
	return "https://api.github.com/repos/" + RepoSlug + "/releases"
}

func (r Releases) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: checkTimeout}
}

// release is the one field of a release this needs.
type release struct {
	TagName string `json:"tag_name"`
}

// Latest is the newest doti release's version, as "v1.2.3".
//
// An empty string with no error means the repository has releases but none of
// them are this app's - which is a real state, not a failure: ssh-cv was
// released from here first.
func (r Releases) Latest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base()+"?per_page=100", nil)
	if err != nil {
		return "", fmt.Errorf("building the release request: %w", err)
	}
	// Asked for explicitly rather than left to the default, because GitHub
	// serves a different shape to clients that do not.
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := r.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("asking %s for releases: %w", r.base(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", r.base(), resp.Status)
	}

	var list []release
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", fmt.Errorf("reading the release list: %w", err)
	}
	for _, rel := range list {
		if version, found := strings.CutPrefix(rel.TagName, TagPrefix); found {
			return "v" + strings.TrimPrefix(version, "v"), nil
		}
	}
	return "", nil
}

// Newer reports whether candidate is a later release than current.
//
// Compared field by field rather than as strings, because "v0.10.0" sorts
// before "v0.9.0" and telling somebody to downgrade is worse than telling them
// nothing. An unstamped build calls itself "dev", which is never older than
// anything: a working copy must not be offered a release that would replace
// the binary being worked on.
func Newer(current, candidate string) bool {
	if candidate == "" || current == "dev" || current == candidate {
		return false
	}
	have, ok := parseVersion(current)
	if !ok {
		return false
	}
	want, ok := parseVersion(candidate)
	if !ok {
		return false
	}
	for i := range have {
		if want[i] != have[i] {
			return want[i] > have[i]
		}
	}
	return false
}

// parseVersion reads "v1.2.3" into its three numbers. Anything with a
// pre-release suffix is refused rather than guessed at: comparing those
// correctly is a specification, and this needs three integers.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
