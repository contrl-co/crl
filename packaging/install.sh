#!/bin/sh
# Install crlc from a released, checksummed binary.
#
#   curl -fsSL https://gitlab.com/contrl-group/crl/-/raw/main/packaging/install.sh | sh
#
# Environment:
#   CRLC_VERSION      release tag to install (default: latest)
#   CRLC_INSTALL_DIR  target directory (default: ~/.local/bin)
#
# The script downloads the release archive and checksums.txt, verifies
# the archive's SHA-256 against the checksums file, and — when cosign
# is installed — verifies the checksums file's keyless signature before
# trusting it. Without cosign it proceeds on the SHA-256 alone and says
# so; install cosign for the full chain.

set -eu

PROJECT_URL="https://gitlab.com/contrl-group/crl"
VERSION="${CRLC_VERSION:-latest}"
INSTALL_DIR="${CRLC_INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *)
    echo "install.sh: unsupported OS: $os (use a release archive from $PROJECT_URL/-/releases)" >&2
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
  base="$PROJECT_URL/-/releases/permalink/latest/downloads"
else
  base="$PROJECT_URL/-/releases/$VERSION/downloads"
fi

archive="crlc_${os}_${arch}.tar.gz"
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

echo "downloading $archive ..."
curl -fsSL -o "$workdir/$archive" "$base/$archive"
curl -fsSL -o "$workdir/checksums.txt" "$base/checksums.txt"

if command -v cosign >/dev/null 2>&1; then
  curl -fsSL -o "$workdir/checksums.txt.sig" "$base/checksums.txt.sig"
  curl -fsSL -o "$workdir/checksums.txt.pem" "$base/checksums.txt.pem"
  echo "verifying checksums signature (cosign) ..."
  cosign verify-blob \
    --certificate "$workdir/checksums.txt.pem" \
    --signature "$workdir/checksums.txt.sig" \
    --certificate-identity-regexp '^https://gitlab\.com/contrl-group/crl//\.gitlab-ci\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$' \
    --certificate-oidc-issuer https://gitlab.com \
    "$workdir/checksums.txt"
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
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$workdir/$archive" | awk '{print $1}')
else
  actual=$(shasum -a 256 "$workdir/$archive" | awk '{print $1}')
fi
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
