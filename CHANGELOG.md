# Changelog

All notable changes to the CRL toolchain. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [semver](https://semver.org). Note that **language editions
version independently of the toolchain**: toolchain releases may ship
weekly; the v1 edition's compilation contract does not change (see
[spec/editions.md](spec/editions.md)).

## [Unreleased]

### Security

- Compilation is now collision-safe against Unicode and invalid-UTF-8
  attacks: strings are folded to NFC in the compiler (the layer that
  also compares them, not the hasher), and invalid UTF-8 is rejected in
  source text, escaped literals, and the struct API. Previously two
  byte-distinct programs could share a bundle hash while evaluating to
  opposite decisions.
- Quorum evaluation is three-valued (Kleene): a missing subject is
  *unknown*, so a negated absent subject (`not <absent>`) can no longer
  read as a clearance. Freshness taints a whole quorum: a stale subject
  fails the check as expired regardless of boolean structure.
- Evaluation fails closed in more places: a stale, future-dated, or
  unprovable observation is `EXPIRED`; a zero-value or round-tripped
  compiled bundle no longer authorizes; an empty program authorizes
  nothing.
- Release provenance: the cosign identity is pinned in verification, so
  a keyless signature from an unrelated identity cannot pass.

### Changed

- **Bundle hashes moved for bundles containing `<`/`>` comparisons.**
  The canonical JSON encoder no longer HTML-escapes `<`, `>`, and `&`,
  so the operator `>=` is hashed as `>=` rather than `>=`. This is
  a deliberate pre-freeze v1 correction (see
  [spec/editions.md](spec/editions.md)); six of the nine example bundle
  hashes changed. The canonical JSON encoding is documented honestly as
  purpose-built, not a conformant RFC 8785 implementation.
- Global final-policy predicates now render before the rules in
  canonical text, so a bundle with a global policy round-trips through
  `crlc fmt`.
- A cluster reports the most severe outcome among its unauthorized
  member rules instead of a generic `DENIED`.

### Fixed

- Newly rejected (each could previously produce a misleading or unsafe
  bundle): duplicate `target`, `package`, `bundle`, and cluster `rules`
  statements; a `count()` threshold above the subject count; a
  block-only global final policy; invalid UTF-8 and oversized source.
- The struct API and the text parser now accept the same quorum
  expressions, so a struct-built bundle can no longer emit canonical
  text the compiler refuses to recompile.
- Unspaced `+` between quorum or cluster subjects (`ca+cb >= 2`,
  `rules ra+rb`) compiles again, matching the spaced form; RFC3339
  positive UTC offsets in timestamps are accepted.
- `crlc fmt -w` formats a symlink's target instead of replacing the
  link; `crlc` exits non-zero when a stdout write fails; `crlc compile`
  and `crlc eval` validate `-format`.
- Lint diagnostics `CRL209` (unreferenced signal) and `CRL210` (global
  predicate scoped into a rule by the carve-out) had their false
  positives tightened: `CRL210` fires only on genuinely mixed
  indentation, and `CRL209` spares a presence-referenced collector's
  sole, structurally required signal.

### Added

- Initial public release of the CRL toolchain:
  - `crlc` CLI: `lint`, `compile`, `fmt`, `eval`, `graph`, `version`.
  - Go library API: `Compile`/`CompileEdition`, `Format`, `Lint`,
    `Graph`, `Evaluate`/`EvaluateAt` with the five-outcome contract.
  - CRL v1 language specification, verified against the compiler in
    CI (docs-lint extracts and compiles every example).
  - Example corpus with golden hashes as the determinism gate.
  - VS Code extension: highlighting, snippets, compiler-backed
    diagnostics.
  - Reproducible release pipeline: pinned toolchain, keyless cosign
    signatures, SHA-256 checksums, CycloneDX SBOM, vulnerability
    scanning.
