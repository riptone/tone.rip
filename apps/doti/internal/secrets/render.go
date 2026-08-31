package secrets

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// Result is what happened to one secret.
type Result struct {
	Name    string
	Target  string
	Changed bool
	Skipped bool
	// Reason is set when Skipped, for the human.
	Reason string
}

// Renderer turns manifest secrets into files.
type Renderer struct {
	Client   *Client
	RepoRoot string
	Home     string
	Platform manifest.Platform
	// DryRun computes everything and writes nothing.
	DryRun bool

	scrub Scrubber
}

// Scrubber exposes the redactor so a caller printing its own messages can
// route them through the same filter.
func (r *Renderer) Scrubber() *Scrubber { return &r.scrub }

// RenderAll renders every secret that applies to this platform.
//
// It stops at the first failure rather than continuing: a half-rendered set
// of credentials is harder to reason about than none, and the caller treats
// the whole step as optional anyway.
func (r *Renderer) RenderAll(ctx context.Context, secrets []manifest.Secret) ([]Result, error) {
	results := make([]Result, 0, len(secrets))
	for i := range secrets {
		result, err := r.Render(ctx, secrets[i])
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

// Render writes one secret to its target.
func (r *Renderer) Render(ctx context.Context, s manifest.Secret) (Result, error) {
	result := Result{Name: s.Name}

	if !s.WantsPlatform(r.Platform) {
		result.Skipped = true
		result.Reason = "not for " + string(r.Platform)
		return result, nil
	}

	target, err := r.expand(s.Target)
	if err != nil {
		return result, err
	}
	result.Target = target

	var data []byte
	switch s.Mode {
	case manifest.ModeFile:
		data, err = r.renderFile(ctx, s)
	case manifest.ModeTemplate:
		data, err = r.renderTemplate(ctx, s)
	default:
		err = fmt.Errorf("secret %q: unknown mode %q", s.Name, s.Mode)
	}
	if err != nil {
		return result, r.scrub.Err(err)
	}

	changed, err := r.write(target, data)
	if err != nil {
		return result, r.scrub.Err(err)
	}
	result.Changed = changed
	return result, nil
}

func (r *Renderer) renderFile(ctx context.Context, s manifest.Secret) ([]byte, error) {
	value, err := r.Client.Field(ctx, s.Item, s.FieldOrDefault())
	if err != nil {
		return nil, err
	}
	r.scrub.Add(value)
	// An empty value means the item or field is wrong far more often than it
	// means "this credential is genuinely blank". Writing it would truncate
	// a working config to nothing and the next failure would surface
	// somewhere unrelated.
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf(
			"secret %q: bitwarden item %q field %q is empty",
			s.Name, s.Item, s.FieldOrDefault())
	}
	return []byte(value), nil
}

func (r *Renderer) renderTemplate(ctx context.Context, s manifest.Secret) ([]byte, error) {
	path := filepath.Join(r.RepoRoot, filepath.FromSlash(s.Template))
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secret %q: reading template: %w", s.Name, err)
	}

	values := make(map[string]string, len(s.Values))
	for key, ref := range s.Values {
		value, err := r.Client.Field(ctx, ref.Item, ref.FieldOrDefault())
		if err != nil {
			return nil, fmt.Errorf("secret %q value %q: %w", s.Name, key, err)
		}
		r.scrub.Add(value)
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf(
				"secret %q: value %q resolved empty from item %q field %q",
				s.Name, key, ref.Item, ref.FieldOrDefault())
		}
		values[key] = value
	}

	// missingkey=error so a placeholder with no matching entry in `values`
	// fails loudly instead of rendering "<no value>" into a config file.
	tmpl, err := template.New(filepath.Base(s.Template)).
		Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("secret %q: parsing template: %w", s.Name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, values); err != nil {
		return nil, fmt.Errorf("secret %q: rendering template: %w", s.Name, err)
	}
	return out.Bytes(), nil
}

// expand resolves a ~/ target against the home directory.
func (r *Renderer) expand(target string) (string, error) {
	if !strings.HasPrefix(target, "~/") {
		return target, nil
	}
	if r.Home == "" {
		return "", fmt.Errorf("cannot expand %q: no home directory known", target)
	}
	return filepath.Join(r.Home, filepath.FromSlash(strings.TrimPrefix(target, "~/"))), nil
}

// write puts data at path with owner-only permissions, atomically.
//
// Atomic because the alternative - truncate then write - leaves a
// zero-length credentials file behind if anything fails in between, and the
// tools reading it fail in ways that look nothing like "the installer died".
//
// It reports whether the content actually changed, and rewrites nothing when
// it did not, so `doti --check` and a re-run leave mtimes alone.
func (r *Renderer) write(path string, data []byte) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	if r.DryRun {
		return true, nil
	}

	dir := filepath.Dir(path)
	// 0700: these directories hold credentials. MkdirAll leaves the mode of
	// directories that already exist alone, so this only tightens new ones.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("creating %s: %w", dir, err)
	}

	// In the same directory, so the rename below is on one filesystem and
	// therefore atomic. A temp file in /tmp would make it a copy.
	tmp, err := os.CreateTemp(dir, ".doti-*")
	if err != nil {
		return false, fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op once the rename has succeeded
	}()

	// Before the write, not after: a chmod afterwards leaves a window where
	// the credentials are on disk at the default mode.
	if err := tmp.Chmod(0o600); err != nil {
		return false, fmt.Errorf("securing temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		return false, fmt.Errorf("flushing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("closing %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return false, fmt.Errorf("installing %s: %w", path, err)
	}
	return true, nil
}
