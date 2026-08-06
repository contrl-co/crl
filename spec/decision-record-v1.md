# CRL Decision Record v1

This document and [`decision-record-v1.schema.json`](decision-record-v1.schema.json)
define the portable CRL decision record. The schema is closed by default: an
unknown field is invalid unless it is inside the explicit `extensions` object.

## Required content

A record binds:

- one record ID, creation time, domain, subject, and correlation ID;
- the raw CRL source, its source hash, canonical text, canonical compiled
  bundle, bundle hash, and edition;
- the exact facts, supplier/source provenance, and evaluation instant;
- one of the five CRL outcomes and the complete evaluation trace;
- the trace hash, record hash, and zero or more role-bound signature envelopes.

All digest fields are lowercase hexadecimal SHA-256 values without a prefix.
All strings and JSON property names must be valid UTF-8. Timestamps use the one
spelling produced by `t.UTC().Format(time.RFC3339Nano)`: UTC `Z`, at most nine
fractional digits, and no trailing fractional zero. Numbers outside the exact
IEEE-754 integer range `[-(2^53-1), 2^53-1]` are invalid.

`canonical_bundle` is a JSON string containing the exact canonical compiled
bundle bytes defined by [`canonical-form.md`](canonical-form.md). It is a
string, rather than a second parsed object, so a verifier can compare the bytes
that were hashed without another serializer changing them.

Each fact used for a decision, except an `observed_at.*` metadata key, has a
provenance entry naming its supplier, source locator, source-document SHA-256,
and observation time. `observed_at.<fact>` uses the provenance entry for
`<fact>`. Duplicate provenance entries for one fact are invalid.

Evidence cannot be observed after the decision that used it, and a record
cannot be created before its evaluation. Consumers must reject
`provenance.observed_at > evaluation.at` and `evaluation.at > created_at`.

## Canonical bytes and hashes

`CanonicalJSON` below means the deterministic encoder in
[`canonical-form.md`](canonical-form.md): sorted keys, no insignificant
whitespace, no HTML escaping, duplicate-key rejection, and the fixed CRL number
rules. It is not RFC 8785/JCS.

The existing compilation hashes do not change:

```text
source_hash = SHA256(raw UTF-8 source bytes)
bundle_hash = SHA256(canonical compiled-bundle JSON bytes)
```

The trace and record use explicit domain separation:

```text
trace_hash = SHA256("crl-decision-trace/v1" || 0x00 || CanonicalJSON(trace))

unsigned_record = every top-level field except record_hash and signatures
record_hash = SHA256("crl-decision-record/v1" || 0x00 || CanonicalJSON(unsigned_record))
```

Array order is significant. Producers must emit provenance in fact-name order
and signatures in `(role, key_id)` order. Reordering either array changes the
record hash or wire bytes and is rejected as non-canonical even if the JSON
Schema accepts the values.

## Signature envelope

Decision-record v1 recognizes Ed25519 only. A role is bound into the signature;
trust policy decides which role names and key IDs are authorized for a given
domain. Schema-valid signatures are not automatically trusted.

To create a signature, derive this payload from the wire envelope and the
top-level `record_hash`:

```json
{
  "algorithm": "ed25519",
  "key_id": "issuer-2026-01",
  "role": "issuer",
  "signed_at": "2026-08-06T15:00:00Z",
  "record_hash": "<record_hash>"
}
```

Then sign:

```text
"crl-decision-signature/v1" || 0x00 || CanonicalJSON(envelope_without_signature)
```

The wire envelope contains `algorithm`, `key_id`, `role`, `signed_at`, and
`signature`; it does not repeat `record_hash`. `signature` is the canonical
padded base64 encoding of the 64-byte Ed25519 signature. Duplicate `(role,
key_id)` pairs are invalid. Key discovery, allowed roles, thresholds, validity
windows, revocation, and compromise recovery belong to the separately
versioned [decision trust policy](decision-trust-policy-v1.md); absence of a
caller-approved, currently active policy fails trust verification closed.

## Validation and verification order

A consumer must stop at the first failed layer:

1. Reject invalid UTF-8, malformed JSON, duplicate keys, trailing data, and
   unsupported schema versions.
2. Validate the JSON Schema with format and content assertions enabled. Reject
   unknown fields, invalid times, unsafe numbers, malformed hashes/signatures,
   and unsupported algorithms.
3. Reject duplicate fact provenance, duplicate signature identities,
   non-canonical array order, and any extension namespace the active policy
   does not recognize.
4. Recompute and compare source, bundle, trace, and record hashes.
5. Resolve exactly one caller-approved trust policy for the record domain that
   is active at the verifier's trusted clock, then validate its pinned hash,
   roles, key status, signature time, revocation, and Ed25519 signatures.
6. Recompile the source and independently re-evaluate the exact facts at
   `evaluation.at`; require the canonical bundle, trace, and outcome to match.
7. Apply replay and context policy using the record ID, domain, subject,
   correlation ID, and evaluation time.

These layers must be reported separately:

- **structural validity**: bytes and schema are valid;
- **integrity**: all hashes match;
- **signature validity**: signature math succeeds;
- **trust**: roles and keys are authorized and current;
- **decision correctness**: recompilation and evaluation reproduce the record.

A valid signature proves only that a key signed the bound record. It does not,
by itself, prove trusted authority or a correct CRL decision.

## Compatibility and extensions

`schema_version` is exact. A v1 verifier must reject any other value; it must
not guess or downgrade. New required behavior needs a new record version.

Optional data may appear only in `extensions`, under a namespaced key such as
`contrl.co/workflow`. Extension values are included in `record_hash`. A verifier
that does not recognize an extension may report the record structurally valid,
but must not report trust or decision correctness when policy marks that
extension as required. Extensions cannot redefine any v1 field, hash, signing
bytes, outcome, or verification step.
