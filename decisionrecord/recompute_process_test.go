package decisionrecord_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contrl-co/crl/decisionrecord"
	crlcrypto "github.com/contrl-co/crl/internal/crypto"
	"github.com/contrl-co/crl/internal/pbenvelope"
)

func evaluatorProcessFixture(test *testing.T, overflowCommand, expectedWorkDir string) (*decisionrecord.Record, decisionrecord.EvaluatorArtifact) {
	test.Helper()
	var document map[string]any
	decode(test, fixture(test, "valid/authorized.json"), &document)
	rule := document["rule"].(map[string]any)
	compiled := pbenvelope.CompiledBundle{
		Edition: rule["edition"].(string), SourceHash: rule["source_hash"].(string),
		CanonicalText: rule["canonical_text"].(string), CanonicalBundle: []byte(rule["canonical_bundle"].(string)),
		Hash: rule["bundle_hash"].(string),
	}.Marshal()
	trace, err := json.Marshal(document["evaluation"].(map[string]any)["trace"])
	if err != nil {
		test.Fatal(err)
	}
	directory := test.TempDir()
	source := fmt.Sprintf(`package main
import ("os"; "bytes"; "path/filepath"; "strings")
func main() {
    if os.Getenv("CRL_VERIFIER_SECRET_CANARY") != "" { os.Exit(17) }
    if %q != "" {
        executable, err := os.Executable()
        relative, relativeErr := filepath.Rel(%q, executable)
        if err != nil || relativeErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) { os.Exit(18) }
    }
    if os.Args[1] == %q {
        block := bytes.Repeat([]byte("x"), 65536)
        for count := 0; count < 513; count++ { if _, err := os.Stdout.Write(block); err != nil { return } }
        return
    }
    if os.Args[1] == "compile" { os.Stdout.Write([]byte(%q)) } else { os.Stdout.Write([]byte(%q)) }
}`, expectedWorkDir, expectedWorkDir, overflowCommand, compiled, trace)
	sourcePath := filepath.Join(directory, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0600); err != nil {
		test.Fatal(err)
	}
	program := filepath.Join(directory, "evaluator.exe")
	build := exec.Command("go", "build", "-o", program, sourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		test.Fatalf("build evaluator process fixture: %v: %s", err, output)
	}
	executable, err := os.ReadFile(program)
	if err != nil {
		test.Fatal(err)
	}
	revision := "sha256:" + crlcrypto.DigestBytes(executable)
	record := recomputationRecord(test, revision, func(map[string]any) {})
	privateRoot := test.TempDir()
	test.Setenv("TMPDIR", privateRoot)
	test.Setenv("TMP", privateRoot)
	return record, decisionrecord.EvaluatorArtifact{ID: "github.com/contrl-co/crl", Path: program}
}

func TestEvaluatorDoesNotInheritVerifierEnvironment(test *testing.T) {
	record, artifact := evaluatorProcessFixture(test, "", "")
	test.Setenv("CRL_VERIFIER_SECRET_CANARY", "must-not-reach-the-evaluator")
	if err := record.VerifyRecomputation(test.Context(), artifact); err != nil {
		test.Fatalf("evaluator received the verifier canary or otherwise failed: %v", err)
	}
}

func TestEvaluatorRefusesOutputOver32MiB(test *testing.T) {
	for _, command := range []string{"compile", "eval"} {
		test.Run(command, func(test *testing.T) {
			record, artifact := evaluatorProcessFixture(test, command, "")
			ctx, cancel := context.WithTimeout(test.Context(), 5*time.Second)
			defer cancel()
			err := record.VerifyRecomputation(ctx, artifact)
			if !errors.Is(err, decisionrecord.ErrRecomputation) || !strings.Contains(err.Error(), "output exceeds 32 MiB") {
				test.Fatalf("excessive output did not refuse at the process boundary: %v", err)
			}
			if ctx.Err() != nil {
				test.Fatal("output refusal waited for the caller's timeout")
			}
		})
	}
}

func TestEvaluatorUsesConfiguredWorkDirWhenDefaultTempIsUnavailable(test *testing.T) {
	workDirectory := test.TempDir()
	record, artifact := evaluatorProcessFixture(test, "", workDirectory)
	artifact.WorkDir = workDirectory
	notDirectory := filepath.Join(test.TempDir(), "not-a-directory")
	if err := os.WriteFile(notDirectory, []byte("file"), 0600); err != nil {
		test.Fatal(err)
	}
	test.Setenv("TMPDIR", notDirectory)
	if err := record.VerifyRecomputation(test.Context(), artifact); err != nil {
		test.Fatalf("evaluator did not run from the configured private work directory: %v", err)
	}
}
