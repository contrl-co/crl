# Contributing

Thanks for looking at CRL. This repository is the public home of the
CRL language: the specification, the `crlc` toolchain, the examples,
and the editor tooling. It is developed in the open, for free use
(final license text pending review — see the README).

One boundary up front: **the CONTRL platform is closed source.** The
governance services, audit chain, registry, and everything else that
*operates* CRL decisions in production live elsewhere and are not open
to contribution here. Issues or merge requests that try to
reconstruct, interface with, or expose platform internals will be
closed.

## What we welcome

- Toolchain bug fixes and diagnostics improvements (`crlc`, the
  linter, the formatter).
- Specification corrections where the text disagrees with the
  compiler — the compiler is the source of truth, and CI enforces
  that every spec example compiles.
- New examples, better docs, editor/tooling integrations.
- Reproducibility and supply-chain improvements to the release
  pipeline.

## What needs an issue first

- **Anything that changes compiler output.** Canonical text and
  bundle hashes are frozen within an edition; a change to either is by
  definition a new edition (see [spec/editions.md](spec/editions.md))
  and needs an accepted design issue before any code. The golden
  corpus test will fail your MR otherwise — that is working as
  intended.
- New language features, new lint rules with `error` severity, new
  CLI commands.

## Mechanics

- Branch + merge request; no direct pushes to `main`.
- Small, reviewable commits: one logical change per commit, mechanical
  changes (renames, moves) separated from behavior changes.
- Tests land with the behavior they pin; docs land with the change
  they describe.
- Every MR must be green: `gofmt`, `go vet`, `go test ./...` (which
  includes the golden corpus, canonical round-trip, and docs-lint
  gates), and the extension's `npm run check` if you touched
  `editors/vscode`.
- Sign your work (DCO): add `Signed-off-by: Your Name <you@example.com>`
  to each commit (`git commit -s`).

## Running the gates locally

```sh
gofmt -l . && go vet ./...
go test ./... -count=1
go run ./cmd/crlc lint examples/
(cd editors/vscode && npm run check)
```
