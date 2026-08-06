// Package decisionrecord strictly parses CRL decision-record v1 documents and
// separately verifies integrity, pinned signature trust, and deterministic
// decision correctness.
//
// Trust verification does not prove decision correctness, and decision
// verification does not prove trust. Neither applies replay or application
// context policy. A relying party must run every required layer before acting.
package decisionrecord
