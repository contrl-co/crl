# Trademark Policy

> **DRAFT — pending legal review.** This policy states intent; final
> wording, registrations, and enforcement posture require counsel
> sign-off before it can be relied on.

"**CRL**" (in the context of rule languages), "**CONTRL**",
"**CONTRL Rule Language**", the "**crlc**" tool name, and any
associated logos are trademarks of the CONTRL group. The code and
specification in this repository are licensed under AGPL-3.0; that
license grants copyright and patent rights, **not** trademark rights
— it contains no trademark grant, and AGPL-3.0 §7(e) expressly
contemplates declining to grant rights in trade names and marks.

## What you may do without asking

- Use the names truthfully to refer to this language and toolchain:
  "written in CRL", "validated with crlc", "compatible with CRL v1".
- State factually that your product consumes or produces CRL, and
  which edition it targets.
- Distribute unmodified releases of the toolchain under their
  original names.

## What requires renaming

- **A modified language or toolchain may not ship under these
  names.** If you fork the compiler and change what it accepts,
  canonicalizes, or hashes, the result is not CRL and must not be
  called CRL, crlc, or CONTRL-anything. Determinism is the language's
  core promise; the name is how users know they have it.
- Domain names, package names, and product names that lead with the
  marks (e.g. `crl-pro`, `contrl-tools`) require written permission.

## What is always disallowed

- Implying sponsorship, certification, or endorsement by CONTRL.
- Using the marks for a service that mints or verifies CRL decision
  records in a way that could be confused with the CONTRL platform.

Questions and permission requests: open an issue in this repository.
