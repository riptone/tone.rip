package authz

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// A scope name to test with. authz has no constants of its own: the server
// gates nothing today, so the allowlist's words are carried through as
// written and it is the caller who decides what they mean.
const scopeNotes Scope = "notes"

// Real ed25519 keys, generated from a fixed seed so the fixtures are stable
// across runs. Hand-written key bytes are not worth the trouble: the wire
// format has to actually parse for Fingerprint to mean anything.
func testKey(t *testing.T, seed byte) gossh.PublicKey {
	t.Helper()
	raw := make([]byte, ed25519.SeedSize)
	for i := range raw {
		raw[i] = seed + byte(i)
	}
	key, err := gossh.NewPublicKey(ed25519.NewKeyFromSeed(raw).Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatalf("build test key: %v", err)
	}
	return key
}

// authorizedLine renders a key the way an authorized_keys file would, with a
// trailing comment carrying the label and any scopes.
func authorizedLine(t *testing.T, key gossh.PublicKey, comment string) string {
	t.Helper()
	line := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key)))
	if comment != "" {
		line += " " + comment
	}
	return line + "\n"
}

func TestFingerprintMatchesOpenSSHFormat(t *testing.T) {
	key := testKey(t, 1)
	got := Fingerprint(key)
	// Must be pasteable straight from `ssh-keygen -lf`, so: SHA256: prefix,
	// base64 body, and no padding.
	if len(got) < 10 || got[:7] != "SHA256:" {
		t.Fatalf("Fingerprint() = %q, want a SHA256: prefix", got)
	}
	if got[len(got)-1] == '=' {
		t.Errorf("Fingerprint() = %q, should not be padded", got)
	}
	if want := gossh.FingerprintSHA256(key); got != want {
		t.Errorf("Fingerprint() = %q, x/crypto says %q", got, want)
	}
}

func TestGrantHas(t *testing.T) {
	grant := Grant{Scopes: []Scope{scopeNotes}}
	if !grant.Has(scopeNotes) {
		t.Error("expected the notes scope")
	}
	if grant.Has("admin") {
		t.Error("did not expect an admin scope")
	}
	// The zero grant is what an unknown key gets; it must deny everything.
	if (Grant{}).Has(scopeNotes) {
		t.Error("the zero Grant must not carry any scope")
	}
}

func TestParseAuthorizedKeys(t *testing.T) {
	laptopKey := testKey(t, 1)
	phoneKey := testKey(t, 90)
	data := []byte(authorizedLine(t, laptopKey, "laptop notes") +
		authorizedLine(t, phoneKey, "phone"))
	grants, err := ParseAuthorizedKeys(data)
	if err != nil {
		t.Fatalf("ParseAuthorizedKeys() error = %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(grants))
	}

	laptop := grants[Fingerprint(laptopKey)]
	if laptop.Label != "laptop" {
		t.Errorf("label = %q, want laptop", laptop.Label)
	}
	if !laptop.Has(scopeNotes) {
		t.Error("laptop should hold the notes scope")
	}

	// A key listed with only a label is recognised but granted nothing.
	for fingerprint, grant := range grants {
		if grant.Label == "phone" {
			if len(grant.Scopes) != 0 {
				t.Errorf("phone (%s) should have no scopes, got %v", fingerprint, grant.Scopes)
			}
		}
	}
}

func TestParseAuthorizedKeysRejectsGarbage(t *testing.T) {
	if _, err := ParseAuthorizedKeys([]byte("not a key at all\n")); err == nil {
		t.Error("expected an error for malformed input")
	}
}

func TestDenierGrantsNothing(t *testing.T) {
	if grant := (Denier{}).Authorize(context.Background(), "SHA256:whatever"); len(grant.Scopes) != 0 {
		t.Errorf("Denier granted %v", grant.Scopes)
	}
}

func newAPI(t *testing.T, handler http.HandlerFunc) (*APIAuthorizer, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &APIAuthorizer{
		Endpoint: server.URL,
		Token:    "test-token",
		Client:   server.Client(),
		TTL:      time.Minute,
	}, server
}

func TestAPIAuthorizerGrantsOnAllowed(t *testing.T) {
	auth, _ := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		var body authorizeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Fingerprint != "SHA256:abc" {
			t.Errorf("fingerprint = %q", body.Fingerprint)
		}
		_ = json.NewEncoder(w).Encode(authorizeResponse{
			Allowed: true, Label: "laptop", Scopes: []Scope{scopeNotes},
		})
	})

	grant := auth.Authorize(context.Background(), "SHA256:abc")
	if !grant.Has(scopeNotes) {
		t.Fatalf("expected the notes scope, got %+v", grant)
	}
	if grant.Label != "laptop" {
		t.Errorf("label = %q", grant.Label)
	}
}

func TestAPIAuthorizerDeniesWhenNotAllowed(t *testing.T) {
	auth, _ := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		// Scopes present but allowed=false must still deny - the flag is
		// authoritative, not the presence of a scope list.
		_ = json.NewEncoder(w).Encode(authorizeResponse{
			Allowed: false, Scopes: []Scope{scopeNotes},
		})
	})
	if grant := auth.Authorize(context.Background(), "SHA256:abc"); grant.Has(scopeNotes) {
		t.Error("allowed=false must deny regardless of the scope list")
	}
}

// Every upstream failure must fail closed: the session still gets the public
// CV, and nothing else.
func TestAPIAuthorizerFailsClosed(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"500": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"401": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		},
		"malformed body": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("{not json"))
		},
		"empty body": func(w http.ResponseWriter, _ *http.Request) {},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			auth, _ := newAPI(t, handler)
			if grant := auth.Authorize(context.Background(), "SHA256:abc"); len(grant.Scopes) != 0 {
				t.Errorf("granted %v on %s", grant.Scopes, name)
			}
		})
	}
}

func TestAPIAuthorizerFailsClosedWhenUnreachable(t *testing.T) {
	auth, server := newAPI(t, func(http.ResponseWriter, *http.Request) {})
	server.Close() // simulate the API being down

	if grant := auth.Authorize(context.Background(), "SHA256:abc"); len(grant.Scopes) != 0 {
		t.Errorf("granted %v with the API down", grant.Scopes)
	}
}

func TestAPIAuthorizerRequiresConfiguration(t *testing.T) {
	empty := &APIAuthorizer{}
	if grant := empty.Authorize(context.Background(), "SHA256:abc"); len(grant.Scopes) != 0 {
		t.Error("an unconfigured authorizer must grant nothing")
	}
	auth, _ := newAPI(t, func(http.ResponseWriter, *http.Request) {
		t.Error("should not have called the API for an empty fingerprint")
	})
	if grant := auth.Authorize(context.Background(), ""); len(grant.Scopes) != 0 {
		t.Error("an empty fingerprint must grant nothing")
	}
}

func TestAPIAuthorizerCaches(t *testing.T) {
	var calls atomic.Int32
	auth, _ := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(authorizeResponse{
			Allowed: true, Label: "laptop", Scopes: []Scope{scopeNotes},
		})
	})

	for i := 0; i < 5; i++ {
		if !auth.Authorize(context.Background(), "SHA256:abc").Has(scopeNotes) {
			t.Fatal("expected a grant")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("hit the API %d times, want 1", got)
	}

	// A different key must not be answered from another key's entry.
	auth.Authorize(context.Background(), "SHA256:different")
	if got := calls.Load(); got != 2 {
		t.Errorf("hit the API %d times after a second fingerprint, want 2", got)
	}
}

func TestAPIAuthorizerCacheExpires(t *testing.T) {
	var calls atomic.Int32
	auth, _ := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(authorizeResponse{Allowed: true})
	})
	auth.TTL = time.Millisecond

	auth.Authorize(context.Background(), "SHA256:abc")
	time.Sleep(5 * time.Millisecond)
	auth.Authorize(context.Background(), "SHA256:abc")

	if got := calls.Load(); got != 2 {
		t.Errorf("hit the API %d times across an expiry, want 2", got)
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !ConstantTimeEqual("secret", "secret") {
		t.Error("equal strings should compare equal")
	}
	for _, other := range []string{"secrey", "secre", "secrets", ""} {
		if ConstantTimeEqual("secret", other) {
			t.Errorf("%q should not equal secret", other)
		}
	}
}
