# Editions

An **edition** is a frozen compilation contract: a bundle declares its
edition (`crl v1`), and the toolchain reproduces that edition's exact
canonical text and bundle hash forever. The compiler retains every
edition it has ever shipped; new or changed semantics may only alter
output in a new edition — never inside an existing one.

## Why editions exist

Content addressing only works if hashes are stable across toolchain
releases. A stored decision that pins `sha256:<bundle-hash>` must still
verify years later, with a newer `crlc`. Editions make that a compiler
guarantee instead of a policy hope: within an edition, a toolchain
update that changes any canonical byte or any hash for existing
source is a bug, and CI treats it as one: a golden corpus of sources
and hashes is compiled on every change, on every platform a CI runner
is attached for, and any diff fails the build. All release binaries
are cross-compiled from one pinned toolchain with CGO disabled, so a
platform cannot diverge without the corpus catching it where it runs.

## Declaring an edition

The source header names the edition:

```text
crl v1
```

`crlc compile --edition v1` additionally pins the edition at the
command line; the compile fails if the requested edition is not
implemented by the toolchain. `v1` is the default and currently the
only edition.

## Status of v1

**v1 is the current edition and is not yet declared frozen.** Until
the freeze is declared, backwards-incompatible corrections may still
land in v1 itself; after it, they can only land in a v2. The freeze
criterion is external reliance: v1 freezes no later than the first
production decision relied on by an external party. From that point:

- the v1 grammar, semantics, canonicalization, and hash encoding in
  this specification are permanent;
- any semantic change, however small, is a new edition;
- `crlc` keeps compiling v1 sources bit-identically, forever.

The freeze will be recorded here and in the changelog when declared.

## Compatibility promises (already in force)

Regardless of freeze status, within any released toolchain version:

- the same source at the same edition produces byte-identical
  canonical text and hash on every supported OS and architecture;
- compilation and evaluation consult no environment: no locale, no
  timezone database surprises, no `time.Now()`, no map-iteration
  order, no CGO.
