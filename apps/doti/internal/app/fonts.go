package app

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/riptone/tone.rip/apps/doti/internal/manifest"
)

// The Nerd Font, which no package manager covers off macOS.
//
// On macOS it is a brew cask and the Brewfile already handles it. On Linux and
// Windows it is a zip on a GitHub release, which is why this exists at all -
// it was the one entry in the manifest's `extras` that nothing installed.
const (
	// Pinned, not `latest`. A font is a dependency like any other, and
	// "whatever shipped this morning" is not a version - it also means the
	// checksum below could not be pinned either, which would leave the
	// download unverified.
	nerdFontVersion = "v3.5.1"
	nerdFontArchive = "JetBrainsMono.zip"
	// The release publishes SHA-256.txt covering every archive, so the
	// download is verified rather than trusted. Fetched alongside rather
	// than hardcoded: pinning the version pins the contents, and a hardcoded
	// hash here would be a second thing to update on every bump.
	nerdFontChecksums = "SHA-256.txt"

	// A zip bomb guard. The real archive is about 40 MB expanded; this is
	// generous and still bounded.
	maxFontArchive = 200 << 20
	maxFontFile    = 20 << 20
)

// DefaultFontBaseURL is where the release assets come from.
var DefaultFontBaseURL = "https://github.com/ryanoasis/nerd-fonts/releases/download/" + nerdFontVersion

// fontBaseURL is the base for this run, overridable so the tests can serve a
// fixture instead of reaching GitHub.
func (a *App) fontBaseURL() string {
	if a.FontBaseURL != "" {
		return a.FontBaseURL
	}
	return DefaultFontBaseURL
}

// fontDir is where this platform keeps user-installed fonts.
func (a *App) fontDir() (string, error) {
	switch a.Platform {
	case manifest.Linux:
		return filepath.Join(a.Home, ".local", "share", "fonts", "JetBrainsMonoNerdFont"), nil
	case manifest.Windows:
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			return "", fmt.Errorf("LOCALAPPDATA is not set")
		}
		return filepath.Join(localAppData, "Microsoft", "Windows", "Fonts"), nil
	default:
		return "", fmt.Errorf("no font directory for %s", a.Platform)
	}
}

// nerdFontFaces is how many Nerd Font faces are already in the font directory.
//
// The same glob InstallNerdFont uses to decide it has nothing to do, exposed so
// the selector can say so too. Without it the extras row read "not checked"
// forever, which made it the one thing Adopt could never drop from a list of
// what the machine is missing.
func (a *App) nerdFontFaces() int {
	if a.Platform == manifest.MacOS {
		// The Brewfile carries the cask, so the cask row is the answer.
		return 0
	}
	dir, err := a.fontDir()
	if err != nil {
		return 0
	}
	installed, err := filepath.Glob(filepath.Join(dir, "*NerdFont*.ttf"))
	if err != nil {
		return 0
	}
	return len(installed)
}

// InstallNerdFont downloads and extracts the font, if this platform needs it.
func (a *App) InstallNerdFont(ctx context.Context) error {
	if a.Platform == manifest.MacOS {
		// The Brewfile carries `cask "font-jetbrains-mono-nerd-font"`.
		a.Report.Line(MarkSkip, "nerd-font (installed by brew on macOS)")
		return nil
	}

	dir, err := a.fontDir()
	if err != nil {
		a.Report.Line(MarkWarn, "nerd-font: "+err.Error())
		return nil
	}

	// Already there is the common case, and re-downloading 40 MB to find
	// that out would make `doti install` slow for everybody.
	if installed, err := filepath.Glob(filepath.Join(dir, "*NerdFont*.ttf")); err == nil && len(installed) > 0 {
		a.Report.Line(MarkOK, fmt.Sprintf("nerd-font (%d faces already installed)", len(installed)))
		return nil
	}
	if a.DryRun {
		a.Report.Line(MarkChange, fmt.Sprintf("would install the Nerd Font %s into %s", nerdFontVersion, dir))
		return nil
	}

	done := a.Report.Working("downloading the Nerd Font " + nerdFontVersion)
	archive, err := a.fetchVerifiedFont(ctx)
	if err != nil {
		done(MarkWarn, "nerd-font: "+err.Error())
		// Not fatal. A missing font is a cosmetic problem - icons in the
		// prompt - and failing the whole install over it would be wrong.
		return nil
	}
	defer os.Remove(archive)

	count, err := extractFonts(archive, dir)
	if err != nil {
		done(MarkWarn, "nerd-font: "+err.Error())
		return nil
	}
	done(MarkChange, fmt.Sprintf("nerd-font: %d faces into %s", count, dir))

	if a.Platform == manifest.Linux && a.Runner.Look("fc-cache") {
		// Without this the font is on disk and invisible to every application
		// until the cache is rebuilt, which looks exactly like a failed
		// install.
		if err := a.Runner.Run(ctx, "fc-cache", "-f", dir); err != nil {
			a.Report.Line(MarkWarn, "fc-cache failed; run it by hand to see the font")
		}
	}
	if a.Platform == manifest.Windows {
		// Windows reads per-user fonts from this directory but only lists
		// registered ones. Said rather than silently half-done.
		a.Report.Line(MarkSkip,
			"Windows needs the faces registered - open the folder and Install for all users")
	}
	return nil
}

// fetchVerifiedFont downloads the archive and checks it against the release's
// published SHA-256, returning the path to a temp file.
func (a *App) fetchVerifiedFont(ctx context.Context) (string, error) {
	base := a.fontBaseURL()

	want, err := a.fetchChecksum(ctx, base+"/"+nerdFontChecksums, nerdFontArchive)
	if err != nil {
		return "", err
	}

	body, err := httpGet(ctx, base+"/"+nerdFontArchive)
	if err != nil {
		return "", err
	}
	defer body.Close()

	file, err := os.CreateTemp("", "doti-font-*.zip")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	path := file.Name()

	digest := sha256.New()
	// Hashed as it is written, so the bytes are never read twice, and capped
	// so a wrong URL cannot fill the disk.
	written, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(body, maxFontArchive))
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(path)
		return "", fmt.Errorf("downloading the archive: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(path)
		return "", closeErr
	}
	if written >= maxFontArchive {
		os.Remove(path)
		return "", fmt.Errorf("archive is larger than %d bytes - refusing", maxFontArchive)
	}

	got := hex.EncodeToString(digest.Sum(nil))
	if got != want {
		os.Remove(path)
		return "", fmt.Errorf("checksum mismatch for %s - refusing to install", nerdFontArchive)
	}
	return path, nil
}

// fetchChecksum pulls one filename's hash out of a `sha256  name` listing.
func (a *App) fetchChecksum(ctx context.Context, url, name string) (string, error) {
	body, err := httpGet(ctx, url)
	if err != nil {
		return "", err
	}
	defer body.Close()

	scanner := bufio.NewScanner(io.LimitReader(body, 1<<20))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Exactly two fields, and the name matched whole: a prefix match
		// would let JetBrainsMono.zip be satisfied by another entry's hash.
		if len(fields) == 2 && fields[1] == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", nerdFontChecksums, err)
	}
	return "", fmt.Errorf("%s has no entry for %s", nerdFontChecksums, name)
}

// httpGet is a GET with a deadline and a status check.
func httpGet(ctx context.Context, url string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("fetching %s: HTTP %d", url, response.StatusCode)
	}
	return response.Body, nil
}

// extractFonts writes the archive's .ttf files into dir, flat.
//
// Flat, by base name, and that is the security control rather than a
// convenience: a zip entry's name comes from whoever built the archive, and
// an entry called `../../.zshrc` would otherwise be written outside the
// target directory. Taking only filepath.Base removes the traversal rather
// than trying to detect it - there is no path left to traverse with.
func extractFonts(archivePath, dir string) (int, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return 0, fmt.Errorf("opening the archive: %w", err)
	}
	defer reader.Close()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("creating %s: %w", dir, err)
	}

	var count int
	for _, entry := range reader.File {
		name := archiveBase(entry.Name)
		if name == "" {
			continue
		}
		// TTF only. The archive also carries .otf of the same faces, and
		// installing both leaves every application listing each face twice.
		if !strings.EqualFold(filepath.Ext(name), ".ttf") {
			continue
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		target, err := safeJoin(dir, name)
		if err != nil {
			return count, err
		}
		if err := extractOne(entry, target); err != nil {
			return count, err
		}
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("the archive held no .ttf files")
	}
	return count, nil
}

// archiveBase reduces a zip entry name to a bare filename.
//
// Slash semantics, and both separators normalised first, because zip entry
// names are defined to use "/" but archives built on Windows put "\\" in them
// anyway. filepath.Base would then behave differently per platform: on Linux
// it treats "\\" as an ordinary character, so `..\..\evil.ttf` survives as a
// literal filename with backslashes in it rather than being reduced. Not an
// escape, but a file named something absurd - and a difference in behaviour
// between platforms is exactly what this function exists to remove.
func archiveBase(entryName string) string {
	name := path.Base(strings.ReplaceAll(entryName, "\\", "/"))
	// path.Base returns "." for an empty input and "/" for a bare slash;
	// neither is a file, and ".." must never be one.
	if name == "." || name == ".." || name == "/" {
		return ""
	}
	return name
}

// safeJoin resolves name inside dir, refusing anything that would land
// outside it.
//
// Redundant today, and deliberately kept anyway. archiveBase already reduces
// an entry to a bare filename, so nothing reaching here has a path in it -
// but that is an invariant of a helper three functions away, and:
//
//   - a static analyser cannot see it. CodeQL flagged this extraction as
//     go/zipslip precisely because the sanitisation was inferable rather
//     than enforced at the point of use.
//   - neither can the next person. Someone "simplifying" archiveBase to
//     filepath.Base, or extending this loop to preserve subdirectories,
//     reintroduces the hole with nothing failing.
//
// The check is containment of the *resolved* path rather than a substring
// scan for "..", because ".." is a legal substring of a legal filename
// ("..." is a valid name) and containment is the property actually wanted.
func safeJoin(dir, name string) (string, error) {
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("archive entry has no usable filename")
	}
	// Either separator: a zip built on Windows puts "\\" in entry names even
	// though the format specifies "/".
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("archive entry %q still contains a path separator", name)
	}

	root := filepath.Clean(dir)
	target := filepath.Join(root, name)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q would be written outside %s", name, root)
	}
	return target, nil
}

func extractOne(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("reading %s from the archive: %w", entry.Name, err)
	}
	defer source.Close()

	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	defer file.Close()

	// Capped: entry.UncompressedSize64 is a claim made by the archive, so a
	// bomb would be believed. The limit is what actually bounds it.
	if _, err := io.Copy(file, io.LimitReader(source, maxFontFile)); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	return nil
}
