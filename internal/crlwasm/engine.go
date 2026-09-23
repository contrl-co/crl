// Package crlwasm is the request/response engine behind the browser
// build of the CRL toolchain (cmd/crl-wasm). It answers JSON strings
// with JSON strings so the WebAssembly boundary carries one argument
// and one return value, and so every handler is testable on a normal
// host without a browser or a js/wasm toolchain.
//
// The engine calls only the public crl API: compile, format, lint,
// graph, and evaluation of a compiled bundle. That is deliberate. This
// package is compiled into a binary that is served to anyone who opens
// a page, so it can contain nothing that is not already published — the
// same reason the API surface itself is narrow.
package crlwasm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	crl "github.com/contrl-co/crl"
)

// Engine answers the browser's requests. The zero value works; Version
// is the toolchain version reported by the info function, stamped by
// the command at build time.
type Engine struct {
	Version string
	// Now supplies the evaluation clock when a request omits one. Nil
	// means time.Now, which in the browser is the visitor's clock.
	Now func() time.Time
}

// Function is one JS global the browser build installs: a name and the
// handler behind it. Each handler takes the request as a JSON string
// and returns the response as a JSON string; it never panics on bad
// input and never returns a Go error, because there is no error channel
// across the WebAssembly boundary — failures come back as {"error":...}.
type Function struct {
	Name string
	Call func(request string) string
}

// Functions lists every global the browser build installs, in a stable
// order. The names are the contract: a consumer looks them up on the
// global object after the module starts.
func (e Engine) Functions() []Function {
	return []Function{
		{Name: "contrlCompileCRL", Call: e.Compile},
		{Name: "contrlFormatCRL", Call: e.Format},
		{Name: "contrlLintCRL", Call: e.Lint},
		{Name: "contrlGraphCRL", Call: e.Graph},
		{Name: "contrlEvaluateCRL", Call: e.Evaluate},
		{Name: "contrlEngineInfo", Call: e.Info},
	}
}

// --- compile --------------------------------------------------------

type compileRequest struct {
	Source  string `json:"source"`
	Edition string `json:"edition,omitempty"`
}

type compileResponse struct {
	Edition       string          `json:"edition"`
	SourceHash    string          `json:"source_hash"`
	CanonicalText string          `json:"canonical_text"`
	Hash          string          `json:"hash"`
	Program       crl.ProgramView `json:"program"`
}

// Compile compiles source and returns its canonical text, its bundle
// hash, and the read-only view of what it declares. The hash is the
// point: it is the content address a consumer pins.
func (e Engine) Compile(request string) string {
	var in compileRequest
	if err := decode(request, &in); err != nil {
		return failure(err)
	}
	source, err := requireSource(in.Source)
	if err != nil {
		return failure(err)
	}
	edition := in.Edition
	if strings.TrimSpace(edition) == "" {
		edition = crl.EditionV1
	}
	compiled, err := crl.CompileEdition(source, edition)
	if err != nil {
		return failure(err)
	}
	return success(compileResponse{
		Edition:       compiled.Edition,
		SourceHash:    compiled.SourceHash,
		CanonicalText: compiled.CanonicalText,
		Hash:          compiled.Hash,
		Program:       compiled.Program(),
	})
}

// --- format ---------------------------------------------------------

type formatRequest struct {
	Source string `json:"source"`
}

type formatResponse struct {
	Formatted  string `json:"formatted"`
	Hash       string `json:"hash"`
	SourceHash string `json:"source_hash"`
}

// Format returns the canonical rendering of source. Formatting is not
// configurable, so the response carries the hash the formatted bytes
// compile to alongside them.
func (e Engine) Format(request string) string {
	var in formatRequest
	if err := decode(request, &in); err != nil {
		return failure(err)
	}
	source, err := requireSource(in.Source)
	if err != nil {
		return failure(err)
	}
	// Compile rather than crl.Format so the canonical text and the hash
	// it addresses come from one compilation and cannot disagree.
	compiled, err := crl.Compile(source)
	if err != nil {
		return failure(err)
	}
	return success(formatResponse{
		Formatted:  compiled.CanonicalText + "\n",
		Hash:       compiled.Hash,
		SourceHash: compiled.SourceHash,
	})
}

// --- lint -----------------------------------------------------------

type lintRequest struct {
	Path   string `json:"path,omitempty"`
	Source string `json:"source"`
}

// Lint reports CRL### diagnostics with positions. Unlike every other
// handler, a source that does not compile is a successful response
// holding the diagnostics, not an {"error":...} — reporting where the
// author went wrong is the whole job.
func (e Engine) Lint(request string) string {
	var in lintRequest
	if err := decode(request, &in); err != nil {
		return failure(err)
	}
	path := in.Path
	if strings.TrimSpace(path) == "" {
		path = "playground.crl"
	}
	return success(crl.Lint(path, in.Source))
}

// --- graph ----------------------------------------------------------

type graphRequest struct {
	Source string `json:"source"`
}

type graphResponse struct {
	SourceHash    string          `json:"source_hash"`
	CanonicalText string          `json:"canonical_text"`
	Hash          string          `json:"hash"`
	Program       crl.ProgramView `json:"program"`
	Graph         json.RawMessage `json:"graph"`
	Structure     json.RawMessage `json:"structure"`
}

// Graph returns the positioned node/edge graph of a bundle. Positions
// and edge routes are computed here, not in the renderer, so the
// diagram is a deterministic function of the source: same source, same
// coordinates, everywhere.
func (e Engine) Graph(request string) string {
	var in graphRequest
	if err := decode(request, &in); err != nil {
		return failure(err)
	}
	source, err := requireSource(in.Source)
	if err != nil {
		return failure(err)
	}
	compiled, err := crl.Compile(source)
	if err != nil {
		return failure(err)
	}
	graph, err := crl.Graph(source)
	if err != nil {
		return failure(err)
	}
	if graph.Hash != compiled.Hash {
		return failure(fmt.Errorf("graph hash %s does not address the compiled bundle %s", graph.Hash, compiled.Hash))
	}
	return success(graphResponse{
		SourceHash:    compiled.SourceHash,
		CanonicalText: compiled.CanonicalText,
		Hash:          compiled.Hash,
		Program:       compiled.Program(),
		Graph:         graph.Layout,
		Structure:     graph.Graph,
	})
}

// --- evaluate -------------------------------------------------------

type evaluateRequest struct {
	Source string    `json:"source"`
	Facts  crl.Facts `json:"facts,omitempty"`
	Now    string    `json:"now,omitempty"`
}

type evaluateResponse struct {
	SourceHash    string `json:"source_hash"`
	CanonicalText string `json:"canonical_text"`
	Hash          string `json:"hash"`
	EvaluatedAt   string `json:"evaluated_at"`
	crl.Evaluation
}

// Evaluate compiles source and evaluates it against facts at an
// instant. The clock is part of the answer, not a hidden input: the
// response echoes the instant used, because a freshness decision is a
// function of it.
func (e Engine) Evaluate(request string) string {
	var in evaluateRequest
	if err := decode(request, &in); err != nil {
		return failure(err)
	}
	source, err := requireSource(in.Source)
	if err != nil {
		return failure(err)
	}
	now, err := e.clock(in.Now)
	if err != nil {
		return failure(err)
	}
	compiled, err := crl.Compile(source)
	if err != nil {
		return failure(err)
	}
	return success(evaluateResponse{
		SourceHash:    compiled.SourceHash,
		CanonicalText: compiled.CanonicalText,
		Hash:          compiled.Hash,
		EvaluatedAt:   now.Format(time.RFC3339Nano),
		Evaluation:    compiled.EvaluateAt(in.Facts, now),
	})
}

// clock resolves the evaluation instant. An explicit RFC3339 timestamp
// wins; otherwise the host clock is used, which in a browser is the
// visitor's own clock and is why nothing evaluated here is a decision
// anyone else has to trust.
func (e Engine) clock(at string) (time.Time, error) {
	if strings.TrimSpace(at) == "" {
		if e.Now != nil {
			return e.Now().UTC(), nil
		}
		return time.Now().UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(at))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid now timestamp: %w", err)
	}
	return parsed.UTC(), nil
}

// --- info -----------------------------------------------------------

type infoResponse struct {
	Engine    string   `json:"engine"`
	Version   string   `json:"version"`
	Edition   string   `json:"edition"`
	Functions []string `json:"functions"`
}

// Info reports which toolchain and edition produced the hashes this
// engine returns, so a page can display the provenance of a hash it
// shows.
func (e Engine) Info(string) string {
	version := e.Version
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}
	functions := e.Functions()
	names := make([]string, 0, len(functions))
	for _, function := range functions {
		names = append(names, function.Name)
	}
	return success(infoResponse{
		Engine:    "crl-wasm",
		Version:   version,
		Edition:   crl.EditionV1,
		Functions: names,
	})
}

// --- envelope -------------------------------------------------------

func requireSource(source string) (string, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "", errors.New("source is required")
	}
	// The untrimmed source is compiled: leading and trailing whitespace
	// is part of the bytes the source hash addresses.
	return source, nil
}

func decode(request string, out any) error {
	body := strings.TrimSpace(request)
	if body == "" {
		body = "{}"
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	return nil
}

func success(value any) string {
	body, err := encode(value)
	if err != nil {
		return failure(err)
	}
	return body
}

func failure(err error) string {
	body, marshalErr := encode(struct {
		Error string `json:"error"`
	}{Error: err.Error()})
	if marshalErr != nil {
		return `{"error":"failed to encode response"}`
	}
	return body
}

// encode writes JSON without HTML escaping, so canonical text survives
// the round trip byte-for-byte instead of coming back with `<` and `&`
// rewritten as escapes.
func encode(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimRight(buffer.String(), "\n"), nil
}
