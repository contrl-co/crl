// Package decisionrecord strictly parses CRL decision-record v1 documents and
// verifies their deterministic integrity and decision correctness.
//
// This package does not verify signature mathematics, key authorization,
// trust policy, replay policy, or application context. A caller must not treat
// successful deterministic verification as an authorization decision.
package decisionrecord
