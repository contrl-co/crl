#!/bin/sh
# Merge gate: a compiler change that moves the hash of unchanged source
# must be disclosed. Compiles the MR base's example corpus with the MR's
# compiler and compares against the base golden file; any drift requires
# a "hashes moved" entry in the MR's CHANGELOG diff (spec/editions.md).
#
# Usage: check-hash-disclosure.sh [base-sha]
# Defaults to CI_MERGE_REQUEST_DIFF_BASE_SHA. The base sha must be
# fetchable; in CI, fetch the target branch first.
set -eu

BASE="${1:-${CI_MERGE_REQUEST_DIFF_BASE_SHA:?need a base sha}}"

worktree=$(mktemp -d)
trap 'git worktree remove --force "$worktree" 2>/dev/null || true' EXIT
git worktree add --detach --force "$worktree" "$BASE" >/dev/null

# Built (not `go run`) so the program's exit code survives: go run
# collapses every program failure to exit 1.
go build -o "$worktree/hashdrift-bin" ./scripts/ci/hashdrift
rc=0
"$worktree/hashdrift-bin" "$worktree" || rc=$?
if [ "$rc" -eq 0 ]; then
    echo "no hash drift against base $BASE"
    exit 0
fi
if [ "$rc" -ne 2 ]; then
    echo "hashdrift failed to run (exit $rc)"
    exit 1
fi
if git diff "$BASE"..HEAD -- CHANGELOG.md | grep -qi "hashes moved"; then
    echo "hash drift is disclosed in CHANGELOG.md; passing"
    exit 0
fi
echo "FAIL: this change moves bundle hashes of unchanged source with no"
echo "'hashes moved' CHANGELOG entry. spec/editions.md treats an"
echo "undisclosed hash move as a bug; disclose it or revert it."
exit 1
