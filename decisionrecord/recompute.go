package decisionrecord

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	crlcrypto "github.com/contrl-co/crl/internal/crypto"
)

var ErrRecomputation = errors.New("decision record: recomputation failed")

// EvaluatorArtifact identifies a local crlc executable selected by the caller.
// Neither field is a resolver: record content never selects a path or download.
// The caller must approve this implementation and artifact for its intended use.
type EvaluatorArtifact struct {
	ID   string
	Path string
}

// VerifyRecomputation checks that an artifact pinned by SHA-256 reproduces the
// recorded compilation and evaluation. It does not establish trust, understand
// required policy extensions or apply replay/context policy. Consumers must
// complete those checks in the contract's order before accepting a decision.
// Git revisions require a separate approved build-to-artifact mapping and are
// refused here; this method never substitutes the current compiler.
func (record *Record) VerifyRecomputation(ctx context.Context, artifact EvaluatorArtifact) (result error) {
	if err := record.VerifyIntegrity(); err != nil {
		return err
	}
	rule := record.object("rule")
	if rule["edition"] != "v1" {
		return fmt.Errorf("%w: unsupported evaluator CLI edition", ErrRecomputation)
	}
	evaluation := record.object("evaluation")
	identity := evaluation["evaluator"].(map[string]any)
	revision := identity["revision"].(string)
	if artifact.ID == "" || identity["id"] != artifact.ID || !strings.HasPrefix(revision, "sha256:") {
		return fmt.Errorf("%w: unavailable evaluator identity or artifact revision", ErrRecomputation)
	}
	executable, err := os.ReadFile(artifact.Path)
	if err != nil {
		return fmt.Errorf("%w: read selected evaluator: %v", ErrRecomputation, err)
	}
	if "sha256:"+crlcrypto.DigestBytes(executable) != revision {
		return fmt.Errorf("%w: evaluator artifact digest mismatch", ErrRecomputation)
	}
	directory, err := os.MkdirTemp("", "crl-recompute-")
	if err != nil {
		return fmt.Errorf("%w: private working directory: %v", ErrRecomputation, err)
	}
	defer func() {
		if err := os.RemoveAll(directory); err != nil {
			result = errors.Join(result, fmt.Errorf("%w: remove private inputs: %v", ErrRecomputation, err))
		}
	}()
	// Execute the bytes that were hashed, never the original replaceable path.
	// The .exe suffix also works on Unix and supports Windows process creation.
	program := filepath.Join(directory, "crlc.exe")
	if err := os.WriteFile(program, executable, 0500); err != nil {
		return fmt.Errorf("%w: copy selected evaluator: %v", ErrRecomputation, err)
	}
	run := func(arguments ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, program, arguments...)
		command.Dir = directory
		command.Stdin = strings.NewReader(rule["source"].(string))
		output, err := command.Output()
		if err != nil {
			// Compiler diagnostics may contain private source or fact values.
			return nil, fmt.Errorf("%w: evaluator command failed: %v", ErrRecomputation, err)
		}
		return output, nil
	}
	compiled, err := run("compile", "-edition", rule["edition"].(string), "-format", "proto", "-")
	if err != nil {
		return err
	}
	if err := record.compareCompilation(compiled); err != nil {
		return err
	}
	facts, err := crlcrypto.CanonicalJSON(evaluation["facts"])
	if err != nil {
		return fmt.Errorf("%w: encode facts: %v", ErrRecomputation, err)
	}
	factsPath := filepath.Join(directory, "facts.json")
	if err := os.WriteFile(factsPath, facts, 0600); err != nil {
		return fmt.Errorf("%w: write private facts: %v", ErrRecomputation, err)
	}
	output, err := run("eval", "-facts", factsPath, "-at", evaluation["at"].(string), "-format", "json", "-")
	if err != nil {
		return err
	}
	trace, err := strictDocument(output)
	if err != nil {
		return fmt.Errorf("%w: invalid evaluator trace", ErrRecomputation)
	}
	actual, err := crlcrypto.CanonicalJSON(trace)
	if err != nil {
		return fmt.Errorf("%w: encode evaluator trace", ErrRecomputation)
	}
	expected, err := crlcrypto.CanonicalJSON(evaluation["trace"])
	if err != nil || !bytes.Equal(actual, expected) {
		return fmt.Errorf("%w: public trace mismatch", ErrRecomputation)
	}
	if trace.(map[string]any)["result"] != evaluation["outcome"] {
		return fmt.Errorf("%w: outcome mismatch", ErrRecomputation)
	}
	return nil
}

func (record *Record) compareCompilation(envelope []byte) error {
	rule := record.object("rule")
	expected := map[uint64]any{1: rule["edition"], 2: rule["source_hash"], 3: rule["canonical_text"], 4: rule["canonical_bundle"], 5: rule["bundle_hash"]}
	seen := map[uint64]bool{}
	for len(envelope) > 0 {
		tag, consumed := binary.Uvarint(envelope)
		if consumed <= 0 || tag&7 != 2 {
			return fmt.Errorf("%w: invalid compilation envelope", ErrRecomputation)
		}
		envelope = envelope[consumed:]
		size, consumed := binary.Uvarint(envelope)
		if consumed <= 0 || size > uint64(len(envelope)-consumed) {
			return fmt.Errorf("%w: invalid compilation field length", ErrRecomputation)
		}
		envelope = envelope[consumed:]
		field := tag >> 3
		if seen[field] || expected[field] != string(envelope[:size]) {
			return fmt.Errorf("%w: compilation field %d mismatch", ErrRecomputation, field)
		}
		seen[field] = true
		envelope = envelope[size:]
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("%w: incomplete compilation envelope", ErrRecomputation)
	}
	return nil
}
