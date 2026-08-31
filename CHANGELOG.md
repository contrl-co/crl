# Changelog

All notable changes to the CRL toolchain. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [semver](https://semver.org). Note that **language editions
version independently of the toolchain**: toolchain releases may ship
weekly; the v1 edition's compilation contract does not change (see
[spec/editions.md](spec/editions.md)).

## [Unreleased]

### Security

- The pinned Go toolchain is updated to 1.25.12 to fix the reachable
  standard-library vulnerability GO-2026-4602, and `golang.org/x/text`
  is updated to 0.39.0 to fix CVE-2026-56852 in Unicode normalization.
- Compilation is now collision-safe against Unicode and invalid-UTF-8
  attacks: strings are folded to NFC in the compiler (the layer that
  also compares them, not the hasher), and invalid UTF-8 is rejected in
  source text, escaped literals, and the struct API. Previously two
  byte-distinct programs could share a bundle hash while evaluating to
  opposite decisions.
- Quorum evaluation is three-valued (Kleene): a missing subject is
  *unknown*, so a negated absent subject (`not <absent>`) can no longer
  read as a clearance. A stale subject is unknown too — it can neither
  carry a branch nor read as cleared under negation.
- **Quorum thresholds now enforce freshness.** A quorum subject that
  names a collector is judged on the signals that collector declares;
  previously only a subject that named a signal was checked, and a
  collector name matched nothing in the signal index, so a count quorum
  consulted no expiry at all and evidence of any age satisfied it —
  evidence stamped in 1999 met a thirty-day window and authorized. A
  stale subject now reduces the count instead of being ignored, and
  instead of disqualifying a boolean expression, so "two of three
  independent sources, currently fresh" is expressible in both forms: a
  disjunction still passes on the branches whose evidence is fresh.
  An unmet quorum reports `EXPIRED` when re-observing the stale
  subjects would reach the threshold, and `INSUFFICIENT_EVIDENCE` when
  the threshold is out of reach on the evidence present. Bundle hashes
  are unaffected — this changes evaluation, not compilation.
- Evaluation fails closed in more places: a stale, future-dated, or
  unprovable observation is `EXPIRED`; a zero-value or round-tripped
  compiled bundle no longer authorizes; an empty program authorizes
  nothing.
- Release provenance: the cosign identity is pinned in verification, so
  a keyless signature from an unrelated identity cannot pass.

### Changed

- The license is declared AGPL-3.0-only in every declaration in this
  repository. The LICENSE grant notice named no version, which under AGPL
  section 14 lets recipients pick any published version, while the release
  metadata already said `AGPL-3.0-only` in three places. The notice now says
  version 3 only, and the README, CONTRIBUTING, and TRADEMARKS wording
  matches. No change for users of version 3; the change removes the option to
  adopt a future license version Contrl has not reviewed. The published
  Homebrew formula still reads `Apache-2.0`: GoReleaser generated it from the
  tag that preceded the `.goreleaser.yaml` fix, and the next release
  regenerates it.
- The canonical repository, Go module, release workflow, Homebrew tap,
  security reporting, and contributor links now use the `contrl-co`
  GitHub organization. The three releases copied from GitLab retain
  their original keyless signing identity and are accepted only through
  checksum-pinned compatibility rules; new releases use the GitHub
  Actions workload identity.
- **Bundle hashes moved for bundles containing `<`/`>` comparisons.**
  The canonical JSON encoder no longer HTML-escapes `<`, `>`, and `&`,
  so the operator `>=` is hashed as `>=` rather than `>=`. This is
  a deliberate pre-freeze v1 correction (see
  [spec/editions.md](spec/editions.md)); six of the nine example bundle
  hashes changed. The canonical JSON encoding is documented honestly as
  purpose-built, not a conformant RFC 8785 implementation.
- **Bundle hashes moved for quorum chains of three or more operands.**
  Canonicalization now flattens each maximal `&`/`|` chain and sorts
  its operands, so every grouping and order of the same operands
  shares one hash and canonical text always recompiles to itself. An
  unsorted chain that happened to be a fixed point of the old pairwise
  normalization compiles to a different hash than before — also a
  pre-freeze v1 correction (see [spec/editions.md](spec/editions.md)),
  pinned by `examples/quorum_chain_assoc.crl`.
- Global final-policy predicates now render before the rules in
  canonical text, so a bundle with a global policy round-trips through
  `crlc fmt`.
- A cluster reports the most severe outcome among its unauthorized
  member rules instead of a generic `DENIED`.

### Fixed

- Same-source collectors can no longer count as independent quorum members.
  The compiler rejects them with `CRL121`; hashes of accepted programs are
  unchanged.
- Newly rejected (each could previously produce a misleading or unsafe
  bundle): duplicate `target`, `package`, `bundle`, and cluster `rules`
  statements; a `count()` threshold above the subject count; a
  block-only or otherwise inverting global final policy — one that
  gates on a rule or cluster failing (`quorum not r`, `block r`,
  `need r == false`, `r & !r2`, including inside a cluster's own
  predicates), which would authorize with no evidence; invalid UTF-8 and
  oversized source.
- An integer literal that float64 cannot represent exactly is
  rejected rather than silently rounded to a different threshold (a
  large round value like 10^18 that is exact still compiles).
- A millisecond duration now converts to real seconds (rounded up):
  `60000ms` is `60s`, where it was previously treated as `1s` — which
  under-satisfied an `age >=` requirement.
- An unquoted `and`/`or`/`not` used as a collector source or signal
  field path stays literal instead of aliasing to `&`/`|`/`!`, which had
  collided distinct locators onto one hash.
- Associative quorum groupings (`a & (b & c)`) canonicalize to one tree,
  so the emitted canonical text always recompiles to the same hash.
- Resource bounds so an in-limit source cannot exhaust memory: an
  aggregate per-bundle quorum budget and an abstract-rule inheritance
  depth cap.
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
