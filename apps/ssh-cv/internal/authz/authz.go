// Package authz resolves an SSH key to who it belongs to.
//
// Nothing this server renders is gated on the answer. The CV is public - as
// public as the website - so a session with no key at all gets everything.
// What a recognised key buys today is its label in the footer, which is how
// you tell which of your own machines you are looking at.
//
// The scopes are still parsed and still resolved, because the mechanism is
// the useful part and it is not worth rebuilding the day something here does
// need gating. SSH already solves "who is asking" better than a password
// would: the client proves possession of a private key during the handshake,
// and the server gets the public half.
//
// Keeping the allowlist in apps/api rather than in this binary or a file next
// to it means a key can be recognised or forgotten by editing a Worker
// secret, with no rebuild, no redeploy, and no SSH into the box that serves
// SSH. The fingerprint is the only thing that crosses the wire; the public
// key itself never leaves the host.
package authz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// Scope names a thing a session may do. Absence of a scope is denial.
//
// No named constant lives here on purpose: this server gates nothing, so a
// constant would be a scope with no reader. Scopes are carried through from
// the allowlist as written, for whatever asks for one first.
type Scope string

// Grant is the answer for one public key.
type Grant struct {
	// Label is a human name for the key, shown in the UI so you can tell
	// which of your machines you are on. Never used for a security decision.
	Label  string  `json:"label"`
	Scopes []Scope `json:"scopes"`
}

// Has reports whether the grant carries a scope. The zero Grant has none,
// which is what an unauthenticated or unknown key gets.
func (g Grant) Has(scope Scope) bool {
	for _, granted := range g.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}

// Fingerprint renders a public key the way OpenSSH does, so what shows up in
// the server's logs is the same string `ssh-keygen -lf` prints and can be
// pasted into the allowlist without translation.
func Fingerprint(key gossh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return "SHA256:" + strings.TrimRight(base64.StdEncoding.EncodeToString(sum[:]), "=")
}

// Authorizer resolves a fingerprint to a grant.
type Authorizer interface {
	Authorize(ctx context.Context, fingerprint string) Grant
}

// Denier grants nothing. It is the fallback when no API is configured, so a
// misconfigured deployment serves the public CV and withholds everything
// else rather than failing open.
type Denier struct{}

func (Denier) Authorize(context.Context, string) Grant { return Grant{} }

// StaticAuthorizer resolves from an in-memory map. Used by tests and by
// `--authorized-keys`, which is the escape hatch for running locally without
// pointing at the production API.
type StaticAuthorizer struct {
	Grants map[string]Grant
}

func (s StaticAuthorizer) Authorize(_ context.Context, fingerprint string) Grant {
	return s.Grants[fingerprint]
}

// ParseAuthorizedKeys reads an OpenSSH authorized_keys file and returns the
// grants it describes.
//
// Scopes come from the comment field, which is where OpenSSH already puts
// free text and what `ssh-keygen` writes your `user@host` into:
//
//	ssh-ed25519 AAAA... laptop notes
//	ssh-ed25519 AAAA... phone
//
// The first word of the comment is the label; any remaining words are
// scopes. A key with no scopes is still recognised - it gets a label and
// nothing else, which is a useful way to say "I know this key" without
// granting it anything.
func ParseAuthorizedKeys(data []byte) (map[string]Grant, error) {
	grants := map[string]Grant{}
	rest := data
	for len(bytes.TrimSpace(rest)) > 0 {
		key, comment, _, remaining, err := gossh.ParseAuthorizedKey(rest)
		if err != nil {
			return nil, fmt.Errorf("parse authorized_keys: %w", err)
		}
		rest = remaining

		fields := strings.Fields(comment)
		grant := Grant{}
		if len(fields) > 0 {
			grant.Label = fields[0]
			for _, scope := range fields[1:] {
				grant.Scopes = append(grant.Scopes, Scope(scope))
			}
		}
		grants[Fingerprint(key)] = grant
	}
	return grants, nil
}

// APIAuthorizer asks apps/api whether a fingerprint is allowed.
type APIAuthorizer struct {
	// Endpoint is the full URL of the authorize route.
	Endpoint string
	// Token authenticates this server to the API. Without it the API cannot
	// distinguish us from anyone who found the endpoint, and the allowlist
	// would be an oracle for guessing fingerprints.
	Token  string
	Client *http.Client

	// A short cache keeps a reconnect loop from turning into a request per
	// connection. Kept deliberately brief: revoking a key should take effect
	// in about the time it takes to notice you need to revoke it.
	TTL time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	grant   Grant
	expires time.Time
}

const (
	defaultTTL     = 60 * time.Second
	requestTimeout = 5 * time.Second
)

type authorizeRequest struct {
	Fingerprint string `json:"fingerprint"`
}

type authorizeResponse struct {
	Allowed bool    `json:"allowed"`
	Label   string  `json:"label"`
	Scopes  []Scope `json:"scopes"`
}

// Authorize returns the grant for a fingerprint, or the zero Grant.
//
// Every failure path - network down, API 500, malformed body - returns the
// zero Grant. A session that cannot be authorized is served the public CV,
// which is the same thing an unknown key gets, so an API outage degrades the
// service rather than exposing anything.
func (a *APIAuthorizer) Authorize(ctx context.Context, fingerprint string) Grant {
	if a.Endpoint == "" || fingerprint == "" {
		return Grant{}
	}
	if grant, ok := a.cached(fingerprint); ok {
		return grant
	}

	body, err := json.Marshal(authorizeRequest{Fingerprint: fingerprint})
	if err != nil {
		return Grant{}
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Grant{}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "tonil-ssh-cv")
	if a.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.Token)
	}

	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Grant{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Grant{}
	}

	var decoded authorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Grant{}
	}
	grant := Grant{}
	if decoded.Allowed {
		grant = Grant{Label: decoded.Label, Scopes: decoded.Scopes}
	}
	a.store(fingerprint, grant)
	return grant
}

func (a *APIAuthorizer) cached(fingerprint string) (Grant, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[fingerprint]
	if !ok || time.Now().After(entry.expires) {
		return Grant{}, false
	}
	return entry.grant, true
}

func (a *APIAuthorizer) store(fingerprint string, grant Grant) {
	ttl := a.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cache == nil {
		a.cache = map[string]cacheEntry{}
	}
	a.cache[fingerprint] = cacheEntry{grant: grant, expires: time.Now().Add(ttl)}
}

// ConstantTimeEqual compares two secrets without leaking their contents
// through timing. Exported because main uses it for the local token check.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
