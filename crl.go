// Package crl is the public API of the CRL toolchain: compile, lint,
// format, evaluate, and graph CRL source.
//
// CRL (CONTRL Rule Language) is a small, deterministic language for
// authorization rules over collected evidence. Compilation is
// content-addressed: the same source always produces the same
// canonical text and the same SHA-256 bundle hash, on every platform.
//
// The API surface is deliberately narrow. The compiler's internal
// syntax tree, object model, and IR are not exported; programs interact
// with a compiled bundle only through its canonical text, its hash, and
// evaluation. This is what lets editions stay frozen: nothing outside
// this package can depend on compiler internals.
package crl

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	lang "gitlab.com/contrl-group/crl/internal/crl"
	"gitlab.com/contrl-group/crl/internal/crlgraph"
	"gitlab.com/contrl-group/crl/internal/crllint"
)

// EditionV1 is the current (and only) CRL edition. An edition is a
// frozen compilation contract: within one edition, the same source
// compiles to byte-identical canonical text and an identical hash,
// forever. Semantic changes only ever land in a new edition.
const EditionV1 = "v1"

// ErrUnknownEdition is returned by CompileEdition for any edition this
// toolchain does not implement.
var ErrUnknownEdition = errors.New("crl: unknown edition")

// Result is one of the five evaluation outcomes. Every consumer of CRL
// evaluations must handle all five spellings.
type Result string

const (
	// Authorized: every required condition proved against present,
	// fresh evidence. The only outcome that advances anything.
	Authorized Result = "AUTHORIZED"
	// Denied: a required condition evaluated against present, fresh
	// evidence and does not hold.
	Denied Result = "DENIED"
	// Blocked: an explicit blocker is active.
	Blocked Result = "BLOCKED"
	// InsufficientEvidence: a required fact is absent or a quorum is
	// unmet.
	InsufficientEvidence Result = "INSUFFICIENT_EVIDENCE"
	// Expired: a required signal exists but its freshness cannot be
	// proven (stale, missing or unparseable observation time, or no
	// evaluation clock).
	Expired Result = "EXPIRED"
)

// Facts is the evidence a bundle is evaluated against. Keys are
// normalized signal names; a signal's observation time, when known, is
// supplied under "observed_at.<signal>".
type Facts = map[string]any

// Compiled is a compiled CRL bundle: its canonical text and its
// content hash, plus the edition it was compiled under.
type Compiled struct {
	// Edition the bundle was compiled under (currently always "v1").
	Edition string `json:"edition"`
	// SourceHash is the SHA-256 of the raw source bytes as submitted.
	SourceHash string `json:"source_hash"`
	// CanonicalText is the normalized rendering of the bundle. It
	// re-compiles to itself and to the same Hash.
	CanonicalText string `json:"canonical_text"`
	// Hash is the hex SHA-256 of the canonical JSON encoding of the
	// compiled bundle — the bundle's content address.
	Hash string `json:"hash"`

	program lang.CompiledBundle
}

// Compile compiles CRL source under the current edition.
func Compile(source string) (Compiled, error) {
	return CompileEdition(source, EditionV1)
}

// CompileEdition compiles CRL source under an explicit edition. The
// only edition this toolchain implements is EditionV1.
func CompileEdition(source, edition string) (Compiled, error) {
	if edition != EditionV1 {
		return Compiled{}, fmt.Errorf("%w %q (this toolchain implements: %s)", ErrUnknownEdition, edition, EditionV1)
	}
	compilation, err := lang.CompileLanguage(source)
	if err != nil {
		return Compiled{}, err
	}
	return Compiled{
		Edition:       edition,
		SourceHash:    compilation.SourceHash,
		CanonicalText: compilation.CanonicalText,
		Hash:          compilation.Hash,
		program: lang.CompiledBundle{
			Program:       compilation.Bundle,
			CanonicalText: compilation.CanonicalText,
			Hash:          compilation.Hash,
		},
	}, nil
}

// Format compiles source and returns its canonical text. Formatting is
// not configurable: the canonical form is the only output, because the
// canonical bytes are what get hashed.
func Format(source string) (string, error) {
	compiled, err := Compile(source)
	if err != nil {
		return "", err
	}
	return compiled.CanonicalText + "\n", nil
}

// Evaluate evaluates the bundle against facts WITHOUT a clock, which
// fails closed on every time-dependent rule: any signal that declares
// a ttl/expires evaluates as EXPIRED. Callers that want freshness
// genuinely evaluated must use EvaluateAt with an explicit clock — and
// record that clock, because the decision is a function of it.
func (c Compiled) Evaluate(facts Facts) Evaluation {
	return c.EvaluateAt(facts, time.Time{})
}

// EvaluateAt evaluates the bundle against facts at an explicit instant.
func (c Compiled) EvaluateAt(facts Facts, now time.Time) Evaluation {
	// program is unexported and set only by the compiler, so a zero-value,
	// JSON-round-tripped, or otherwise hand-built Compiled carries an empty
	// program that would authorize vacuously. Fail closed unless the program
	// is the one this Compiled's hash addresses.
	if c.program.Hash == "" || c.program.Hash != c.Hash {
		return Evaluation{Result: InsufficientEvidence, Authorized: false}
	}
	return newEvaluation(lang.EvaluateBundleAt(c.program, facts, now))
}

// Lint lints CRL source and reports structured diagnostics. The path
// is used only to label diagnostics; pass any display name for
// non-file sources.
func Lint(path, source string) LintReport {
	return newLintReport(crllint.LintSource(path, source, crllint.Options{IncludeCanonical: true}))
}

// Graph compiles source and derives its deterministic node/edge graph
// with a layout. The same source always yields the same graph, node
// IDs included. The graph and layout are returned as JSON documents:
// the graph is a rendering projection, and JSON is its contract.
func Graph(source string) (GraphResult, error) {
	compilation, err := lang.CompileLanguage(source)
	if err != nil {
		return GraphResult{}, err
	}
	graph := crlgraph.Build(compilation.Bundle)
	graphJSON, err := json.Marshal(graph)
	if err != nil {
		return GraphResult{}, err
	}
	layoutJSON, err := json.Marshal(crlgraph.Layout(graph))
	if err != nil {
		return GraphResult{}, err
	}
	return GraphResult{
		Hash:   compilation.Hash,
		Graph:  graphJSON,
		Layout: layoutJSON,
	}, nil
}

// GraphResult is the deterministic graph projection of a compiled
// bundle: pure structure plus a computed layout, both as JSON.
type GraphResult struct {
	// Hash is the content hash of the bundle the graph was derived
	// from.
	Hash   string          `json:"hash"`
	Graph  json.RawMessage `json:"graph"`
	Layout json.RawMessage `json:"layout"`
}
