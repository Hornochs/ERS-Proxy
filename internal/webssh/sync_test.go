package webssh

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// legacySource derives dial_legacy.go from dial_modern.go: the same code on
// internal/legacyssh instead of golang.org/x/crypto/ssh.
func legacySource(modern []byte) []byte {
	s := string(modern)
	s = strings.Replace(s, "package webssh\n\n// dial_legacy.go is generated from this file (see TestLegacyInSync): keep the\n// two identical apart from the import and the function names.\n",
		"// Code generated from dial_modern.go by TestLegacyInSync (ERSPROXY_UPDATE=1 go test -run TestLegacyInSync ./internal/webssh). DO NOT EDIT.\n\npackage webssh\n", 1)
	s = strings.Replace(s, `"golang.org/x/crypto/ssh"`, `ssh "github.com/lan-dot-party/ers-proxy/internal/legacyssh"`, 1)
	s = strings.ReplaceAll(s, "dialModern", "dialLegacy")
	s = strings.ReplaceAll(s, "mapErrModern", "mapErrLegacy")
	return []byte(s)
}

// TestLegacyInSync keeps the legacy dialer identical to the modern one.
func TestLegacyInSync(t *testing.T) {
	modern, err := os.ReadFile("dial_modern.go")
	if err != nil {
		t.Fatal(err)
	}
	want := legacySource(modern)
	if os.Getenv("ERSPROXY_UPDATE") == "1" {
		if err := os.WriteFile("dial_legacy.go", want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile("dial_legacy.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("dial_legacy.go is out of date: run ERSPROXY_UPDATE=1 go test -run TestLegacyInSync ./internal/webssh")
	}
}

// TestLegacySSHOnlyForWebSSH: only this package's legacy dialer may use the
// frozen SSH copy (and its internal packages).
func TestLegacySSHOnlyForWebSSH(t *testing.T) {
	root := filepath.Join("..", "..")
	const legacy = "github.com/lan-dot-party/ers-proxy/internal/legacyssh"
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "dist" || name == "bin") {
				return filepath.SkipDir
			}
			if filepath.ToSlash(path) == "../../internal/legacyssh" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !strings.HasPrefix(p, legacy) {
				continue
			}
			rel := filepath.ToSlash(path)
			if rel != "../../internal/webssh/dial_legacy.go" && !strings.HasPrefix(rel, "../../internal/webssh/") {
				t.Errorf("%s imports %s: only internal/webssh may use the frozen SSH copy", rel, p)
			}
			if strings.HasPrefix(rel, "../../internal/webssh/") && !strings.HasSuffix(rel, "_test.go") &&
				rel != "../../internal/webssh/dial_legacy.go" && !strings.HasPrefix(rel, "../../internal/webssh/sshtest/") {
				t.Errorf("%s imports %s: only dial_legacy.go (and tests) may", rel, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
