# CRL for Visual Studio Code

Language support for CRL (CONTRL Rule Language): syntax highlighting,
snippets, bracket/comment configuration, and compiler-backed
diagnostics for `.crl` files.

## Features

- **Highlighting** for the full v1 surface: declarations
  (`package`, `bundle`, `rule`, `abstract rule`, `constructor`,
  `extends`, `cluster`, `target`, `collector`, `signal`), predicates
  (`need`, `block`, `quorum`, `count`, `N of M`), temporal operators
  (`before`, `after`, `within`, `age`, `now`), signal kinds,
  durations, timestamps, and `#` comments.
- **Diagnostics from the real compiler.** The extension pipes the
  document through `crlc lint -format json` and surfaces `CRL###`
  errors and warnings inline, on type (debounced), on save, or on
  demand (`CRL: Lint Document`).
- **Snippets** for every declaration form, including rule inheritance
  and all three quorum spellings.

## Requirements

[`crlc`](../../docs/install.md) on your PATH. When the CRL toolchain
repository itself is open, the extension runs the in-tree compiler
(`go run ./cmd/crlc`) instead, so grammar work and diagnostics stay in
lockstep.

## Settings

| Setting | Default | Meaning |
|---|---|---|
| `crl.lint.command` | `auto` | Linter executable; `auto` prefers the in-tree toolchain, then `crlc` from PATH. The extension appends `lint -format json -fail-on none`. |
| `crl.lint.args` | `[]` | Arguments inserted before the required lint arguments (for wrappers such as `go`). |
| `crl.lint.run` | `onType` | When diagnostics refresh: `onType`, `onSave`, or `off`. |
| `crl.lint.delayMs` | `300` | Debounce for on-type linting. |
| `crl.lint.trace` | `false` | Log linter invocations to the CRL output channel. |

If the linter cannot be run at all, the extension reports a single
`CRL000` warning on the first line instead of failing silently.

## Development

```sh
npm run check          # syntax-check extension.js and validate assets
npm ci
npm run package -- --out crl-language-support.vsix
```
