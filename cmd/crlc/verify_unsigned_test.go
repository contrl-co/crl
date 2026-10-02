package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestVerifyUnsignedRecordStaysUnverifiedWithPublicKeys(test *testing.T) {
	body, err := os.ReadFile(portableFixture)
	if err != nil {
		test.Fatal(err)
	}
	var document map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		test.Fatal(err)
	}
	document["signatures"] = []any{}
	body, err = json.Marshal(document)
	if err != nil {
		test.Fatal(err)
	}
	args := append(portableKeys(test), "-")
	code, stdout, _ := runCLI(test, string(body), args...)
	var report recordVerification
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		test.Fatal(err)
	}
	if code != 1 || report.Verified || report.Structure.Status != "passed" || report.Integrity.Status != "passed" || report.SignatureMath.Status != "unverified" {
		test.Fatalf("unsigned record with unrelated keys was reported incorrectly: code=%d %s", code, stdout)
	}
}
