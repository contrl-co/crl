package decisionrecord_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/contrl-co/crl/decisionrecord"
	crlcrypto "github.com/contrl-co/crl/internal/crypto"
)

// The evaluator must see an empty environment, not merely miss one canary
// variable. The fixture dumps everything it was given to a path baked in at
// build time, since it has no environment to find that path through.
func TestEvaluatorReceivesEmptyEnvironment(test *testing.T) {
	directory := test.TempDir()
	dump := filepath.Join(directory, "environment.txt")
	source := fmt.Sprintf(`package main
import ("os"; "strings")
func main() {
    os.WriteFile(%q, []byte(strings.Join(os.Environ(), "\n")), 0600)
    os.Exit(1)
}`, dump)
	sourcePath := filepath.Join(directory, "main.go")
	if err := os.WriteFile(sourcePath, []byte(source), 0600); err != nil {
		test.Fatal(err)
	}
	program := filepath.Join(directory, "evaluator.exe")
	build := exec.Command("go", "build", "-o", program, sourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		test.Fatalf("build evaluator environment fixture: %v: %s", err, output)
	}
	executable, err := os.ReadFile(program)
	if err != nil {
		test.Fatal(err)
	}
	test.Setenv("CRL_VERIFIER_SECRET_CANARY", "must-not-reach-the-evaluator")
	record := recomputationRecord(test, "sha256:"+crlcrypto.DigestBytes(executable), func(map[string]any) {})
	artifact := decisionrecord.EvaluatorArtifact{ID: "github.com/contrl-co/crl", Path: program}
	if err := record.VerifyRecomputation(context.Background(), artifact); !errors.Is(err, decisionrecord.ErrRecomputation) {
		test.Fatalf("fixture evaluator must be refused: %v", err)
	}
	environment, err := os.ReadFile(dump)
	if err != nil {
		test.Fatalf("fixture evaluator did not run: %v", err)
	}
	if len(environment) != 0 {
		test.Fatalf("evaluator received environment:\n%s", environment)
	}
}
