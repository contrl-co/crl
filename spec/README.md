# CRL v1 Language Specification

CRL (CONTRL Rule Language) is a small, deterministic language for
authorization rules over collected evidence. A CRL bundle declares what
evidence must exist, how fresh it must be, and what must be true of it;
evaluation produces exactly one of five outcomes.

This specification describes CRL v1 as implemented by the `crlc`
toolchain in this repository. The compiler is the source of truth:
every claim here is checked against it, and every example compiles
(CI extracts and lints each fenced `crl` block).

## Contents

| Document | Covers |
|---|---|
| [syntax.md](syntax.md) | Lexical rules, source forms, and the full grammar |
| [semantics.md](semantics.md) | Evaluation: the five outcomes, freshness, quorum, temporal predicates, composition |
| [canonical-form.md](canonical-form.md) | Canonical text, canonical JSON, and content addressing |
| [editions.md](editions.md) | The edition contract and the status of v1 |

## The contract in three sentences

1. **Deterministic**: the same source always compiles to the same
   canonical text and the same SHA-256 hash, on every platform.
2. **Five outcomes**: evaluation returns exactly one of `AUTHORIZED`,
   `DENIED`, `BLOCKED`, `INSUFFICIENT_EVIDENCE`, or `EXPIRED` — and
   every consumer must handle all five.
3. **Fail closed**: evidence whose freshness cannot be proven never
   satisfies a rule.

## A complete example

```crl
crl v1
package examples.permits
bundle permit.application

rule permit_application
	target permit.application
	collector application_file municipality file_upload from /bundles/application.json
		signal application_complete bool from application.complete ttl 30d
	collector registry_check land_registry api from /bundles/registry.json
		signal permit_hold_active bool from permit.hold_active ttl 30d
	collector reviewer_attest reviewer attestation from /bundles/review.json
		signal reviewer_approved bool from review.approved ttl 30d
	need application_complete == true
	block permit_hold_active
	quorum 2 of 3 application_file registry_check reviewer_attest
```

Three independent collectors each contribute a signal; the rule needs a
complete application, is blocked by an active hold, and requires
evidence from at least two of the three sources.

## Conventions in this spec

- Fenced blocks tagged `crl` are complete sources and must lint clean.
- Fenced blocks tagged `crl` whose first line is the comment
  `# docs-lint: expect-error` are deliberate counterexamples and must
  FAIL to compile — CI asserts that too.
- Fenced blocks tagged `text` are fragments or schematic grammar, not
  compilable sources.
