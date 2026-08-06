#!/bin/sh

set -eu

repo_root=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
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
exit 0
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

printf '%064d  %s\n' 0 "$archive" >"$fixture_dir/checksums.txt"
if run_install bad-checksum "$base_bin" 1; then
  echo "test-install-script: installer accepted a checksum mismatch" >&2
  exit 1
fi
grep -q 'checksum mismatch' "$test_root/bad-checksum.out"
test ! -e "$test_root/install-bad-checksum/crlc"

echo "install-script regression tests passed"
