package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// buildArchive makes a zip holding the named entries.
func buildArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range entries {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveRelease stands in for the GitHub release: the archive, and a
// SHA-256.txt covering it.
func serveRelease(t *testing.T, archive []byte, checksum string) string {
	t.Helper()
	if checksum == "" {
		sum := sha256.Sum256(archive)
		checksum = hex.EncodeToString(sum[:])
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/"+nerdFontArchive, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/"+nerdFontChecksums, func(w http.ResponseWriter, _ *http.Request) {
		// Two entries, so the lookup has to match the name rather than take
		// the first line.
		fmt.Fprintf(w, "%s  SomeOtherFont.zip\n%s  %s\n",
			strings.Repeat("a", 64), checksum, nerdFontArchive)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

// linuxFixture is an App that will look for a font.
func linuxFixture(t *testing.T) (*App, *Recorder) {
	t.Helper()
	a, _, rec := fixture(t)
	a.Platform = manifest.Linux
	return a, rec
}

func TestTheFontIsDownloadedVerifiedAndExtracted(t *testing.T) {
	a, rec := linuxFixture(t)
	a.FontBaseURL = serveRelease(t, buildArchive(t, map[string]string{
		"JetBrainsMonoNerdFont-Regular.ttf": "regular",
		"JetBrainsMonoNerdFont-Bold.ttf":    "bold",
		// The real archive carries .otf of the same faces; installing both
		// leaves every application listing each face twice.
		"JetBrainsMonoNerdFont-Regular.otf": "should be skipped",
		"README.md":                         "also skipped",
	}), "")

	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatal(err)
	}

	dir, err := a.fontDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) != 2 {
		t.Fatalf("extracted %v, want the two .ttf files only", names)
	}
	if !rec.Contains("2 faces") {
		t.Errorf("texts = %v", rec.Texts())
	}
}

// The control that makes downloading an archive acceptable at all.
func TestATamperedArchiveIsRefusedAndNothingIsWritten(t *testing.T) {
	a, rec := linuxFixture(t)
	archive := buildArchive(t, map[string]string{"X-Regular.ttf": "real"})
	// A checksum for different bytes: what a swapped asset looks like.
	a.FontBaseURL = serveRelease(t, archive, strings.Repeat("b", 64))

	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatalf("a bad checksum is reported, not returned: %v", err)
	}
	if !rec.Contains("checksum mismatch") {
		t.Fatalf("texts = %v", rec.Texts())
	}
	dir, _ := a.fontDir()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("a font directory was created despite the mismatch")
	}
}

// Zip slip. An entry's name comes from whoever built the archive, and
// `../../.zshrc` would otherwise be written outside the target directory.
// Taking only the base name removes the traversal rather than detecting it.
func TestAnEntryCannotEscapeTheFontDirectory(t *testing.T) {
	a, _ := linuxFixture(t)
	a.FontBaseURL = serveRelease(t, buildArchive(t, map[string]string{
		"../../../../../../tmp/doti-escaped.ttf":   "escaped",
		"..\\..\\..\\windows-style-escape.ttf":     "escaped",
		"nested/dir/JetBrainsMonoNerdFont-Reg.ttf": "fine",
	}), "")

	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/tmp/doti-escaped.ttf"); err == nil {
		os.Remove("/tmp/doti-escaped.ttf")
		t.Fatal("an archive entry escaped the font directory")
	}

	dir, _ := a.fontDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.ContainsAny(entry.Name(), `/\`) || strings.Contains(entry.Name(), "..") {
			t.Errorf("entry kept a path component: %q", entry.Name())
		}
	}
	// All three land flat, by base name.
	if len(entries) != 3 {
		t.Fatalf("want 3 flattened files, got %d", len(entries))
	}
}

func TestAnArchiveWithNoFontsIsReported(t *testing.T) {
	a, rec := linuxFixture(t)
	a.FontBaseURL = serveRelease(t, buildArchive(t, map[string]string{"README.md": "x"}), "")
	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("no .ttf files") {
		t.Fatalf("texts = %v", rec.Texts())
	}
}

func TestAChecksumFileWithoutOurArchiveIsRefused(t *testing.T) {
	a, rec := linuxFixture(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/"+nerdFontChecksums, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  SomethingElse.zip\n", strings.Repeat("c", 64))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	a.FontBaseURL = server.URL

	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("no entry for") {
		t.Fatalf("texts = %v", rec.Texts())
	}
}

// Re-downloading 40 MB to discover the font is already there would make
// `doti install` slow for everybody.
func TestAnAlreadyInstalledFontIsNotDownloadedAgain(t *testing.T) {
	a, rec := linuxFixture(t)
	dir, err := a.fontDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "JetBrainsMonoNerdFont-Regular.ttf"), "already here")

	// No server: any attempt to download would fail loudly.
	a.FontBaseURL = "http://127.0.0.1:1/never"
	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("already installed") {
		t.Fatalf("texts = %v", rec.Texts())
	}
}

// On macOS the Brewfile carries the cask, so doing it here would install it
// twice.
func TestTheFontIsLeftToBrewOnMacOS(t *testing.T) {
	a, _, rec := fixture(t)
	a.FontBaseURL = "http://127.0.0.1:1/never"
	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("installed by brew on macOS") {
		t.Fatalf("texts = %v", rec.Texts())
	}
}

func TestADryRunDownloadsNothing(t *testing.T) {
	a, rec := linuxFixture(t)
	a.DryRun = true
	a.FontBaseURL = "http://127.0.0.1:1/never"
	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rec.Contains("would install the Nerd Font") {
		t.Fatalf("texts = %v", rec.Texts())
	}
	dir, _ := a.fontDir()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("a dry run created the font directory")
	}
}

// The extra is only installed when the manifest asks for it on this platform.
func TestWantsExtraFollowsTheManifest(t *testing.T) {
	a, _, _ := fixture(t)
	if a.WantsExtra("nerd-font") {
		t.Error("the fixture manifest declares no extras")
	}

	write(t, filepath.Join(a.Repo, "manifest.jsonc"),
		strings.Replace(fixtureManifest, `"tools"`,
			`"extras": [{"name":"nerd-font","platforms":["linux"]}], "tools"`, 1))
	a.manifest = nil

	if a.WantsExtra("nerd-font") {
		t.Error("declared for linux only, so macOS should not want it")
	}
	a.Platform = manifest.Linux
	if !a.WantsExtra("nerd-font") {
		t.Error("linux should want it")
	}
	if a.WantsExtra("something-else") {
		t.Error("an undeclared extra should not be wanted")
	}
}

func TestSelectedToolsRejectsAnUnknownName(t *testing.T) {
	a, _, _ := fixture(t)
	missing := []manifest.Tool{{Cmd: "fd"}, {Cmd: "gh"}}

	a.Tools = "fd"
	got, err := a.selectedTools(missing)
	if err != nil || len(got) != 1 || got[0].Cmd != "fd" {
		t.Fatalf("got %+v, err %v", got, err)
	}

	// A silent no-op would let someone conclude the tool was already there.
	a.Tools = "fd,nope"
	if _, err := a.selectedTools(missing); err == nil {
		t.Fatal("want an error for an unknown tool")
	} else if !strings.Contains(err.Error(), "fd, gh") {
		t.Errorf("the error should list what is missing: %v", err)
	}

	a.Tools = ""
	if got, _ := a.selectedTools(missing); len(got) != 2 {
		t.Errorf("no --tools means all of them, got %+v", got)
	}
}

func TestArchiveBaseIsPlatformIndependent(t *testing.T) {
	for input, want := range map[string]string{
		"font.ttf":            "font.ttf",
		"nested/dir/font.ttf": "font.ttf",
		`..\..\..\evil.ttf`:   "evil.ttf",
		"../../../evil.ttf":   "evil.ttf",
		`mixed/sep\font.ttf`:  "font.ttf",
		"/absolute/font.ttf":  "font.ttf",
		"..":                  "",
		".":                   "",
		"/":                   "",
		"":                    "",
	} {
		if got := archiveBase(input); got != want {
			t.Errorf("archiveBase(%q) = %q, want %q", input, got, want)
		}
	}
}

// safeJoin is the guard that makes the containment property enforced at the
// point of use rather than inferred from archiveBase three functions away.
// CodeQL flagged the extraction as go/zipslip for exactly that reason.
func TestSafeJoinRefusesAnythingLeavingTheDirectory(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{
		"", ".", "..",
		"../evil.ttf", "../../evil.ttf",
		"sub/evil.ttf", `sub\evil.ttf`,
		"/etc/evil.ttf", `\evil.ttf`,
	} {
		if got, err := safeJoin(dir, name); err == nil {
			t.Errorf("safeJoin(%q) returned %q, want a refusal", name, got)
		}
	}

	// And the legitimate cases still work - including "...", which contains
	// ".." as a substring but is a perfectly ordinary filename. A naive
	// strings.Contains(name, "..") check would reject it.
	for _, name := range []string{"font.ttf", "JetBrainsMonoNerdFont-Bold.ttf", "..."} {
		got, err := safeJoin(dir, name)
		if err != nil {
			t.Errorf("safeJoin(%q) = %v, want it accepted", name, err)
			continue
		}
		if filepath.Dir(got) != filepath.Clean(dir) {
			t.Errorf("safeJoin(%q) = %q, which is not directly inside %q", name, got, dir)
		}
	}
}

// The end-to-end version: a hostile archive cannot place a file outside the
// font directory, whatever its entry names say.
func TestAHostileArchiveWritesNothingOutsideTheFontDirectory(t *testing.T) {
	a, _ := linuxFixture(t)
	canary := filepath.Join(t.TempDir(), "canary.ttf")

	a.FontBaseURL = serveRelease(t, buildArchive(t, map[string]string{
		"../../../../../../../../.." + canary:     "escaped",
		`..\..\..\..\..\..\..\windows-escape.ttf`: "escaped",
		"legit/JetBrainsMonoNerdFont-Regular.ttf": "fine",
	}), "")

	if err := a.InstallNerdFont(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(canary); err == nil {
		t.Fatal("an archive entry escaped the font directory")
	}

	dir, _ := a.fontDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		rel, err := filepath.Rel(dir, full)
		if err != nil || strings.Contains(rel, "..") || strings.ContainsAny(rel, `/\`) {
			t.Errorf("%q is not a plain file directly inside the font directory", entry.Name())
		}
	}
}
