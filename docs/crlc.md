# crlc — the CRL toolchain

One binary: lint, compile, format, evaluate, and graph CRL source.

```text
crlc <command> [flags] [path ...]
```

Every command reads a file path argument, `-` for stdin, or stdin when
no path is given.

**Exit codes** (all commands): `0` success; `1` the command's check
failed (lint threshold met, compile error, or `-require-authorized`
unmet); `2` usage or I/O error.

## crlc lint

```text
crlc lint [-format text|json] [-fail-on error|warning|info|none] [-canonical] [-quiet] [path ...]
```

Lints files, directories (recursively, `.crl` files; `.git`,
`node_modules`, and `vendor` are skipped), or stdin. Diagnostics carry
stable `CRL###` codes — see [diagnostics.md](diagnostics.md).

- `-fail-on` sets the exit-1 threshold (default `error`; `none` always
  exits 0).
- `-format json` emits the full structured report; add `-canonical`
  to include each file's canonical compiled text.
- Text diagnostics print as `path:line:col: severity CODE: message`.

## crlc compile

```text
crlc compile [-edition v1] [-format text|json] [path]
```

Compiles one source and prints its canonical text followed by a
trailing `# sha256:<hash>` line — the whole output is itself valid,
lintable CRL. `-format json` emits
`{ok, edition, source_hash, canonical_text, hash}` instead.

`-edition` pins the edition (default and currently only: `v1`);
requesting an unimplemented edition fails the compile.

## crlc fmt

```text
crlc fmt [path]
crlc fmt -w path ...
```

Prints the canonical form of one source (path, `-`, or stdin).
Formatting is not configurable: the canonical form is the only output,
because the canonical bytes are what get hashed. `-w` rewrites one or
more files in place and prints the paths it changed.

## crlc eval

```text
crlc eval -facts facts.json [-at rfc3339] [-format text|json] [-require-authorized] [path]
```

Compiles one source and evaluates it against a JSON object of facts.
Prints one of the five outcomes — `AUTHORIZED`, `DENIED`, `BLOCKED`,
`INSUFFICIENT_EVIDENCE`, `EXPIRED` — followed by the failing checks
and their reasons; `-format json` emits the full evaluation trace.

- `-at` sets the evaluation clock. **Without it, evaluation runs
  clockless and fails closed on every time-dependent rule** — signals
  with `ttl`/`expires` read as expired. Pass a real clock to evaluate
  freshness, and record the clock you passed: the decision is a
  function of it.
- `-require-authorized` exits 1 unless the outcome is `AUTHORIZED` —
  for CI gates and scripts.

Fact keys are lowercase signal names; observation times go under
`observed_at.<signal>`:

```json
{
  "application_complete": true,
  "observed_at.application_complete": "2026-06-01T09:00:00Z"
}
```

## crlc graph

```text
crlc graph [path]
```

Emits the bundle's deterministic node/edge graph and a computed layout
as JSON, plus the bundle hash. The same source always produces the
same graph, node identities included — the graph is a projection of
the compiled bundle, not a second source of truth.

## crlc version

Prints the toolchain version and the editions it implements.

## Embedding in Go

The same operations are available as a Go library:

```go
import crl "gitlab.com/contrl-group/crl"

compiled, err := crl.Compile(source)          // canonical text + hash
report := crl.Lint("policy.crl", source)      // CRL### diagnostics
evaluation := compiled.EvaluateAt(facts, now) // one of five outcomes
```

The API surface is `Compile`/`CompileEdition`, `Format`, `Lint`,
`Graph`, and `Compiled.Evaluate`/`EvaluateAt`. Compiler internals
(AST, IR) are deliberately not exported.
