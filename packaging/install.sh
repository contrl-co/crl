#!/bin/sh
# Install crlc from a released, checksummed binary.
#
#   curl -fsSL https://raw.githubusercontent.com/contrl-co/crl/main/packaging/install.sh | sh
#
# Environment:
#   CRLC_VERSION      release tag to install (default: latest)
#   CRLC_INSTALL_DIR  target directory (default: ~/.local/bin)
#
# The script downloads the release archive and checksums.txt, verifies
# the archive's SHA-256 against the checksums file, and — when cosign
# is installed — verifies the checksums file's keyless signature before
# trusting it. Without cosign it proceeds on the published SHA-256 alone
# and says so; install cosign for the full chain.

set -eu

PROJECT_URL="https://github.com/contrl-co/crl"
VERSION="${CRLC_VERSION:-latest}"
INSTALL_DIR="${CRLC_INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *)
    echo "install.sh: unsupported OS: $os (use a release archive from $PROJECT_URL/releases)" >&2
    exit 1
    ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *)
    echo "install.sh: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

if [ "$VERSION" = "latest" ]; then
  base="$PROJECT_URL/releases/latest/download"
else
  base="$PROJECT_URL/releases/download/$VERSION"
fi

archive="crlc_${os}_${arch}.tar.gz"
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

echo "downloading $archive ..."
curl -fsSL -o "$workdir/$archive" "$base/$archive"
curl -fsSL -o "$workdir/checksums.txt" "$base/checksums.txt"

if command -v cosign >/dev/null 2>&1; then
  curl -fsSL -o "$workdir/checksums.txt.sig" "$base/checksums.txt.sig"
  curl -fsSL -o "$workdir/checksums.txt.pem" "$base/checksums.txt.pem"
  echo "verifying checksums signature (GitHub Actions identity) ..."
  if cosign verify-blob \
    --certificate "$workdir/checksums.txt.pem" \
    --signature "$workdir/checksums.txt.sig" \
    --certificate-identity-regexp '^https://github\.com/contrl-co/crl/\.github/workflows/release\.yml@refs/tags/[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    "$workdir/checksums.txt" >/dev/null 2>&1; then
    echo "verified GitHub Actions release identity"
  else
    # Releases migrated from GitLab retain their original, valid
    # certificates. Permit that identity only for the three checksum
    # manifests whose bytes were independently validated during migration.
    checksums_digest=$(sha256_file "$workdir/checksums.txt")
    case "$checksums_digest" in
      0450e27bb1216f93d08d08e27cef1ecb7482cedf58e66fa4cba86c9ded60257a | \
      db7bd1dea94951cc210a9f70ee7ded3b47b48be0bd5aacfa701cf0534f0aeb5b | \
      bb86a92cea6f0f972cb24e637f0227215378193124732b62d0559c08b7eba369)
        echo "verifying checksum-pinned migrated release identity ..."
        cosign verify-blob \
          --certificate "$workdir/checksums.txt.pem" \
          --signature "$workdir/checksums.txt.sig" \
          --certificate-identity-regexp '^https://gitlab\.com/contrl-group/crl//\.gitlab-ci\.yml@refs/tags/0\.1\.0(-beta0[45])?$' \
          --certificate-oidc-issuer https://gitlab.com \
          "$workdir/checksums.txt" >/dev/null
        ;;
      *)
        echo "install.sh: release signature is not from the CRL GitHub workflow" >&2
        exit 1
        ;;
    esac
  fi
else
  echo "note: cosign not found; verifying SHA-256 only." >&2
  echo "      install cosign to verify the release signature as well." >&2
fi

echo "verifying SHA-256 ..."
expected=$(grep "  $archive\$" "$workdir/checksums.txt" | awk '{print $1}')
if [ -z "$expected" ]; then
  echo "install.sh: $archive not found in checksums.txt" >&2
  exit 1
fi
actual=$(sha256_file "$workdir/$archive")
if [ "$expected" != "$actual" ]; then
  echo "install.sh: checksum mismatch for $archive" >&2
  echo "  expected: $expected" >&2
  echo "  actual:   $actual" >&2
  exit 1
fi

tar -xzf "$workdir/$archive" -C "$workdir" crlc
mkdir -p "$INSTALL_DIR"
install -m 0755 "$workdir/crlc" "$INSTALL_DIR/crlc"

echo "installed $("$INSTALL_DIR/crlc" version) to $INSTALL_DIR/crlc"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: add $INSTALL_DIR to your PATH" ;;
esac
