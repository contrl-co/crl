# CRL Decision Record v1

This document and [`decision-record-v1.schema.json`](decision-record-v1.schema.json)
define the portable CRL decision record. The schema is closed by default: an
unknown field is invalid unless it is inside the explicit `extensions` object.
Distribute the schema with its [validation dialect](decision-record-v1-dialect.json)
and register both by their declared IDs; those IDs do not require network access.
This is an unreleased contract; the earlier draft fixtures are superseded.
Compiler edition v1 and its source and bundle hashes are unchanged.

## Required content

A record binds:

- one record ID, creation time, domain, subject, and correlation ID;
- the raw CRL source, its source hash, canonical text, canonical compiled
  bundle, bundle hash, and edition;
- the exact facts, supplier/source provenance, and evaluation instant;
- the evaluator implementation and immutable revision, and the expected trust
  policy ID and digest;
- one of the five CRL outcomes and the complete evaluation trace;
- the trace hash, record hash, and zero or more role-bound signature envelopes.

All digest fields are lowercase hexadecimal SHA-256 values without a prefix.
All strings and JSON property names must be valid UTF-8 Unicode scalar values;
reject unpaired UTF-16 surrogate escapes before decoding, including in keys
and embedded bundle JSON. Timestamps use the spelling produced by
`t.UTC().Format(time.RFC3339Nano)`: UTC `Z`, at most nine
fractional digits, and no trailing fractional zero. Numbers outside the exact
IEEE-754 integer range `[-(2^53-1), 2^53-1]` are invalid.

`canonical_bundle` is a JSON string containing the exact canonical compiled
bundle bytes defined by [`canonical-form.md`](canonical-form.md). It is a
string, rather than a second parsed object, so a verifier can compare the bytes
that were hashed without another serializer changing them.

Every entry in `evaluation.facts`, except an `observed_at.*` metadata key, has a
provenance entry naming its supplier, source locator, source-document SHA-256,
and observation time. The fact names and provenance names must match exactly:
missing, extra, and duplicate provenance are invalid. `observed_at.<fact>`
requires `<fact>` to exist and its string value must equal that fact's
provenance `observed_at`. Metadata cannot have its own provenance entry.
Attribution and a source digest do not prove the source's contents or authority.
Checking a private source requires authorized access or a policy-approved
attestation; the record grants neither. Replay verifies the supplied claims,
not a calculation over unavailable source data.

## Canonical bytes and hashes

`CanonicalJSON` below encodes a JSON syntax tree, retaining each number's exact
JSON token. Object keys sort by Unicode scalar value; arrays retain order;
there is no whitespace between tokens. Duplicate decoded keys are invalid.
Numbers MUST NOT pass through floating-point parsing before hashing: `1.50`,
`1.5`, `1e2`, `100`, `1.0`, `1`, and `-0` retain their spellings. Different
spellings may deliberately have different hashes; the same wire document
always has one hash. Apply numeric bounds to the exact decimal value, before
rounding. Go consumers use `json.Decoder.UseNumber` throughout, including
trace `actual`, `expected`, and extension values. This is not RFC 8785/JCS.

Strings have no Unicode normalization on the record hash path. Escape quote
and backslash as `\"` and `\\`; use `\b`, `\t`, `\n`, `\f`, `\r` for those
controls, lowercase `\u00xx` for other U+0000–U+001F characters, and `\u2028`
and `\u2029` for those separators. Emit every other scalar as UTF-8, including
`<`, `>`, `&`, and `/`. Escaped and literal representations of the same valid
string are equivalent. Unpaired surrogates are invalid, never U+FFFD aliases.
The pinned evaluator applies NFC to fact strings for comparisons; the record
keeps the original strings and hashes them without that normalization.

The existing compilation hashes do not change:

```text
source_hash = SHA256(raw UTF-8 source bytes)
bundle_hash = SHA256(canonical compiled-bundle JSON bytes)
```

These two digests and `provenance.source_digest` are undomained SHA-256.
Never substitute them for the domain-separated trace, record, or policy hash.

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

Transport envelopes, including Protobuf, carry these canonical JSON bytes
unchanged. Protobuf serialization is not the hash input.

## Evaluator and trace

`evaluation.evaluator` names an implementation in `id` and an immutable source
revision or artifact digest in `revision`; a mutable branch or tag is invalid.
Verification must use that implementation at that revision and the recorded
language edition. Unknown or unavailable revisions fail decision verification
closed; do not substitute the latest toolchain. An identifier is not authority
to download or execute code. The verifier selects approved implementations.

The trace is the pinned evaluator's public `Evaluation` JSON, including the
numeric comparison `reference` field when present. Empty optional arrays
(`rules`, `clusters`, `global_checks`, `checks`, `members`, `providers`) are
omitted, never injected as `[]` or `null`. Nonempty arrays retain evaluator
order. Absent `actual` or `expected` is distinct from false, zero, or null.
The schema describes this projection independently of Go struct tags.
Replay compares canonical trace bytes, including numeric tokens, not
floating-point value equality.

An empty `aspect` is permitted only for an `INSUFFICIENT_EVIDENCE` trace with
`authorized: false` and no optional arrays. This represents the public API's
invalid-compiled-program refusal. It is structurally recordable, but it cannot
pass decision correctness if recompiling the recorded source produces a
different result. A failure record never authorizes an action.

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

Call this five-field object `signature_payload`: copy `algorithm`, `key_id`,
`role`, and `signed_at` from the envelope and add the top-level `record_hash`.
There are no other fields. Then sign these exact bytes:

```text
"crl-decision-signature/v1" || 0x00 || CanonicalJSON(signature_payload)
```

The wire envelope contains `algorithm`, `key_id`, `role`, `signed_at`, and
`signature`; it does not repeat `record_hash`. `signature` is the canonical
padded base64 encoding of the 64-byte Ed25519 signature. Duplicate `(role,
key_id)` pairs are invalid. Key discovery, allowed roles, thresholds, validity
windows, revocation, and compromise recovery belong to the separately
versioned trust policy; absence of that policy fails trust verification closed.

`trust_policy.id` is a resolver identifier, not a URL to fetch automatically.
`trust_policy.hash` is
`SHA256("crl-decision-trust-policy/v1" || 0x00 || policy_bytes)`, where
`policy_bytes` are the exact immutable bytes in the verifier's approved policy
store. The policy's own format and acceptance rules are separately versioned.
The verifier must require both the recorded ID and digest and independently
approve the policy for the record's domain and intended use. A producer cannot
choose a weaker policy merely by hashing it. The policy must bind the relevant
party identities, roles, mandates, purpose, validity, and revocation context.

An unsigned record may be structurally valid and have matching hashes, but
MUST fail trust. Trust requires every required role and threshold, including
at least two independent parties; multiple keys or roles of one party count
as one party. No party, including CONTRL, may complete verification alone.
Invalid signatures fail rather than being discarded to satisfy a threshold.

## Validation and verification order

A consumer must stop at the first failed layer:

1. Reject invalid UTF-8, unpaired surrogate escapes, malformed JSON, duplicate
   keys, trailing data, and unsupported schema versions.
2. Validate using the supplied dialect's required format assertions and parse
   `canonical_bundle` as a strict JSON object. Reject
   unknown fields, invalid times, unsafe numbers, malformed hashes/signatures,
   and unsupported algorithms.
3. Require the exact provenance coverage and observation-time agreement above.
   Reject duplicate signature identities and non-canonical array order.
4. Recompute and compare source, bundle, trace, and record hashes.
5. Resolve and approve the pinned trust policy, then validate authority for
   the claimed suppliers and the decision signers,
   independent-party thresholds, key status, signature time, revocation,
   required extensions, and Ed25519 signatures over `signature_payload`.
6. Use the pinned evaluator to recompile the source and independently
   re-evaluate the exact facts at `evaluation.at`; require the canonical text,
   bundle, trace, and outcome to match.
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

The schema alone does not implement these seven layers. Its dialect requires
format assertions; consumers that cannot load it or implement that vocabulary
must refuse validation. `contentMediaType` and `uniqueItems` are annotations
or whole-value constraints, not embedded-JSON parsing, provenance coverage,
identity uniqueness, or cryptographic verification. Those checks are mandatory
in the stated layers. The Go conformance harness enables content assertions
in addition to strict parsing; other consumers must perform equivalent checks.

## Compatibility and extensions

The Go `decisionrecord` package implements strict parsing, provenance coverage,
content-digest verification and Ed25519 signature mathematics for this contract.
`Parse` returns an immutable record; `Bytes` returns a copy of its canonical
wire bytes. `VerifyIntegrity` and `VerifySignatureMath` are separate checks.
Neither method approves a trust policy, counts independent parties, checks key
revocation, replays a pinned evaluator or permits an action. A caller must not
present success from these methods as trusted decision verification.

`VerifyRecomputation` accepts a caller-selected local `EvaluatorArtifact`
and a cancellation context. It uses the edition-v1 CLI protocol. For a
`sha256:` revision it copies and hashes
the executable, then runs those exact bytes in a private temporary directory.
It recompiles the embedded source and compares all five compilation-envelope
fields, then evaluates the exact fact tokens at the recorded time and compares
the complete public trace and outcome. Missing artifacts, other revision forms
and mismatches refuse; there is no download or fallback to a current compiler.
Private input files are removed on success and failure.

Recomputation alone does not approve an artifact, a policy, required extensions
or action authority. Consumers must still apply the verification order above.
The CLI continues to refuse overall verification while those layers are absent.
Implementation identity includes its entrypoint: a public API using exact
number tokens can emit a different trace from a CLI that decodes facts through
floating point. A verifier must reject that mismatch, never normalize the
recorded trace to make it pass.

`schema_version` is exact. A v1 verifier must reject any other value; it must
not guess or downgrade. New required behavior needs a new record version.
`rule.edition` identifies the language independently of the record format;
an unsupported edition fails recompilation, not schema-version negotiation.

Optional data may appear only in `extensions`, under a namespaced key such as
`contrl.co/workflow`. Extension values are included in `record_hash`. A verifier
that does not recognize an extension may report the record structurally valid,
but must not report trust or decision correctness when policy marks that
extension as required. Extensions cannot redefine any v1 field, hash, signing
bytes, outcome, or verification step.

## Conformance fixtures

The fixtures and tests under `testdata/decision-record-v1` exercise this
contract, not a production trust service. Their public keys and policy are
test-only. They cover real Ed25519 signing bytes, tampering, provenance,
Unicode, numeric tokens, all five outcomes, and the API refusal shape.
