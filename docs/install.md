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
  --certificate-identity-regexp 'gitlab.com/contrl-group/crl' \
  --certificate-oidc-issuer https://gitlab.com \
  checksums.txt
sha256sum --check --ignore-missing checksums.txt
```

## Homebrew (macOS / Linux)

```sh
brew tap contrl-group/crl https://gitlab.com/contrl-group/homebrew-crl.git
brew install crlc
```

The formula pins the release checksum; Homebrew verifies it on
install.

## Install script

```sh
curl -fsSL https://gitlab.com/contrl-group/crl/-/raw/main/packaging/install.sh | sh
```

The script downloads the release for your OS/arch, **verifies the
SHA-256 against the signed checksums file**, and installs to
`~/.local/bin` (override with `CRLC_INSTALL_DIR`). Pipe-to-shell is a
convenience; the audit-grade path is downloading the script, reading
it, and running it yourself.

## From source

```sh
go install gitlab.com/contrl-group/crl/cmd/crlc@latest
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
