package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Import paths that must never appear in this module.
//
// charmbracelet/wish's SCP middleware has an unfixed path-traversal flaw:
// fileSystemHandler.prefixed() runs filepath.Clean on the client-supplied
// path, notices it does not already start with the root, and joins it to the
// root anyway - so "../../../etc/passwd" cleans to itself, joins to
// "/etc/passwd", and is served. The same call sits behind Write and Mkdir, so
// it reads *and* writes, and the filenames come off the SCP wire through a
// regex that accepts any string.
//
// There is no version to upgrade to. The advisory covers everything through
// v1.4.7, which is the newest v1 release ("go list -m -versions" agrees), and
// lists the patched version as none. The v2 line under charm.land carries the
// same code.
//
// So the mitigation is architectural: this server registers a bubbletea
// program, recover, activeterm and logging, and no file-transfer handler at
// all. There is nothing for a traversal to traverse. Two independent things
// would have to change for that to stop being true - an import here, and a
// middleware in main.go - and neither would fail to compile, fail another
// test, or look wrong in review. Hence this.
//
// If a genuine need for file transfer ever arrives, it does not arrive by
// deleting this test. It arrives by validating the path against the root in
// our own handler, and by writing the traversal cases from the advisory as
// tests that expect a refusal.
var forbiddenImports = []string{
	"github.com/charmbracelet/wish/scp",
	"charm.land/wish/v2/scp",
}

func TestNoSCPMiddleware(t *testing.T) {
	fset := token.NewFileSet()

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// bin/ holds a build artifact; there is no source under it.
			if entry.Name() == "bin" || strings.HasPrefix(entry.Name(), ".") {
				if path == "." {
					return nil
				}
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for _, forbidden := range forbiddenImports {
				if value == forbidden || strings.HasPrefix(value, forbidden+"/") {
					t.Errorf(
						"%s imports %q, which has an unpatched path traversal "+
							"(arbitrary file read and write) in every released "+
							"version of wish - see the comment on forbiddenImports",
						path, value,
					)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module sources: %v", err)
	}
}

// A walk that finds nothing proves nothing, so check the walk itself reaches
// the file we care most about. Without this, a typo in the skip rules would
// turn the test above into a permanent, silent pass.
func TestSecurityWalkReachesMain(t *testing.T) {
	found := false
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && path == "main.go" {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module sources: %v", err)
	}
	if !found {
		t.Fatal("the import scan never reached main.go")
	}
}
