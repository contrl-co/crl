# Changelog

All notable changes to the CRL toolchain. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions
follow [semver](https://semver.org). Note that **language editions
version independently of the toolchain**: toolchain releases may ship
weekly; the v1 edition's compilation contract does not change (see
[spec/editions.md](spec/editions.md)).

## [Unreleased]

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
