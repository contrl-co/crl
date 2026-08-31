#!/bin/sh
# Builds the browser toolchain: cmd/crl-wasm compiled to WebAssembly,
# plus the wasm_exec.js shim from the same Go toolchain that produced
# the binary. The two must ship together — the shim implements the
# host calls this exact runtime expects, and a mismatched pair fails at
# instantiation.
#
# Usage: scripts/build-wasm.sh [output-dir]
# Defaults to dist/wasm (gitignored, like every other build output).
#
# CRL_VERSION stamps the version reported by contrlEngineInfo; release
# builds pass the tag, as .goreleaser.yaml does for crlc.
set -eu

out="${1:-dist/wasm}"
version="${CRL_VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"

goroot=$(go env GOROOT)
shim="$goroot/lib/wasm/wasm_exec.js"
if [ ! -f "$shim" ]; then
    # Go moved the shim to lib/wasm in 1.24; keep the old path working
    # so the script fails on a real problem rather than a layout change.
    shim="$goroot/misc/wasm/wasm_exec.js"
fi
if [ ! -f "$shim" ]; then
    echo "build-wasm: no wasm_exec.js in $goroot" >&2
    exit 1
fi

mkdir -p "$out"

# Same determinism flags as the crlc release build: no cgo, trimmed
# paths, stripped symbols. The artifact is served to every visitor, so
# it carries no local paths and no debug tables.
CGO_ENABLED=0 GOOS=js GOARCH=wasm go build \
    -trimpath \
    -ldflags "-s -w -X main.version=$version" \
    -o "$out/crl.wasm" \
    ./cmd/crl-wasm

# The shim lives read-only in the module cache, and a plain copy carries
# that mode over — which makes the next build fail on its own output.
rm -f "$out/wasm_exec.js"
cp "$shim" "$out/wasm_exec.js"
chmod u+w "$out/wasm_exec.js"

echo "crl.wasm      $(wc -c <"$out/crl.wasm" | tr -d ' ') bytes"
echo "version       $version"
echo "shim          $shim"
if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$out/crl.wasm"
elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$out/crl.wasm"
fi
