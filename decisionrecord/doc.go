// Package decisionrecord strictly parses CRL decision-record v1 documents and
// separately verifies integrity, pinned signature trust, and deterministic
// decision correctness.
//
// Trust verification does not prove decision correctness, and decision
// verification does not prove trust. VerifyForUse composes every layer with a
// pinned context policy and an atomic replay store before a relying party acts.
package decisionrecord
