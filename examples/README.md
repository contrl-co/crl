# CRL Examples

Every `.crl` file here compiles: CI lints the whole directory and
compares each file's bundle hash against [golden.txt](golden.txt), so
the corpus doubles as the determinism gate — an unintended change to
canonical text or hashing fails the build.

| Example | Demonstrates |
|---|---|
| [permit_application.crl](permit_application.crl) | object-block form, `schema`, need + block + single-subject quorum |
| [permit_quorum_2of3.crl](permit_quorum_2of3.crl) | `quorum N of M` sugar, multiple collectors, indentation form |
| [prior_power_to_site.crl](prior_power_to_site.crl) | `time` signals, `unit`, `optional`, `within ... before`, `age <=` |
| [rule_composition.crl](rule_composition.crl) | `constructor`, `abstract rule`, `extends` inheritance |
| [escrow_release.crl](escrow_release.crl) | string/number comparisons, `!=` `<` `<=`, `required`, absolute `before` deadline |
| [inspection_quorum.crl](inspection_quorum.crl) | boolean quorums (`and`/`or`/`not`, parentheses), `count()`, `a + b >= n` |
| [temporal_windows.crl](temporal_windows.crl) | `before now`, `after <timestamp>`, `within ... after`, `age >=` |
| [milestone_cluster.crl](milestone_cluster.crl) | clusters, global final policy, rule/cluster booleans, `min_provider_trust` |
| [commented_walkthrough.crl](commented_walkthrough.crl) | a line-by-line annotated rule |

Some examples use `y` durations and intentionally carry the linter's
`CRL207` note about 365-day years; the corpus lints clean at the
default (error) threshold.

## Try them

```sh
crlc lint examples/
crlc compile examples/permit_quorum_2of3.crl
crlc fmt examples/commented_walkthrough.crl

crlc eval -facts examples/facts/permit_quorum_2of3.authorized.json \
    -at 2026-06-02T00:00:00Z examples/permit_quorum_2of3.crl
# AUTHORIZED

crlc eval -facts examples/facts/permit_quorum_2of3.blocked.json \
    -at 2026-06-02T00:00:00Z examples/permit_quorum_2of3.crl
# BLOCKED

# Freshness fails closed: same facts, but evaluated four months later
# the 30-day TTLs have lapsed.
crlc eval -facts examples/facts/permit_quorum_2of3.authorized.json \
    -at 2026-10-01T00:00:00Z examples/permit_quorum_2of3.crl
# EXPIRED
```

## Regenerating golden hashes

After an intentional language change (a new edition):

```sh
go test . -run TestExamplesMatchGoldenHashes -update-golden
```
