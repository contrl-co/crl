# Performance baseline

Measure before optimizing. The baseline covers representative and 40-rule
compile/lint workloads, representative serial and parallel evaluation, graph
generation, and clean/cached `crlc` builds.

Run from a clean checkout:

```sh
BENCH_COUNT=10 ./scripts/performance-baseline.sh > baseline.txt 2>&1
```

The report includes the commit, toolchain, host metadata, latency, allocations,
artifact size and hash, build time, and peak memory. Compare results only when
the Go version, OS/architecture, and runner class match. Keep raw reports as CI
artifacts; do not commit machine-specific output.

`TestAllocationBudgets` gates the same workloads using measured ceilings. The
develop workflow stores a 10-sample report for 30 days; wall-clock and peak
memory remain informational until enough same-runner samples support stable
budgets. A budget change must include before/after samples, an explanation,
and independent review. Never relax a budget to hide a correctness or security
regression.
