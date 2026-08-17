#!/bin/sh
# Merge gate: a test file added by a PR must fail against the base
# implementation. A new test that passes with the old implementation
# pins nothing: it would stay green if the fix it claims to cover
# regressed. Tests-only PRs are exempt.
#
# Granularity is the whole suite: the gate fails only when EVERY new
# and changed test still passes on the base implementation. One
# genuinely-pinning test can therefore mask a vacuous sibling; review
# still owns per-test judgment.
#
# Usage: check-new-tests-pin.sh [base-sha]
# Defaults to CI_MERGE_REQUEST_DIFF_BASE_SHA.
set -eu

BASE="${1:-${CI_MERGE_REQUEST_DIFF_BASE_SHA:?need a base sha}}"

added=$(git diff --name-only --diff-filter=A "$BASE"..HEAD -- '*_test.go')
if [ -z "$added" ]; then
    echo "no new test files; nothing to gate"
    exit 0
fi
impl=$(git diff --name-only "$BASE"..HEAD -- '*.go' ':(exclude)*_test.go')
if [ -z "$impl" ]; then
    echo "tests-only change; nothing to pin"
    exit 0
fi
# Implementation files the PR adds do not exist in the base, and
# `git checkout BASE -- ...` below can only restore files the base has,
# so it leaves them in place. A whole new package's tests would then run
# against the very implementation they arrived with and pass — exactly
# the vacuous case this gate exists to catch. Remove them explicitly.
added_impl=$(git diff --name-only --diff-filter=A "$BASE"..HEAD -- '*.go' ':(exclude)*_test.go')
echo "new test files:"
echo "$added"

worktree=$(mktemp -d)
trap 'git worktree remove --force "$worktree" 2>/dev/null || true' EXIT
git worktree add --detach --force "$worktree" HEAD >/dev/null
cd "$worktree"
# Base implementation + the PR's tests. A compile failure counts as
# pinning: the tests reference symbols the fix introduces.
git checkout -q "$BASE" -- '*.go' ':(exclude)*_test.go'
if [ -n "$added_impl" ]; then
    printf '%s\n' "$added_impl" | while IFS= read -r file; do
        [ -n "$file" ] || continue
        rm -f "$file"
    done
fi
if go test ./... -count=1 >test-output.log 2>&1; then
    echo "FAIL: the full suite is green with the BASE implementation and"
    echo "this PR's tests. The new test files above pin no behavior"
    echo "change; make each one fail before its fix."
    exit 1
fi
echo "suite is red against the base implementation; new tests pin their change"
