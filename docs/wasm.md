# The browser build

`cmd/crl-wasm` is the CRL toolchain compiled to WebAssembly. It gives a
web page the same compiler, formatter, linter, graph projection, and
evaluator that `crlc` runs, with no server: a rule is compiled and
evaluated in the tab, and the source never leaves it.

Two properties carry over unchanged from the CLI, and they are gated in
CI (`ci / wasm`):

- **Same hashes.** The browser build compiles the example corpus to the
  hashes in `examples/golden.txt`, byte-for-byte the canonical text
  `crlc` produces. A browser compiler that disagreed with the CLI would
  be worse than no browser compiler.
- **Only the language.** The artifact links this module,
  `golang.org/x/text`, and the standard library — nothing else.
  Whatever a WebAssembly module links is served to everyone who opens
  the page, and it decompiles, so the linked set is a merge gate
  (`scripts/ci/check-wasm-deps.sh`), not a convention.

What it is not: an authority. Evaluating a rule here tells you what the
rule decides about facts you supplied, on a clock the page supplied. It
mints nothing anyone else has to trust.

## Build

```sh
scripts/build-wasm.sh            # writes dist/wasm/
scripts/build-wasm.sh public/    # or anywhere else
```

The script emits two files that must be served together:

| | |
|---|---|
| `crl.wasm` | the toolchain |
| `wasm_exec.js` | Go's host shim, copied from the toolchain that built the binary |

The shim implements the host calls that exact Go runtime expects; a
mismatched pair fails at instantiation. `CRL_VERSION` stamps the
version `contrlEngineInfo` reports, as the release build does for
`crlc`.

## Loading

```html
<script src="/wasm_exec.js"></script>
<script type="module">
  const go = new Go();
  const wasm = await WebAssembly.instantiateStreaming(fetch("/crl.wasm"), go.importObject);
  void go.run(wasm.instance); // never resolves: the module blocks so its globals stay callable
</script>
```

`go.run` starts the module and does not return — the Go program blocks
deliberately, because a Go WebAssembly module's exports die when `main`
returns. The globals appear shortly after `go.run` is called, so poll
for `contrlEngineInfo` rather than assuming it is there on the next
line.

Serve `crl.wasm` with `Content-Type: application/wasm` if you want
`instantiateStreaming`; otherwise fetch the bytes and use
`WebAssembly.instantiate`.

## Calling

Every global takes **one JSON string** and returns **one JSON string**.
A WebAssembly export has no error channel, so failures come back as a
JSON object with an `error` key rather than as a thrown exception:

```js
function call(name, request) {
  const response = JSON.parse(globalThis[name](JSON.stringify(request)));
  if (response.error) throw new Error(response.error);
  return response;
}
```

### `contrlCompileCRL`

Request `{ source, edition? }` — `edition` defaults to `v1` and any
other value is an error, because an edition is a frozen compilation
contract.

Response:

| field | |
|---|---|
| `edition` | the edition compiled under |
| `source_hash` | SHA-256 of the submitted source bytes |
| `canonical_text` | the normalized rendering; recompiles to itself |
| `hash` | the bundle's content address — the thing to pin |
| `program` | read-only view of what the bundle declares: rules, collectors, signals, predicates, clusters |

### `contrlFormatCRL`

Request `{ source }`. Response `{ formatted, hash, source_hash }`.
Formatting is not configurable: the canonical form is the only output,
because the canonical bytes are what get hashed. `formatted` and `hash`
come from one compilation, so text and hash cannot disagree.

### `contrlLintCRL`

Request `{ path?, source }` — `path` only labels the diagnostics and
defaults to `playground.crl`.

Response `{ path, ok, compiled_hash, canonical_text, diagnostics }`,
where each diagnostic carries `line`, `column`, `severity`, a stable
`CRL###` `code`, and a `message` (see
[diagnostics.md](diagnostics.md)).

Lint is the one function where a source that does not compile is a
*successful* response holding diagnostics, not an `error`. Reporting
where the author went wrong is the whole job.

### `contrlGraphCRL`

Request `{ source }`. Response carries `source_hash`, `canonical_text`,
`hash`, `program`, and:

| field | |
|---|---|
| `graph` | the positioned layout: `nodes` with `x`/`y`/`width`/`height`, `edges` with orthogonal `points`, plus the overall `width`/`height` |
| `structure` | the same graph without geometry, for consumers that lay out their own |

Positions and edge routes are computed here, not in the renderer, so
the diagram is a deterministic function of the source: the same source
yields the same coordinates, and node IDs are structural, so a node
keeps its identity across edits.

### `contrlEvaluateCRL`

Request `{ source, facts, now? }`.

`facts` is a flat JSON object of signal names to values. A signal's
observation time goes under `observed_at.<signal>` as RFC3339 — that is
what freshness is judged against. `examples/facts/` holds runnable
fact sets for the example corpus.

`now` is the evaluation clock as an RFC3339 timestamp. Omit it and the
host's clock is used, which in a browser is the visitor's own clock;
either way the response echoes the instant in `evaluated_at`, because a
freshness decision is a function of it.

Response: `source_hash`, `canonical_text`, `hash`, `evaluated_at`, and
the evaluation itself — `result` (one of the five outcomes),
`authorized`, `aspect`, and the `checks`, `rules`, `clusters`, and
`global_checks` traces that explain it. Every consumer must handle all
five spellings of `result`; only `AUTHORIZED` advances anything.

### `contrlEngineInfo`

Request `{}`. Response `{ engine, version, edition, functions }` — the
toolchain version that produced the hashes on the page, the edition it
compiled under, and the names of every global installed.

## Compiling a rule end to end

`scripts/ci/wasm-smoke.cjs` is the worked example: it loads the
artifact under Node, compiles `examples/permit_quorum_2of3.crl`, checks
the hash against `examples/golden.txt` and against `crlc` run on the
same file, evaluates both fact sets to `AUTHORIZED` and `BLOCKED`,
shows that the same evidence read at today's clock is `EXPIRED`, and
renders the graph. Run it after a build:

```sh
scripts/build-wasm.sh
node scripts/ci/wasm-smoke.cjs
```
