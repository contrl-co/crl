package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/contrl-co/crl/decisionrecord"
)

type verificationLayer struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type recordVerification struct {
	Verified            bool              `json:"verified"`
	Structure           verificationLayer `json:"structure"`
	Integrity           verificationLayer `json:"integrity"`
	SignatureMath       verificationLayer `json:"signature_math"`
	Trust               verificationLayer `json:"trust"`
	DecisionCorrectness verificationLayer `json:"decision_correctness"`
	ReplayContext       verificationLayer `json:"replay_context"`
}

func runVerify(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("crlc verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "text", "output format: text or json")
	keys := map[string]ed25519.PublicKey{}
	flags.Func("public-key", "public key for signature mathematics as key_id=hex (repeatable; not a trust policy)", func(value string) error {
		identifier, encoded, found := strings.Cut(value, "=")
		if !found || strings.TrimSpace(identifier) == "" || identifier != strings.TrimSpace(identifier) {
			return errors.New("expected key_id=hex")
		}
		if _, exists := keys[identifier]; exists {
			return errors.New("duplicate public key identifier")
		}
		publicKey, err := hex.DecodeString(encoded)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return errors.New("expected 32-byte Ed25519 public key in hex")
		}
		keys[identifier] = publicKey
		return nil
	})
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *format != "text" && *format != "json" {
		if _, err := fmt.Fprintf(stderr, "crlc verify: unsupported format %q\n", *format); err != nil {
			return 2
		}
		return 2
	}
	body, code := readSource(flags.Args(), stdin, stderr, "crlc verify")
	if code != 0 {
		return code
	}
	report := checkRecord([]byte(body), keys)
	if *format == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return 1
		}
	} else {
		for _, layer := range []struct {
			name  string
			check verificationLayer
		}{
			{"structure", report.Structure}, {"integrity", report.Integrity},
			{"signature_math", report.SignatureMath}, {"trust", report.Trust},
			{"decision_correctness", report.DecisionCorrectness}, {"replay_context", report.ReplayContext},
		} {
			if _, err := fmt.Fprintf(stdout, "%s: %s %s\n", layer.name, layer.check.Status, layer.check.Detail); err != nil {
				return 1
			}
		}
		if _, err := fmt.Fprintln(stdout, "Decision verification refused: required verification layers remain unverified."); err != nil {
			return 1
		}
	}
	// Mathematical checks alone can never satisfy the contract's trust,
	// independent-party, pinned evaluator and replay requirements.
	return 1
}

func checkRecord(body []byte, keys map[string]ed25519.PublicKey) recordVerification {
	unverified := verificationLayer{Status: "unverified", Detail: "earlier layer did not pass"}
	report := recordVerification{
		Structure: unverified, Integrity: unverified, SignatureMath: unverified,
		Trust:               verificationLayer{Status: "unverified", Detail: "approved trust-policy verification is unavailable"},
		DecisionCorrectness: verificationLayer{Status: "unverified", Detail: "pinned evaluator replay is unavailable"},
		ReplayContext:       verificationLayer{Status: "unverified", Detail: "replay and context policy verification is unavailable"},
	}
	record, err := decisionrecord.Parse(body)
	if err != nil {
		report.Structure = verificationLayer{Status: "failed", Detail: err.Error()}
		return report
	}
	report.Structure = verificationLayer{Status: "passed"}
	if err := record.VerifyIntegrity(); err != nil {
		report.Integrity = verificationLayer{Status: "failed", Detail: err.Error()}
		return report
	}
	report.Integrity = verificationLayer{Status: "passed"}
	if len(keys) == 0 {
		report.SignatureMath = verificationLayer{Status: "unverified", Detail: "no public keys supplied"}
		return report
	}
	if err := record.VerifySignatureMath(keys); err != nil {
		report.SignatureMath = verificationLayer{Status: "failed", Detail: err.Error()}
		return report
	}
	report.SignatureMath = verificationLayer{Status: "passed", Detail: "public-key mathematics only; no authority established"}
	return report
}
