# Canonical Form and Content Addressing

Determinism is the point of CRL: the same source always produces the
same canonical text and the same hash, on every platform, forever
within an edition. This document specifies exactly what is normalized
and exactly what is hashed.

## Two hashes

Compilation produces two distinct digests:

| Digest | Over | Purpose |
|---|---|---|
| **source hash** | the raw source bytes, exactly as submitted | provenance of the authored text |
| **bundle hash** | the canonical JSON encoding of the compiled bundle | the bundle's content address |

Everything that refers to "the hash" of a bundle means the bundle
hash. Sources that differ only in comments, whitespace, layout style,
identifier case, or sugar (`N of M` vs `count()`) compile to the
**same** bundle hash; any semantic difference changes it.

## Canonicalization

Compilation normalizes the object model before rendering or hashing:

- identifiers (names, kinds, targets, fields, units, packages, bundle
  names) are lowercased and trimmed;
- every string on the hash path — identifiers, string literals,
  collector sources, signal source fields — is folded to Unicode NFC,
  and invalid UTF-8 is rejected, so a program's bytes are fixed before
  it is hashed;
- the version is always the canonical `crl/v1`;
- source locators are rendered bare when they match
  `[A-Za-z0-9_./:@?-]+` and quoted otherwise;
- durations are lowercased and re-derived from their literal
  (`ttl 24h` stays `24h`; it is not converted to seconds in the text,
  but its second count is fixed in the compiled form);
- `expires <duration>` canonicalizes to `ttl <duration>`;
- **declaration order is preserved** for rules, clusters, collectors,
  signals, and predicates — authors control reading order, and
  reordering declarations is a semantic edit that changes the hash;
- within a single quorum predicate, `count()` subjects are sorted
  lexicographically, and boolean expressions normalize commutative
  operand order — `quorum b & a` and `quorum a & b` are the same
  predicate;
- the `N of M` and `a + b >= n` count spellings desugar to the
  `count(...) >= n` form;
- abstract rules are expanded into their concrete heirs and disappear;
- comments are discarded.

## Canonical text

The canonical text is a complete CRL source rendered from the
normalized bundle with a fixed layout: the `crl v1` header, `package`
and `bundle` lines when present, then any global predicates, then each
rule (tab-indented body, signals nested under their collectors), then
each cluster. Global predicates render *before* the rules: emitted after
a rule body they would sit at column 0 following it, where the rule-body
carve-out would absorb them into that rule and the text would no longer
re-compile to itself. The canonical text of a bundle:

- re-compiles, and re-compiles **to itself** (formatting is a fixed
  point);
- re-compiles to the same bundle hash;
- is what `crlc fmt` prints and what `crlc compile` emits.

## Canonical JSON and the bundle hash

The bundle hash is the hex-encoded SHA-256 of a canonical JSON
encoding of the compiled bundle. The encoding is fixed and
purpose-built — deliberately **not** a conformant RFC 8785 (JSON
Canonicalization Scheme) implementation (see the note on numbers below):

- object keys sorted lexicographically at every level (by code point;
  every object key CRL emits is a fixed ASCII schema key, so byte,
  code-point, and UTF-16 order all coincide);
- no whitespace between tokens;
- numbers are emitted exactly as the compiler's canonical rendering
  produced them — the encoder itself performs no reformatting. Number
  literals are normalized earlier, during compilation: `1.0`
  canonicalizes to `1`, `1e2` to `100`, `4.50` to `4.5`, so the bytes
  hashed are the canonical rendering, never the authored spelling.
  CRL's single numeric type is IEEE-754 double; whole numbers render
  with exact integer digits, and an integer literal beyond ±2^53 is
  rejected at compile time rather than silently rounded;
- strings are emitted verbatim, with no HTML escaping of `<`, `>`, or
  `&`;
- duplicate object keys rejected outright — two inputs that differ
  only in duplicate-key content must never canonicalize to the same
  bytes.

CRL deliberately does not adopt RFC 8785/JCS: its number rule
re-serializes values through the ECMAScript shortest-form algorithm
(for example `1E30` → `1e+30`), whose output is not guaranteed to
match the compiler's canonical rendering byte for byte. The hash is
defined over exactly the bytes the compiler renders, so it is
reproduced by following this specification (or reusing this
toolchain), not by running an off-the-shelf JCS library.

Unicode normalization is **not** performed by the encoder. The compiler
folds every hashed string to Unicode NFC and rejects invalid UTF-8
*before* the bundle is hashed (see Canonicalization above), so the bytes
that are hashed are exactly the bytes the evaluator compares — a hash
identifies one program. Normalizing inside the encoder instead would let
two programs that decide differently share a hash.

The compiled-bundle document that gets encoded contains: the version,
optional package and bundle names, the rules (name, target,
collectors with their signals and expiry contracts, predicates), the
clusters, and the global predicates — i.e. everything evaluation
depends on, and nothing else. Source positions, comments, and layout
never reach the hash.

## What this buys

- **Audit**: a decision record can pin the exact rule content by hash;
  anyone can recompile published source and check the hash matches.
- **Anti-drift**: an independent implementation that follows this
  canonicalization reproduces the bundle hash from the published
  source; two toolchains (or the same one on different platforms) agree
  byte-for-byte. CI compiles a golden corpus and fails on any hash
  difference.
- **Dedup/identity**: a bundle's identity is its content, not its
  file name or formatting.
