# Security Policy

## Scope

This repository covers the CRL language toolchain: the compiler,
linter, evaluator, `crlc` CLI, editor extension, and release
pipeline. Reports about the CONTRL platform (the hosted services that
operate CRL decisions) are out of scope here and should go through the
platform's own disclosure channel.

Security-relevant properties of this toolchain worth probing:

- **Determinism**: any input where compilation output differs across
  platforms, toolchain builds, or runs.
- **Hash integrity**: distinct bundles that canonicalize to the same
  hash, or hashes that fail to cover semantics they should.
- **Fail-closed evaluation**: any path where unproven or stale
  evidence produces `AUTHORIZED`.
- **Supply chain**: release artifacts that do not match their
  checksums/signatures, or dependency vulnerabilities.

## Reporting

Open a **confidential issue** on
`https://gitlab.com/contrl-group/crl` (Issues → New issue → tick
"This issue is confidential"). Include a minimal reproducing input
where possible; `.crl` sources and facts JSON are ideal.

Please do not disclose publicly until a fix ships or 90 days pass,
whichever comes first.

## Response targets

- Acknowledgement: within 3 business days.
- Fix SLA once a vulnerability is confirmed: **critical, 48 hours;
  high, 7 days**; others prioritized in the normal release flow.
- Dependency scanning (trivy, grype) runs on every merge request and
  on a schedule; advisories affecting released artifacts are published
  in the release notes.
