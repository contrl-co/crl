package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// crlc fmt -w must format the file a symlink points at, not replace the
// symlink with a regular file.
func TestFmtWriteFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.crl")
	link := filepath.Join(dir, "link.crl")
	messy := "# messy\n" + strings.ReplaceAll(goodSource, "\t", "    ")
	if err := os.WriteFile(real, []byte(messy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	code, _, stderr := runCLI(t, "", "fmt", "-w", link)
	if code != 0 {
		t.Fatalf("want exit 0, got %d (%s)", code, stderr)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link must remain a symlink, got mode %v err %v", fi.Mode(), err)
	}
	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "crl v1\n") {
		t.Fatal("the real target must be formatted, not the symlink replaced")
	}
}
