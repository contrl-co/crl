package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

const portableFixture = "../../spec/testdata/decision-record-v1/valid/authorized.json"

func portableKeys(test *testing.T) []string {
	test.Helper()
	body, err := os.ReadFile("../../spec/testdata/decision-record-v1/policy.json")
	if err != nil {
		test.Fatal(err)
	}
	var policy struct {
		Keys map[string]struct {
			PublicKey string `json:"public_key"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &policy); err != nil {
		test.Fatal(err)
	}
	identifiers := make([]string, 0, len(policy.Keys))
	for identifier := range policy.Keys {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	args := []string{"verify", "-format", "json"}
	for _, identifier := range identifiers {
		args = append(args, "-public-key", identifier+"="+policy.Keys[identifier].PublicKey)
	}
	return args
}

func TestVerifyRefusesUntrustedFixtureDespiteValidSignatures(test *testing.T) {
	args := append(portableKeys(test), portableFixture)
	code, stdout, stderr := runCLI(test, "", args...)
	if code != 1 {
		test.Fatalf("incomplete verification must refuse: code=%d stderr=%s", code, stderr)
	}
	var report recordVerification
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		test.Fatal(err)
	}
	if report.Verified || report.Structure.Status != "passed" || report.Integrity.Status != "passed" || report.SignatureMath.Status != "passed" {
		test.Fatalf("incorrect available checks: %s", stdout)
	}
	if report.Trust.Status != "unverified" || report.DecisionCorrectness.Status != "unverified" || report.ReplayContext.Status != "unverified" {
		test.Fatalf("fixture keys granted authority: %s", stdout)
	}
}

func TestVerifyReportsFirstFailedLayer(test *testing.T) {
	body, err := os.ReadFile(portableFixture)
	if err != nil {
		test.Fatal(err)
	}
	for _, scenario := range []struct {
		name, body, structure, integrity, signature string
	}{
		{"malformed", `{}`, "failed", "unverified", "unverified"},
		{"tampered", strings.Replace(string(body), `"approved": true`, `"approved": false`, 1), "passed", "failed", "unverified"},
		{"missing keys", string(body), "passed", "passed", "unverified"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			code, stdout, _ := runCLI(test, scenario.body, "verify", "-format", "json", "-")
			var report recordVerification
			if err := json.Unmarshal([]byte(stdout), &report); err != nil {
				test.Fatal(err)
			}
			if code != 1 || report.Verified || report.Structure.Status != scenario.structure || report.Integrity.Status != scenario.integrity || report.SignatureMath.Status != scenario.signature {
				test.Fatalf("incorrect refusal: code=%d %s", code, stdout)
			}
		})
	}
	args := portableKeys(test)
	args[4] = strings.Split(args[4], "=")[0] + "=" + strings.Repeat("00", 32)
	code, stdout, _ := runCLI(test, string(body), args...)
	var report recordVerification
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		test.Fatal(err)
	}
	if code != 1 || report.SignatureMath.Status != "failed" || report.Verified {
		test.Fatalf("substituted key accepted: %s", stdout)
	}
}

func TestVerifyUsageAndTextRefusal(test *testing.T) {
	for _, args := range [][]string{
		{"verify", "-format", "other"},
		{"verify", "-public-key", "broken"},
		{"verify", "-public-key", "key=00"},
		{"verify", "-public-key", "key=" + strings.Repeat("gg", 32)},
		{"verify", "-public-key", "key=" + strings.Repeat("00", 32), "-public-key", "key=" + strings.Repeat("00", 32)},
		{"verify", "missing-record.json"},
		{"verify", portableFixture, portableFixture},
	} {
		if code, _, _ := runCLI(test, "", args...); code != 2 {
			test.Fatalf("usage/input error must exit 2: %v: %d", args, code)
		}
	}
	code, stdout, _ := runCLI(test, "", "verify", portableFixture)
	if code != 1 || !strings.Contains(stdout, "trust: unverified") || !strings.Contains(stdout, "Decision verification refused") {
		test.Fatalf("text output hid refusal: %d %s", code, stdout)
	}
}
