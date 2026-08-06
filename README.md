# CRL — CONTRL Rule Language

CRL is a small, deterministic language for authorization rules over
collected evidence. A rule declares what evidence must exist, how
fresh it must be, and what must be true of it; evaluation returns
exactly one of five outcomes: `AUTHORIZED`, `DENIED`, `BLOCKED`,
`INSUFFICIENT_EVIDENCE`, or `EXPIRED`.

Three properties carry the design:

- **Deterministic.** The same source compiles to the same canonical
  text and the same SHA-256 hash on every platform, forever within an
  edition. No clocks, no map ordering, no locale — a decision can be
  pinned by hash and re-verified years later.
- **Fail closed.** Evidence whose freshness cannot be proven never
  satisfies a rule. A missing timestamp, a stale attestation, or a
  clockless evaluation reads as `EXPIRED`, not as silently true.
- **Five outcomes, one contract.** Only `AUTHORIZED` advances
  anything; the other four withhold for distinct, machine-readable
  reasons.

## Example

```crl
crl v1
package examples.permits
bundle permit.quorum

rule permit_application
	target permit.application
	collector application_file municipality file_upload from /bundles/application.json
		signal application_complete bool from application.complete ttl 30d
	collector registry_check land_registry api from /bundles/registry.json
		signal permit_hold_active bool from permit.hold_active ttl 30d
	collector reviewer_attest reviewer attestation from /bundles/review.json
		signal reviewer_approved bool from review.approved ttl 30d
	need application_complete == true
	block permit_hold_active
	quorum 2 of 3 application_file registry_check reviewer_attest
```

```sh
$ crlc lint permit.crl
permit.crl: ok

$ crlc compile permit.crl | tail -1
# sha256:9c25bb48199e652cbf1a1a272d8feccf26f4c61d4940db31bf95b7f9f4e1d8e7

$ crlc eval -facts facts.json -at 2026-06-02T00:00:00Z permit.crl
AUTHORIZED
```

More in [examples/](examples/), including facts files you can run.

## Install

```sh
brew tap contrl-group/tap https://gitlab.com/contrl-group/homebrew-tap.git
brew install crlc
```

Releases are reproducible builds with SHA-256 checksums, CycloneDX
SBOMs, and keyless cosign signatures — verify before trusting; see
[docs/install.md](docs/install.md).

## Documentation

| | |
|---|---|
| [spec/](spec/README.md) | The CRL v1 language specification — syntax, semantics, canonical form, editions |
| [docs/crlc.md](docs/crlc.md) | CLI reference and the Go embedding API |
| [docs/decision-security.md](docs/decision-security.md) | Decision trust boundaries, key inventory, rotation, and compromise response |
| [docs/authoring-patterns.md](docs/authoring-patterns.md) | Common bundle shapes and when to use each |
| [docs/diagnostics.md](docs/diagnostics.md) | The `CRL###` lint diagnostic catalog |
| [examples/](examples/README.md) | Runnable examples covering every language feature |
| [editors/vscode/](editors/vscode/README.md) | VS Code extension: highlighting, snippets, live diagnostics |

Every CRL snippet in this repository — spec, README, examples — is
extracted and compiled in CI. If it's written down here, it compiles.

## What this repository is (and is not)

This is the public home of the CRL **language**: specification,
compiler front-end, linter, formatter, local evaluator, graph
projection, portable decision-record schemas and verifier, examples, and editor
tooling. All of it is free software, in the open, under the AGPL — see
[License](#license).

The CONTRL **platform** — the operated services that bind rules to
real evidence streams and record decisions in a verifiable audit
trail — is a separate, closed-source product. It owns evidence capture, private
keys, approved policy distribution, and the durable replay store; this package
defines and verifies their public contracts.
Running `crlc eval` tells you what a rule decides about facts you
supply; it does not mint a decision record anyone else must trust.
That separation is deliberate: the language spreads the standard, and
the platform is accountable for operating it.

## License

Code and specification are licensed under
[AGPL-3.0](LICENSE) (GNU Affero General Public License, version 3).
The CRL and CONTRL names are trademarks — see
[TRADEMARKS.md](TRADEMARKS.md).

The AGPL is a strong copyleft license: you may use, study, modify, and
redistribute CRL freely, and if you run a modified version as a
network service you must offer its users the corresponding source
(AGPL §13). Contrl holds the copyright to CRL and can additionally
license it under other terms — contact us if the AGPL does not fit
your deployment.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) — toolchain fixes, spec
corrections, examples, and editor tooling are welcome; changes to
compiler output are edition changes and need a design issue first.
Security reports: [SECURITY.md](SECURITY.md).
