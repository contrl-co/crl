#!/bin/sh
set -eu

benchmark_count="${BENCH_COUNT:-10}"
case "$benchmark_count" in
	''|*[!0-9]*|0)
		echo "BENCH_COUNT must be a positive integer" >&2
		exit 2
		;;
esac

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
benchmark_tmp=$(mktemp -d "${TMPDIR:-/tmp}/crl-performance.XXXXXX")
cleanup() {
	rm -rf -- "$benchmark_tmp"
}
trap cleanup EXIT HUP INT TERM

cd "$repository_root"

echo "commit=$(git rev-parse HEAD)"
echo "timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "system=$(uname -smr)"
echo "toolchain=$(go version)"
go env GOOS GOARCH CGO_ENABLED
if command -v sysctl >/dev/null 2>&1; then
	echo "cpu=$(sysctl -n machdep.cpu.brand_string 2>/dev/null || true)"
	echo "logical_cpus=$(sysctl -n hw.ncpu 2>/dev/null || true)"
	echo "memory_bytes=$(sysctl -n hw.memsize 2>/dev/null || true)"
elif test -r /proc/cpuinfo; then
	grep -m1 'model name' /proc/cpuinfo || true
	echo "logical_cpus=$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)"
	grep -m1 MemTotal /proc/meminfo 2>/dev/null || true
fi

echo "benchmark_count=$benchmark_count"
go test . -run '^$' -bench 'Benchmark(Compile|Evaluate(Parallel)?|Lint|Graph)$' -benchmem -count "$benchmark_count"

if /usr/bin/time -l true >/dev/null 2>&1; then
	time_mode=darwin
elif /usr/bin/time -v true >/dev/null 2>&1; then
	time_mode=gnu
else
	time_mode=none
fi

timed_build() {
	label="$1"
	shift
	echo "$label"
	case "$time_mode" in
		darwin) /usr/bin/time -l "$@" ;;
		gnu) /usr/bin/time -v "$@" ;;
		*) time "$@" ;;
	esac
}

build_cache="$benchmark_tmp/go-build"
mkdir "$build_cache"
timed_build clean_build env GOCACHE="$build_cache" go build -trimpath -buildvcs=false -o "$benchmark_tmp/clean-crlc" ./cmd/crlc
timed_build cached_build env GOCACHE="$build_cache" go build -trimpath -buildvcs=false -o "$benchmark_tmp/cached-crlc" ./cmd/crlc
cmp "$benchmark_tmp/clean-crlc" "$benchmark_tmp/cached-crlc"
echo "artifacts_identical=true"
wc -c "$benchmark_tmp/clean-crlc" "$benchmark_tmp/cached-crlc"
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum "$benchmark_tmp/clean-crlc" "$benchmark_tmp/cached-crlc"
else
	shasum -a 256 "$benchmark_tmp/clean-crlc" "$benchmark_tmp/cached-crlc"
fi
