#!/bin/sh
# A PR metadata edit must never cancel code CI or create a green required
# check on a head SHA that the code jobs did not test.
set -eu

ci=.github/workflows/ci.yml
pr_gate=.github/workflows/pr-gate.yml

if grep -Eq 'types:.*edited' "$ci"; then
    echo "FAIL: ci must not run on pull_request edited events"
    exit 1
fi

if grep -q 'METADATA_ONLY_EDIT' "$ci"; then
    echo "FAIL: ci must not pass metadata edits through an all-skipped gate"
    exit 1
fi

metadata_group="github.event.action == 'edited' && github.event.changes.base == null && github.run_id || 'state'"
if ! grep -Fq "$metadata_group" "$pr_gate"; then
    echo "FAIL: pr-gate metadata edits must use a unique concurrency group"
    exit 1
fi

echo "workflow gate contract is safe"
