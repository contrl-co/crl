package crl

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const expectErrorMarker = "# docs-lint: expect-error"

// TestDocsCRLBlocks extracts every fenced ```crl block from every
// markdown file in the repository and lints it. Blocks must compile
// clean — unless their first line is the expect-error marker, in which
// case they must FAIL to compile. A documentation example that stops
// compiling (or a counterexample that starts compiling) fails the
// build.
func TestDocsCRLBlocks(t *testing.T) {
	var files []string
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no markdown files found")
	}

	blocks := 0
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, block := range crlBlocks(string(raw)) {
			blocks++
			label := fmt.Sprintf("%s:%d", path, block.line)
			expectError := strings.TrimSpace(firstLine(block.source)) == expectErrorMarker
			report := Lint(label, block.source)
			hasError := false
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Severity == "error" {
					hasError = true
					break
				}
			}
			switch {
			case expectError && !hasError:
				t.Errorf("%s: block is marked expect-error but compiles", label)
			case !expectError && hasError:
				t.Errorf("%s: documentation example does not compile: %s", label, firstErrorMessage(report))
			}
		}
	}
	if blocks == 0 {
		t.Fatal("no fenced crl blocks found in any markdown file")
	}
	t.Logf("checked %d crl blocks across %d markdown files", blocks, len(files))
}

type docBlock struct {
	line   int
	source string
}

func crlBlocks(markdown string) []docBlock {
	var blocks []docBlock
	lines := strings.Split(markdown, "\n")
	inBlock := false
	var start int
	var current []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if trimmed == "```crl" {
				inBlock = true
				start = i + 2 // 1-based, first line inside the fence
				current = nil
			}
			continue
		}
		if trimmed == "```" {
			inBlock = false
			blocks = append(blocks, docBlock{line: start, source: strings.Join(current, "\n") + "\n"})
			continue
		}
		current = append(current, line)
	}
	return blocks
}

func firstLine(source string) string {
	if index := strings.IndexByte(source, '\n'); index >= 0 {
		return source[:index]
	}
	return source
}

func firstErrorMessage(report LintReport) string {
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Severity == "error" {
			return fmt.Sprintf("%s %s", diagnostic.Code, diagnostic.Message)
		}
	}
	return "unknown error"
}
