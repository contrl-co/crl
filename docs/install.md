# Installing crlc

Releases ship as static binaries for Linux, macOS, and Windows
(amd64/arm64), built reproducibly with a pinned toolchain
(`CGO_ENABLED=0 -trimpath`), and published with SHA-256 checksums, a
CycloneDX SBOM, and keyless cosign signatures.

## Verify first

Verification is part of installation, not an optional extra — an
unverified binary is a bypass of everything CRL's determinism buys.
Every release publishes `checksums.txt` with a cosign signature and
certificate. To verify a download:

```sh
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp '^https://github\.com/contrl-co/crl/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum --check --ignore-missing checksums.txt
```

The `0.1.0-beta04`, `0.1.0-beta05`, and `0.1.0` releases were signed by
the original GitLab pipeline before their artifacts were copied byte for
byte to GitHub. The install script accepts that historical identity only
when `checksums.txt` matches one of those three migration-validated
manifests. Every later release must match the GitHub Actions identity
shown above.

## Homebrew (macOS / Linux)

```sh
brew tap contrl-co/tap https://github.com/contrl-co/homebrew-tap.git
brew install crlc
```

The formula pins the release checksum; Homebrew verifies it on
install.

## Install script

```sh
curl -fsSL https://raw.githubusercontent.com/contrl-co/crl/main/packaging/install.sh | sh
```

The script downloads the release for your OS/arch, requires `cosign`, verifies
the checksum manifest's keyless identity, verifies the archive SHA-256, and
installs to `~/.local/bin` (override with `CRLC_INSTALL_DIR`). It fails closed
when `cosign` is absent. `CRLC_ALLOW_UNVERIFIED=1` is an explicit emergency
checksum-only fallback and emits a warning; it must not be used for routine
installation. Pipe-to-shell is a convenience; the audit-grade path is
downloading the script, reading it, installing `cosign`, and running the script
yourself.

## From source

```sh
go install github.com/contrl-co/crl/cmd/crlc@latest
```

Building from source with the pinned Go toolchain reproduces the
released binaries bit-for-bit (see `.goreleaser.yaml` for the exact
flags).

## Editor support

The VS Code extension (syntax highlighting, snippets, lint-on-type
via `crlc`) lives in [editors/vscode](../editors/vscode/); install it from the
marketplace as `contrl.crl-language-support` (or, until the first
marketplace release, install the `.vsix` from the release artifacts or
build it locally with `npx @vscode/vsce package`).
