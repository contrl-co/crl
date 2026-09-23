#!/bin/sh

set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT

fixture_dir="$test_root/fixtures"
payload_dir="$test_root/payload"
base_bin="$test_root/base-bin"
verified_bin="$test_root/verified-bin"
rejected_bin="$test_root/rejected-bin"
mkdir -p "$fixture_dir" "$payload_dir" "$base_bin" "$verified_bin" "$rejected_bin"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux | darwin) ;;
  *)
    echo "test-install-script: unsupported test OS: $os" >&2
    exit 1
    ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *)
    echo "test-install-script: unsupported test architecture: $arch" >&2
    exit 1
    ;;
esac

archive="crlc_${os}_${arch}.tar.gz"

cat >"$payload_dir/crlc" <<'EOF'
#!/bin/sh
echo "crlc test-version"
EOF
chmod +x "$payload_dir/crlc"
tar -czf "$fixture_dir/$archive" -C "$payload_dir" crlc

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

archive_digest=$(sha256_file "$fixture_dir/$archive")
printf '%s  %s\n' "$archive_digest" "$archive" >"$fixture_dir/checksums.txt"
: >"$fixture_dir/checksums.txt.sig"
: >"$fixture_dir/checksums.txt.pem"
manifest_digest=$(sha256_file "$fixture_dir/checksums.txt")
printf '%s\ngithub\n' "$manifest_digest" >"$fixture_dir/checksums.txt.sigstore.json"

cat >"$base_bin/curl" <<'EOF'
#!/bin/sh
set -eu

output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      output=$2
      shift 2
      ;;
    -*) shift ;;
    *)
      url=$1
      shift
      ;;
  esac
done

test -n "$output"
test -n "$url"
cp "$CRLC_TEST_FIXTURES/$(basename "$url")" "$output"
EOF
chmod +x "$base_bin/curl"
cp "$base_bin/curl" "$verified_bin/curl"
cp "$base_bin/curl" "$rejected_bin/curl"

cat >"$verified_bin/cosign" <<'EOF'
#!/bin/sh
set -eu
# A strict CLI-contract double, not a cryptographic verifier. The separate
# test-release-signing.cjs exercises real pinned-cosign bundle signatures.
test "$1" = verify-blob
if [ "$2" = --certificate ]; then
  test "$(basename "$3")" = checksums.txt.pem
  test "$4" = --signature
  test "$(basename "$5")" = checksums.txt.sig
  test "$6" = --certificate-identity-regexp
  test "$7" = '^https://gitlab\.com/contrl-group/crl//\.gitlab-ci\.yml@refs/tags/0\.1\.0(-beta0[45])?$'
  test "$8" = --certificate-oidc-issuer
  test "$9" = https://gitlab.com
  test "$#" -eq 10
  echo 'verified historical CLI contract' >&2
  exit 0
fi
test "$2" = --bundle
bundle=$3
test "$4" = --certificate-identity-regexp
test "$5" = '^https://github\.com/contrl-co/crl/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$'
test "$6" = --certificate-oidc-issuer
test "$7" = https://token.actions.githubusercontent.com
test "$#" -eq 8
test "$(sed -n '2p' "$bundle")" = github
if command -v sha256sum >/dev/null 2>&1; then
  digest=$(sha256sum "$8" | awk '{print $1}')
else
  digest=$(shasum -a 256 "$8" | awk '{print $1}')
fi
test "$(sed -n '1p' "$bundle")" = "$digest"
EOF
chmod +x "$verified_bin/cosign"

cat >"$rejected_bin/cosign" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod +x "$rejected_bin/cosign"

run_install() {
  case_name=$1
  command_path=$2
  allow_unverified=$3
  install_dir="$test_root/install-$case_name"
  output_file="$test_root/$case_name.out"

  if CRLC_TEST_FIXTURES="$fixture_dir" \
    CRLC_VERSION=v9.9.9 \
    CRLC_INSTALL_DIR="$install_dir" \
    CRLC_ALLOW_UNVERIFIED="$allow_unverified" \
    PATH="$command_path:/usr/bin:/bin" \
    sh "$repo_root/packaging/install.sh" >"$output_file" 2>&1; then
    return 0
  fi
  return 1
}

if run_install missing-cosign "$base_bin" 0; then
  echo "test-install-script: installer accepted an unverifiable release" >&2
  exit 1
fi
grep -q 'cosign is required' "$test_root/missing-cosign.out"
test ! -e "$test_root/install-missing-cosign/crlc"

run_install emergency-recovery "$base_bin" 1
grep -q 'CRLC_ALLOW_UNVERIFIED=1' "$test_root/emergency-recovery.out"
test "$("$test_root/install-emergency-recovery/crlc" version)" = 'crlc test-version'

run_install verified-signer "$verified_bin" 0
grep -q 'verified GitHub Actions release identity' "$test_root/verified-signer.out"
test "$("$test_root/install-verified-signer/crlc" version)" = 'crlc test-version'

if run_install rejected-signer "$rejected_bin" 1; then
  echo "test-install-script: emergency mode bypassed an observed signer failure" >&2
  exit 1
fi
grep -q 'release signature is not from the CRL GitHub workflow' "$test_root/rejected-signer.out"
test ! -e "$test_root/install-rejected-signer/crlc"

# Real public manifest bytes pin the historical selector without downloading
# or executing an old release binary. A valid legacy verification must reach
# the archive checksum check, which rejects our intentionally different stub.
cp "$repo_root/scripts/ci/fixtures/0.1.0-checksums.txt" "$fixture_dir/checksums.txt"
test "$(sha256_file "$fixture_dir/checksums.txt")" = 0450e27bb1216f93d08d08e27cef1ecb7482cedf58e66fa4cba86c9ded60257a
if run_install historical "$verified_bin" 0; then
  echo "test-install-script: historical checksum accepted a different binary" >&2
  exit 1
fi
grep -q 'verified historical CLI contract' "$test_root/historical.out"
grep -q 'checksum mismatch' "$test_root/historical.out"
if run_install historical-rejected "$rejected_bin" 1; then
  echo "test-install-script: installer bypassed historical signer failure" >&2
  exit 1
fi
test ! -e "$test_root/install-historical-rejected/crlc"

printf 'tampered\n' >>"$fixture_dir/checksums.txt"
if run_install historical-tampered "$verified_bin" 1; then
  echo "test-install-script: installer trusted a changed historical manifest" >&2
  exit 1
fi
if grep -q 'checksum-pinned migrated' "$test_root/historical-tampered.out"; then
  echo "test-install-script: historical trust was not checksum-pinned" >&2
  exit 1
fi
test ! -e "$test_root/install-historical-tampered/crlc"
printf '%s  %s\n' "$archive_digest" "$archive" >"$fixture_dir/checksums.txt"

printf '%s\nother-identity\n' "$manifest_digest" >"$fixture_dir/checksums.txt.sigstore.json"
if run_install wrong-identity "$verified_bin" 1; then
  echo "test-install-script: installer accepted a wrong identity" >&2
  exit 1
fi
test ! -e "$test_root/install-wrong-identity/crlc"

printf '%s\ngithub\n' "$manifest_digest" >"$fixture_dir/checksums.txt.sigstore.json"
printf 'tampered manifest\n' >>"$fixture_dir/checksums.txt"
if run_install tampered-manifest "$verified_bin" 1; then
  echo "test-install-script: installer accepted a tampered signed manifest" >&2
  exit 1
fi
test ! -e "$test_root/install-tampered-manifest/crlc"
printf '%s  %s\n' "$archive_digest" "$archive" >"$fixture_dir/checksums.txt"

mv "$fixture_dir/checksums.txt.sigstore.json" "$fixture_dir/bundle.saved"
if run_install missing-bundle "$verified_bin" 1; then
  echo "test-install-script: installer fell back from a missing bundle" >&2
  exit 1
fi
test ! -e "$test_root/install-missing-bundle/crlc"
mv "$fixture_dir/bundle.saved" "$fixture_dir/checksums.txt.sigstore.json"

printf 'tampered archive\n' >>"$fixture_dir/$archive"
if run_install tampered-archive "$verified_bin" 0; then
  echo "test-install-script: installer accepted a tampered archive" >&2
  exit 1
fi
grep -q 'checksum mismatch' "$test_root/tampered-archive.out"
test ! -e "$test_root/install-tampered-archive/crlc"

printf '%064d  %s\n' 0 "$archive" >"$fixture_dir/checksums.txt"
if run_install bad-checksum "$base_bin" 1; then
  echo "test-install-script: installer accepted a checksum mismatch" >&2
  exit 1
fi
grep -q 'checksum mismatch' "$test_root/bad-checksum.out"
test ! -e "$test_root/install-bad-checksum/crlc"

echo "install-script regression tests passed"
