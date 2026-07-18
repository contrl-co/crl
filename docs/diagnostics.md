# Lint Diagnostics

`crlc lint` reports findings with stable codes. Errors mean the source
does not compile; warnings compile but flag risky or unidiomatic
authoring. Codes are append-only: a code never changes meaning.

## Errors (CRL1xx)

| Code | Meaning |
|---|---|
| `CRL100` | Lexical or statement-level syntax error (bad token, unterminated string, malformed statement) |
| `CRL101` | Source contains no statements |
| `CRL110` | Document-structure error (misplaced declaration, unclosed block, signal before collector, malformed inheritance) |
| `CRL120` | Compile error (unknown signal or quorum subject, type mismatch, unsupported operator, duplicate names, conflicting expiry, unreachable rule under a final policy) |

The message carries the underlying compiler error; the diagnostic is
positioned at the offending line where the compiler can attribute one.

## Warnings (CRL2xx)

| Code | Meaning |
|---|---|
| `CRL200` | Missing `crl v1` version header |
| `CRL201` | Missing `package` declaration |
| `CRL202` | Missing `bundle` name |
| `CRL203` | Multiple rules or clusters with no top-level final policy — every object must authorize, which may not be what the author meant |
| `CRL204` | Rule target has no namespace segment (prefer `permit.application` over `application`) |
| `CRL205` | Two signals in one collector map the same source field |
| `CRL206` | `ms` TTL — sub-second TTLs are not representable and canonicalize to exactly one second |
| `CRL207` | `y` TTL — a year counts as exactly 365 days, no leap-year handling; spell the intent in days if the boundary matters |
| `CRL208` | A `block` field named like an expiry flag (`*expired*`, `*_expires`) — an active blocker reports `BLOCKED`, never `EXPIRED`; declare a signal expiry or use a temporal predicate for expiry semantics |
| `CRL209` | A signal is declared but never referenced by a `need`, `block`, `quorum`, or temporal predicate — it does not affect the decision, so dropping the predicate that used it silently removes a requirement or blocker |

## Editor integration

`CRL000` is reserved for editor tooling to report that the linter
binary itself could not be run (not installed, not on PATH). It is
never produced by `crlc` itself.
