#!/bin/sh
# Merge gate: the browser build must link nothing but the CRL language.
#
# A WebAssembly module is served to everyone who opens the page that
# embeds it, and it decompiles. Whatever cmd/crl-wasm links is therefore
# published, whether or not anyone meant to publish it. This gate pins
# the linked set to this module, golang.org/x/text (Unicode
# normalization, the compiler's only dependency), and the standard
# library. An import outside that set fails here, at review time, rather
# than being noticed after the artifact is already being served.
#
# The second check is about the build environment rather than the code:
# a release artifact must carry no absolute paths from the machine that
# produced it, which is what -trimpath is for.
#
# Usage: check-wasm-deps.sh [artifact]
# Defaults to dist/wasm/crl.wasm; the artifact check is skipped when it
# has not been built.
set -eu

artifact="${1:-dist/wasm/crl.wasm}"

# Package paths never contain whitespace, so the word split is safe.
# The case statements are kept out of the command substitution: bash 3.2,
# still the /bin/sh on macOS, misparses `case` patterns inside `$( )`.
linked=$(GOOS=js GOARCH=wasm go list -deps ./cmd/crl-wasm)
external=""
for pkg in $linked; do
    # A standard-library import path has no dot in its first element.
    case "${pkg%%/*}" in
        *.*) ;;
        *) continue ;;
    esac
    case "$pkg" in
        github.com/contrl-co/crl | github.com/contrl-co/crl/*) continue ;;
        golang.org/x/text/*) continue ;;
    esac
    external="$external$pkg
"
done

if [ -n "$external" ]; then
    echo "FAIL: the browser build links packages outside the CRL language:"
    printf '%s' "$external" | sed 's/^/    /'
    echo "Everything cmd/crl-wasm links is published to every visitor."
    exit 1
fi

if [ -f "$artifact" ]; then
    if grep -q -a -e '/Users/' -e '/home/' "$artifact"; then
        echo "FAIL: $artifact carries absolute build paths; build it with -trimpath"
        exit 1
    fi
    echo "browser build links only the CRL language; $artifact carries no build paths"
    exit 0
fi

echo "browser build links only the CRL language ($artifact not built; skipped its path check)"
