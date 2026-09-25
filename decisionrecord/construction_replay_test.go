package decisionrecord_test

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/contrl-co/crl/decisionrecord"
	crlcrypto "github.com/contrl-co/crl/internal/crypto"
)

func TestConstructedSignedRecordRecomputesWithActualCLI(test *testing.T) {
	program := filepath.Join(test.TempDir(), "crlc.exe")
	if output, err := exec.Command("go", "build", "-o", program, "../cmd/crlc").CombinedOutput(); err != nil {
		test.Fatalf("build fixture artifact: %v: %s", err, output)
	}
	executable, err := os.ReadFile(program)
	if err != nil {
		test.Fatal(err)
	}
	input := constructionInput(test)
	input.Evaluation.Evaluator.Revision = "sha256:" + crlcrypto.DigestBytes(executable)
	record, err := decisionrecord.NewUnsigned(input)
	if err != nil {
		test.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{}
	// These keys belong to this test, not to two independent organizations.
	for index, role := range []string{"issuer", "reviewer"} {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(index + 1)}, ed25519.SeedSize))
		keyID := "local-fixture-" + role
		record, err = record.Sign(decisionrecord.SigningIdentity{KeyID: keyID, Role: role, SignedAt: input.CreatedAt}, key)
		if err != nil {
			test.Fatal(err)
		}
		keys[keyID] = key.Public().(ed25519.PublicKey)
	}
	if err := record.VerifySignatureMath(keys); err != nil {
		test.Fatal(err)
	}
	if err := record.VerifyRecomputation(test.Context(), decisionrecord.EvaluatorArtifact{ID: input.Evaluation.Evaluator.ID, Path: program}); err != nil {
		test.Fatal(err)
	}
}
