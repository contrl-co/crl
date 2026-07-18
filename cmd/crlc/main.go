// crlc is the CRL toolchain: lint, compile, fmt, eval, and graph in
// one binary.
//
//	crlc lint    [-format text|json] [-fail-on error|warning|info|none] [-canonical] [-quiet] [path ...]
//	crlc compile [-edition v1] [-format text|json] [path]
//	crlc fmt     [path] | crlc fmt -w path ...
//	crlc eval    -facts facts.json [-at rfc3339] [-format text|json] [-require-authorized] [path]
//	crlc graph   [path]
//	crlc version
//
// Every command reads stdin when no path is given (or when the path is
// "-"). Exit codes: 0 on success, 1 when the command's check fails
// (lint threshold met, compile error, eval below -require-authorized),
// 2 on usage or I/O errors.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	crl "gitlab.com/contrl-group/crl"
	"gitlab.com/contrl-group/crl/internal/crllint"
)

// version is stamped at release time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	command, rest := args[0], args[1:]
	switch command {
	case "lint":
		return runLint(rest, stdin, stdout, stderr)
	case "compile":
		return runCompile(rest, stdin, stdout, stderr)
	case "fmt":
		return runFmt(rest, stdin, stdout, stderr)
	case "eval":
		return runEval(rest, stdin, stdout, stderr)
	case "graph":
		return runGraph(rest, stdin, stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "crlc %s (editions: %s)\n", version, crl.EditionV1)
		return 0
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "crlc: unknown command %q\n", command)
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: crlc <command> [flags] [path ...]

commands:
  lint      lint CRL sources and report CRL### diagnostics
  compile   compile to canonical text and content hash
  fmt       print (or rewrite) the canonical form
  eval      evaluate a bundle against facts
  graph     emit the deterministic rule graph as JSON
  version   print toolchain version and supported editions

Commands read stdin when no path is given. Run a command with -h for
its flags.
`)
}

// --- lint -----------------------------------------------------------

type lintOutput struct {
	OK      bool             `json:"ok"`
	Reports []crllint.Report `json:"reports"`
}

func runLint(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("crlc lint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "output format: text or json")
	failOn := flags.String("fail-on", "error", "exit 1 on diagnostics at or above: error, warning, info, or none")
	includeCanonical := flags.Bool("canonical", false, "include canonical compiled CRL in JSON output")
	quiet := flags.Bool("quiet", false, "suppress text output when lint succeeds")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	threshold, failEnabled, err := parseFailOn(*failOn)
	if err != nil {
		fmt.Fprintf(stderr, "crlc lint: %v\n", err)
		return 2
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "crlc lint: unsupported format %q\n", *format)
		return 2
	}

	opts := crllint.Options{IncludeCanonical: *includeCanonical}
	reports, readErr := lintInputs(flags.Args(), stdin, opts)
	if readErr != nil {
		fmt.Fprintf(stderr, "crlc lint: %v\n", readErr)
		return 2
	}
	failed := false
	for _, report := range reports {
		if failEnabled && crllint.MeetsThreshold(report.Diagnostics, threshold) {
			failed = true
			break
		}
	}

	switch *format {
	case "json":
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(lintOutput{OK: !failed, Reports: reports})
	default:
		writeLintText(stdout, reports, *quiet)
	}
	if failed {
		return 1
	}
	return 0
}

func lintInputs(args []string, stdin io.Reader, opts crllint.Options) ([]crllint.Report, error) {
	if len(args) == 0 || (len(args) == 1 && args[0] == "-") {
		source, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return []crllint.Report{crllint.LintSource("<stdin>", string(source), opts)}, nil
	}
	paths, err := expandInputs(args)
	if err != nil {
		return nil, err
	}
	reports := make([]crllint.Report, 0, len(paths))
	for _, path := range paths {
		report, err := crllint.LintFile(path, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func expandInputs(args []string) ([]string, error) {
	var paths []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			paths = append(paths, arg)
			continue
		}
		if err := filepath.WalkDir(arg, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "node_modules", "vendor":
					return filepath.SkipDir
				default:
					return nil
				}
			}
			if strings.EqualFold(filepath.Ext(path), ".crl") {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("no CRL files found")
	}
	return paths, nil
}

func parseFailOn(value string) (crllint.Severity, bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error":
		return crllint.SeverityError, true, nil
	case "warning":
		return crllint.SeverityWarning, true, nil
	case "info":
		return crllint.SeverityInfo, true, nil
	case "none":
		return crllint.SeverityInfo, false, nil
	default:
		return "", false, fmt.Errorf("unsupported -fail-on value %q", value)
	}
}

func writeLintText(w io.Writer, reports []crllint.Report, quiet bool) {
	count := 0
	for _, report := range reports {
		for _, diagnostic := range report.Diagnostics {
			count++
			fmt.Fprintf(
				w,
				"%s:%d:%d: %s %s: %s\n",
				diagnostic.Path,
				diagnostic.Line,
				diagnostic.Column,
				diagnostic.Severity,
				diagnostic.Code,
				diagnostic.Message,
			)
		}
	}
	if count == 0 && !quiet {
		if len(reports) == 1 {
			fmt.Fprintf(w, "%s: ok\n", reports[0].Path)
			return
		}
		fmt.Fprintf(w, "%d CRL files: ok\n", len(reports))
	}
}

// --- compile --------------------------------------------------------

type compileOutput struct {
	OK            bool   `json:"ok"`
	Edition       string `json:"edition,omitempty"`
	SourceHash    string `json:"source_hash,omitempty"`
	CanonicalText string `json:"canonical_text,omitempty"`
	Hash          string `json:"hash,omitempty"`
	Error         string `json:"error,omitempty"`
}

func runCompile(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("crlc compile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	edition := flags.String("edition", crl.EditionV1, "edition to compile under")
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	source, code := readSource(flags.Args(), stdin, stderr, "crlc compile")
	if code != 0 {
		return code
	}
	compiled, err := crl.CompileEdition(source, *edition)
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		if err != nil {
			_ = encoder.Encode(compileOutput{OK: false, Error: err.Error()})
			return 1
		}
		_ = encoder.Encode(compileOutput{
			OK:            true,
			Edition:       compiled.Edition,
			SourceHash:    compiled.SourceHash,
			CanonicalText: compiled.CanonicalText,
			Hash:          compiled.Hash,
		})
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "crlc compile: %v\n", err)
		return 1
	}
	// The trailing hash line is a CRL comment, so the output as a whole
	// is still valid, lintable CRL.
	fmt.Fprintln(stdout, compiled.CanonicalText)
	fmt.Fprintf(stdout, "# sha256:%s\n", compiled.Hash)
	return 0
}

// --- fmt ------------------------------------------------------------

func runFmt(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("crlc fmt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	write := flags.Bool("w", false, "rewrite the file in place instead of printing")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	paths := flags.Args()
	if *write && len(paths) == 0 {
		fmt.Fprintln(stderr, "crlc fmt: -w requires a file path")
		return 2
	}
	if !*write {
		source, code := readSource(paths, stdin, stderr, "crlc fmt")
		if code != 0 {
			return code
		}
		formatted, err := crl.Format(source)
		if err != nil {
			fmt.Fprintf(stderr, "crlc fmt: %v\n", err)
			return 1
		}
		fmt.Fprint(stdout, formatted)
		return 0
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "crlc fmt: %v\n", err)
			return 2
		}
		formatted, err := crl.Format(string(raw))
		if err != nil {
			fmt.Fprintf(stderr, "crlc fmt: %s: %v\n", path, err)
			return 1
		}
		if formatted == string(raw) {
			continue
		}
		if err := os.WriteFile(path, []byte(formatted), 0o644); err != nil {
			fmt.Fprintf(stderr, "crlc fmt: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, path)
	}
	return 0
}

// --- eval -----------------------------------------------------------

func runEval(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("crlc eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	factsPath := flags.String("facts", "", "path to a JSON object of facts (required)")
	at := flags.String("at", "", "evaluation clock as an RFC3339 timestamp; omit to evaluate without a clock (fails closed on freshness)")
	format := flags.String("format", "text", "output format: text or json")
	requireAuthorized := flags.Bool("require-authorized", false, "exit 1 unless the result is AUTHORIZED")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *factsPath == "" {
		fmt.Fprintln(stderr, "crlc eval: -facts is required")
		return 2
	}
	factsRaw, err := os.ReadFile(*factsPath)
	if err != nil {
		fmt.Fprintf(stderr, "crlc eval: %v\n", err)
		return 2
	}
	var facts crl.Facts
	if err := json.Unmarshal(factsRaw, &facts); err != nil {
		fmt.Fprintf(stderr, "crlc eval: parse facts: %v\n", err)
		return 2
	}
	now := time.Time{}
	if *at != "" {
		parsed, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			fmt.Fprintf(stderr, "crlc eval: parse -at: %v\n", err)
			return 2
		}
		now = parsed
	}
	source, code := readSource(flags.Args(), stdin, stderr, "crlc eval")
	if code != 0 {
		return code
	}
	compiled, err := crl.Compile(source)
	if err != nil {
		fmt.Fprintf(stderr, "crlc eval: %v\n", err)
		return 1
	}
	evaluation := compiled.EvaluateAt(facts, now)

	switch *format {
	case "json":
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(evaluation)
	default:
		fmt.Fprintln(stdout, evaluation.Result)
		for _, check := range evaluation.Checks {
			if check.Passed {
				continue
			}
			where := check.Field
			if check.QuorumExpression != "" {
				where = check.QuorumExpression
			}
			fmt.Fprintf(stdout, "  %s %s: %s\n", check.Kind, where, check.Reason)
		}
	}
	if *requireAuthorized && evaluation.Result != crl.Authorized {
		return 1
	}
	return 0
}

// --- graph ----------------------------------------------------------

func runGraph(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("crlc graph", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	source, code := readSource(flags.Args(), stdin, stderr, "crlc graph")
	if code != 0 {
		return code
	}
	result, err := crl.Graph(source)
	if err != nil {
		fmt.Fprintf(stderr, "crlc graph: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(result)
	return 0
}

// --- shared ---------------------------------------------------------

func readSource(paths []string, stdin io.Reader, stderr io.Writer, command string) (string, int) {
	switch len(paths) {
	case 0:
		raw, err := io.ReadAll(stdin)
		if err != nil {
			fmt.Fprintf(stderr, "%s: read stdin: %v\n", command, err)
			return "", 2
		}
		return string(raw), 0
	case 1:
		path := paths[0]
		if path == "-" {
			raw, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintf(stderr, "%s: read stdin: %v\n", command, err)
				return "", 2
			}
			return string(raw), 0
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", command, err)
			return "", 2
		}
		return string(raw), 0
	default:
		fmt.Fprintf(stderr, "%s: expected one path (or stdin)\n", command)
		return "", 2
	}
}
