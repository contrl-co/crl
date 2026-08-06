// Package decisionrecord strictly parses CRL decision-record v1 documents.
//
// This package does not verify signature mathematics, key authorization,
// trust policy, replay policy, or application context. A caller must not treat
// a successfully parsed record as an authorization decision.
package decisionrecord
