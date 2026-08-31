// Package manifest reads dotfiles' manifest.jsonc, the single source of truth
// for what gets installed and linked on a machine.
//
// The file is JWCC - JSON with comments and trailing commas - which
// encoding/json cannot read. It is *not* safe to strip comments with a regex
// or a hand-rolled scanner that does not track string state: the real file
// contains
//
//	"fresh_machine": "git clone https://github.com/... && ./scripts/install.sh"
//
// and a stripper that sees `//` outside of a string parser eats the rest of
// that line, silently truncating the value rather than failing. hujson is a
// real JWCC parser and is the one dependency this package takes.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tailscale/hujson"
)

// Platform is an OS the manifest can target.
type Platform string

const (
	MacOS   Platform = "macos"
	Linux   Platform = "linux"
	Windows Platform = "windows"
)

var validPlatforms = []Platform{MacOS, Linux, Windows}

// SecretMode selects how a secret becomes a file on disk.
type SecretMode string

const (
	// ModeFile writes one Bitwarden field out verbatim. For files that are
	// sensitive end to end - a credential set has no public half worth
	// templating, and a template of nothing but placeholders is just a
	// second copy of the schema to keep in sync.
	ModeFile SecretMode = "file"
	// ModeTemplate renders a checked-in template, filling named holes. For
	// mostly-public config with a few secret fields, where keeping the shape
	// in git is worth more than keeping it out.
	ModeTemplate SecretMode = "template"
)

// ValueRef locates one value inside the vault.
type ValueRef struct {
	Item string `json:"item"`
	// Field is a login field ("username", "password"), "notes", or the name
	// of a custom field on the item. Defaults to "password".
	Field string `json:"field,omitempty"`
}

// Secret is one file rendered from the vault at install time.
//
// Targets are deliberately absolute paths in $HOME rather than stow packages.
// Stow works by symlinking $HOME into the repo, so anything stowed lives in
// the working tree and is one `git add -A` away from being committed - which
// is exactly how a credential file ends up in history. Rendered secrets are
// written straight to their target and never exist inside the repo.
type Secret struct {
	Name      string     `json:"name"`
	Mode      SecretMode `json:"mode"`
	Target    string     `json:"target"`
	Platforms []Platform `json:"platforms,omitempty"`

	// ModeFile only.
	Item  string `json:"item,omitempty"`
	Field string `json:"field,omitempty"`

	// ModeTemplate only. Template is relative to the dotfiles repo root.
	Template string              `json:"template,omitempty"`
	Values   map[string]ValueRef `json:"values,omitempty"`
}

// StowPackage is a directory whose tree mirrors $HOME.
type StowPackage struct {
	Name      string     `json:"name"`
	Platforms []Platform `json:"platforms"`
}

// Tool is something installed through a package manager.
type Tool struct {
	Cmd    string `json:"cmd"`
	Brew   string `json:"brew,omitempty"`
	Winget string `json:"winget,omitempty"`
	// App is the application bundle to look for when Cmd is not on PATH.
	//
	// A macOS GUI app installs to /Applications/<App>.app and puts nothing
	// on PATH, so `command -v ghostty` is a false negative - it reported
	// Ghostty missing on a machine where it was open at the time. The shell
	// installer had the same bug; this is the field that fixes it rather
	// than a hardcoded list of names in the detector.
	//
	// Checked on macOS only. On Linux and Windows the command is the
	// application, and an app-bundle path means nothing.
	App string `json:"app,omitempty"`
}

// Cask is a macOS GUI application.
type Cask struct {
	Brew      string     `json:"brew"`
	Platforms []Platform `json:"platforms,omitempty"`
}

// Component is a named unit with no package-manager entry (extras and
// system components share this shape).
type Component struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Platforms   []Platform `json:"platforms,omitempty"`
}

// ZshPlugin is an interactive-shell plugin installed alongside the tools.
type ZshPlugin struct {
	Brew string `json:"brew"`
}

// CLIOption documents one installer flag. Carried in the manifest so the
// help text and the README describe the same interface.
type CLIOption struct {
	Flag string `json:"flag"`
	Desc string `json:"desc"`
}

// CLI is the installer's self-description.
type CLI struct {
	Description     string      `json:"description,omitempty"`
	Options         []CLIOption `json:"options,omitempty"`
	FreshMachine    string      `json:"fresh_machine,omitempty"`
	ExistingMachine string      `json:"existing_machine,omitempty"`
}

// ExtraTool is something to verify that no `tools` entry covers - zsh, brew,
// stow itself.
//
// Written as a bare string in the common case, or as an object when the thing
// is a GUI application:
//
//	"extra_tools": { "macos": ["zsh", { "cmd": "ghostty", "app": "Ghostty" }] }
//
// The object form exists because `command -v ghostty` is a permanent false
// negative: a macOS app installs to /Applications/Ghostty.app and puts
// nothing on PATH, so `check --strict` could never pass on a machine where
// Ghostty was open at the time.
type ExtraTool struct {
	Cmd string `json:"cmd"`
	// App is the bundle to look for when Cmd is not on PATH. macOS only.
	App string `json:"app,omitempty"`
}

// UnmarshalJSON accepts either a bare command name or the object form.
func (e *ExtraTool) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		e.Cmd = name
		return nil
	}
	// Aliased so this does not recurse back into itself.
	type plain ExtraTool
	var obj plain
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("extra_tools entry must be a string or {cmd, app}: %w", err)
	}
	if obj.Cmd == "" {
		return fmt.Errorf("extra_tools entry has no cmd")
	}
	*e = ExtraTool(obj)
	return nil
}

// Vault is where the secrets come from.
//
// Not a secret itself - it is which Bitwarden deployment the account lives
// on, and getting it wrong produces "Invalid master password", which sends
// you looking at the wrong thing entirely.
type Vault struct {
	// Server is the deployment URL, e.g. https://vault.bitwarden.eu for the
	// EU cloud. Omit for Bitwarden's default (the US cloud), or for a CLI
	// somebody has configured by hand.
	Server string `json:"server,omitempty"`
}

// Health is what `doti check` verifies beyond the tool list: extra binaries
// per platform, and the symlinks that should exist.
type Health struct {
	ExtraTools map[Platform][]ExtraTool       `json:"extra_tools,omitempty"`
	Links      map[Platform]map[string]string `json:"links,omitempty"`
}

// Manifest is the whole file.
type Manifest struct {
	// Schema is the editor's validation pointer. Declared so
	// DisallowUnknownFields does not reject the file over it.
	Schema           string        `json:"$schema,omitempty"`
	App              string        `json:"app"`
	Version          string        `json:"version"`
	StowPackages     []StowPackage `json:"stow_packages"`
	StowIgnore       []string      `json:"stow_ignore,omitempty"`
	Tools            []Tool        `json:"tools,omitempty"`
	ZshPlugins       []ZshPlugin   `json:"zsh_plugins,omitempty"`
	BrewCasks        []Cask        `json:"brew_casks,omitempty"`
	Extras           []Component   `json:"extras,omitempty"`
	WingetExtras     []string      `json:"winget_extras,omitempty"`
	Mcps             []string      `json:"mcps,omitempty"`
	SystemComponents []Component   `json:"system_components,omitempty"`
	CLI              *CLI          `json:"cli,omitempty"`
	Health           *Health       `json:"health,omitempty"`
	// Secrets is optional: a checkout with no vault configured still
	// installs, it just renders nothing.
	Secrets []Secret `json:"secrets,omitempty"`
	Vault   *Vault   `json:"vault,omitempty"`
}

// Load reads and validates a manifest.
func Load(path string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return Parse(raw)
}

// Parse reads a manifest from bytes.
func Parse(raw []byte) (*Manifest, error) {
	// Standardize turns JWCC into plain JSON, preserving offsets so a syntax
	// error still points at the right line in the original file.
	plain, err := hujson.Standardize(raw)
	if err != nil {
		return nil, fmt.Errorf("manifest is not valid JSONC: %w", err)
	}

	var m Manifest
	decoder := json.NewDecoder(strings.NewReader(string(plain)))
	// A typo in a key is a silent no-op otherwise, which for a file that
	// decides what gets installed is worse than a failed parse.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate reports the first structural problem it finds.
func (m *Manifest) Validate() error {
	if m.App == "" {
		return fmt.Errorf("manifest: app is required")
	}
	for _, pkg := range m.StowPackages {
		if pkg.Name == "" {
			return fmt.Errorf("manifest: a stow_packages entry has no name")
		}
		if err := validatePlatforms(pkg.Name, pkg.Platforms); err != nil {
			return err
		}
	}
	seen := make(map[string]bool, len(m.Secrets))
	for i := range m.Secrets {
		if err := m.Secrets[i].validate(); err != nil {
			return err
		}
		if seen[m.Secrets[i].Name] {
			return fmt.Errorf("manifest: duplicate secret %q", m.Secrets[i].Name)
		}
		seen[m.Secrets[i].Name] = true
	}
	return nil
}

func validatePlatforms(owner string, platforms []Platform) error {
	for _, p := range platforms {
		if !slices.Contains(validPlatforms, p) {
			return fmt.Errorf("manifest: %s has unknown platform %q", owner, p)
		}
	}
	return nil
}

func (s *Secret) validate() error {
	if s.Name == "" {
		return fmt.Errorf("manifest: a secrets entry has no name")
	}
	if s.Target == "" {
		return fmt.Errorf("manifest: secret %q has no target", s.Name)
	}
	// A relative target would resolve against the working directory, which
	// for an installer run from the repo means writing the secret *into the
	// repo* - the one outcome this design exists to prevent.
	if !strings.HasPrefix(s.Target, "~/") && !filepath.IsAbs(s.Target) {
		return fmt.Errorf(
			"manifest: secret %q target %q must be absolute or start with ~/",
			s.Name, s.Target)
	}
	if err := validatePlatforms("secret "+s.Name, s.Platforms); err != nil {
		return err
	}

	switch s.Mode {
	case ModeFile:
		if s.Item == "" {
			return fmt.Errorf("manifest: secret %q (mode file) has no item", s.Name)
		}
		if s.Template != "" || len(s.Values) > 0 {
			return fmt.Errorf(
				"manifest: secret %q is mode file but sets template/values", s.Name)
		}
	case ModeTemplate:
		if s.Template == "" {
			return fmt.Errorf("manifest: secret %q (mode template) has no template", s.Name)
		}
		if len(s.Values) == 0 {
			return fmt.Errorf("manifest: secret %q (mode template) has no values", s.Name)
		}
		if s.Item != "" {
			return fmt.Errorf(
				"manifest: secret %q is mode template but sets item", s.Name)
		}
		for key, ref := range s.Values {
			if ref.Item == "" {
				return fmt.Errorf(
					"manifest: secret %q value %q has no item", s.Name, key)
			}
		}
	default:
		return fmt.Errorf(
			"manifest: secret %q has unknown mode %q (want %q or %q)",
			s.Name, s.Mode, ModeFile, ModeTemplate)
	}
	return nil
}

// FieldOrDefault is the vault field to read, defaulting to the sensible one
// for the mode.
func (s *Secret) FieldOrDefault() string {
	if s.Field != "" {
		return s.Field
	}
	return "notes"
}

// FieldOrDefault is the vault field for one templated value.
func (v ValueRef) FieldOrDefault() string {
	if v.Field != "" {
		return v.Field
	}
	return "password"
}

// WantsPlatform reports whether this secret applies to the given OS. An empty
// platform list means every platform.
func (s *Secret) WantsPlatform(p Platform) bool {
	return len(s.Platforms) == 0 || slices.Contains(s.Platforms, p)
}
