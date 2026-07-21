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
and `bundle` lines when present, then each rule (tab-indented body,
signals nested under their collectors), each cluster, and any global
predicates. The canonical text of a bundle:

- re-compiles, and re-compiles **to itself** (formatting is a fixed
  point);
- re-compiles to the same bundle hash;
- is what `crlc fmt` prints and what `crlc compile` emits.

## Canonical JSON and the bundle hash

The bundle hash is the hex-encoded SHA-256 of a canonical JSON
encoding of the compiled bundle. The encoding is
[RFC 8785 (JSON Canonicalization Scheme)](https://www.rfc-editor.org/rfc/rfc8785)
for the values CRL produces:

- object keys sorted lexicographically at every level (RFC 8785 orders
  by UTF-16 code units; every object key CRL emits is a fixed ASCII
  schema key, where UTF-16, UTF-8, and code-point order all coincide);
- no whitespace between tokens;
- numbers are IEEE-754 double precision — CRL has a single numeric type
  and integers larger than 2^53 are not representable exactly;
- strings are emitted verbatim, with no HTML escaping of `<`, `>`, or
  `&`;
- duplicate object keys rejected outright — two inputs that differ
  only in duplicate-key content must never canonicalize to the same
  bytes.

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
- **Anti-drift**: because the encoding is RFC 8785, an independent
  implementation in any language can reproduce the bundle hash from the
  published source; two toolchains (or the same one on different
  platforms) agree byte-for-byte. CI compiles a golden corpus and fails
  on any hash difference.
- **Dedup/identity**: a bundle's identity is its content, not its
  file name or formatting.
