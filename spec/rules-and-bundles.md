# Rules and Bundles

CRL source compiles to a bundle. A bundle contains rules, optional
clusters, and optional global predicates.

```text
Bundle
  rules: Rule[]
  clusters: Cluster[]
  predicates: Predicate[]
```

The source file itself is the bundle. A `bundle <name>` header may name
it, and `bundle <name> { ... }` may wrap its contents.

```crl
crl v1
package examples.permits

bundle permit.launch {
    rule application_ready {
        target permit.application
        collector application_file municipality file_upload from /bundles/application.json {
            signal application_complete bool from application.complete ttl 30d
        }
        need application_complete == true
        quorum application_file
    }
}
```

## Rule

A rule declares authorization conditions for one target aspect.

```crl
crl v1
package examples.finance
bundle finance.capital

rule capital_ready
    target finance.capital
    collector funding_record finance webhook from /bundles/funding.json
        signal committed_usd number from capital.committed_usd ttl 30d
    need committed_usd >= 250000
    quorum funding_record
```

A rule must have:

- one valid rule name,
- a `target`,
- at least one `collector`,
- at least one predicate.

Rule names are exposed as boolean subjects after evaluation. Later
clusters and global predicates refer to a rule by its plain rule name.

Rules may extend a `constructor` or `abstract rule`; inherited
collectors and predicates are expanded into the concrete rule before
canonical compilation:

```text
rule permit_ready extends no_active_hold
```

## Constructor and abstract rule

Constructors and abstract rules define reusable rule structure. They
can declare collectors and predicates, and may also declare a target.
They are not evaluated directly — they are compile-time authoring
objects, expanded away before hashing (see
[object-model.md](object-model.md#composition-model)).

```text
constructor current_registry_evidence {
    collector registry_record registry webhook from /bundles/registry.json schema registry_evidence_v1 {
        signal record_current bool from record.current ttl 30d
    }
    need record_current == true
    quorum registry_record
}

abstract rule no_active_hold extends current_registry_evidence {
    collector hold_record registry webhook from /bundles/holds.json {
        signal active_hold bool from hold.active ttl 30d
    }
    block active_hold
}
```

## Target

`target` names the aspect the rule is written for.

```text
target utility.power
target permit.application
target construction.foundation
```

Targets are identifiers, normalized to lowercase. What a compiled
bundle may actually be bound to govern is the evaluating host's
decision, made outside the language.

## Collector

A collector declares the evidence source and connector shape that can
produce signals for a rule.

```text
collector utility_record utility file_upload from /bundles/power.json schema utility_power_v1
```

| Field | Meaning |
|-------|---------|
| `name` | Collector name. Also a quorum subject. |
| `provider_type` | Open provider category, such as `utility`, `engineering`, `registry`, or `finance`. |
| `connector_kind` | Open connector category, such as `webhook`, `file_upload`, `api_poll`, `stream`, or `attestation`. |
| `source` | Connector-local payload address or source descriptor. |
| `schema` | Optional normalized schema identifier for the external payload contract. |

CRL does not fetch evidence. It declares what normalized evidence is
expected; the evaluating host supplies the evidence payloads as facts
at evaluation time.

Collector presence can satisfy quorum through any of these fact keys:

```text
provider.<collector>
provider:<collector>
<collector>
```

## Signal

A signal declares a typed fact produced by a collector.

```text
signal capacity_kw number from power.capacity_kw unit kw ttl 10y
signal power_built bool from power.built ttl 10y
signal credential_reference string from credential.reference ttl 30d
signal confirmed_at time from power.confirmed_at ttl 10y
signal notes string from power.notes optional ttl 30d
```

| Field | Meaning |
|-------|---------|
| `name` | The fact name used by predicates. |
| `kind` | `number`, `bool`, `string`, or `time`. |
| `source_field` | Connector-local payload field path. |
| `unit` | Optional unit for `number` signals. |
| `optional` | Marks a signal as non-required for external payload normalization. |
| `ttl` / `expires` | Evidence freshness rule. |

Predicates use signal names, not source field paths:

```text
need capacity_kw >= 2000
```

This is correct because `capacity_kw` is the declared signal. Do not
write `need power.capacity_kw >= 2000` unless `power.capacity_kw` is
itself declared as the signal name.

## Freshness

`ttl` and `expires` define how long a signal remains usable.

```text
signal inspection_passed bool from inspection.passed ttl 30d
signal permit_current bool from permit.current expires 2026-12-31T23:59:59Z
```

Freshness fails closed. A `ttl` signal's age is proven by an
`observed_at.<signal>` fact; when that fact is missing or unparseable,
the age is unknowable and the signal evaluates as **expired** — a
forgotten timestamp never disables a declared freshness guarantee.
Evaluating without a clock likewise reads every `ttl`/`expires` signal
as expired. See [semantics.md](semantics.md#freshness-fail-closed).

## Predicate

Predicates are the proof obligations of a rule, cluster, or bundle.

### `need`

`need` compares a fact against a literal:

```text
need application_complete == true
need progress_percent >= 80
need credential_reference == "CRD-001"
```

and supports temporal forms over `time` signals:

```text
need confirmed_at before construction_start
need confirmed_at after "2026-01-01T00:00:00Z"
need confirmed_at within 10y before construction_start
need confirmed_at age <= 10y
```

The field must resolve to a visible signal, rule result, cluster
result, or a reserved kernel fact (see
[semantics.md](semantics.md#kernel-facts)). Temporal fields and
temporal field references must be `time` signals.

### `block`

`block` is a fail-closed guard:

```text
block active_hold
```

It passes when the field is false or numeric zero; it fails — as
`BLOCKED` — when the field is true or non-zero.

### `quorum`

Logical quorum:

```text
quorum inspection_upload & permit_registry
quorum utility_record and not grid_hold
```

Count quorum, in three equivalent spellings:

```text
quorum count(inspection_upload, permit_registry, utility_record) >= 2
quorum inspection_upload + permit_registry + utility_record >= 2
quorum 2 of 3 inspection_upload permit_registry utility_record
```

At rule scope, count-quorum subjects are collectors. At cluster scope,
count quorum may also count rule subjects. At global scope, it may
count collector, rule, and cluster subjects.

## Cluster

A cluster composes rule results and may add predicates.

```text
cluster buildable_scope
    rules power_to_site + permit_clearance
    quorum power_to_site & permit_clearance
```

A cluster must have:

- one valid cluster name,
- at least one rule reference,
- at least one predicate.

All listed member rules must authorize before the cluster can
authorize, and the cluster's own predicates must also pass.

## Global policy

Top-level predicates define the bundle's final policy:

```text
need buildable_scope == true
quorum buildable_scope
```

When global predicates exist, they alone decide the bundle, and every
declared rule and cluster must be reachable from them — a bundle
cannot hide rules that the final authorization never considers. With
no global predicates, all rules and clusters must authorize. In
indentation form, put global predicates before the rules (an
unindented predicate directly after a rule body belongs to that rule —
see [semantics.md](semantics.md#rules-clusters-and-the-bundle)).

## Type system

| Signal kind | Supported literals | Supported operators |
|-------------|--------------------|---------------------|
| `number` | finite numbers | `==`, `!=`, `>`, `>=`, `<`, `<=` |
| `bool` | `true`, `false` | `==`, `!=` |
| `string` | quoted strings | `==`, `!=` |
| `time` | RFC3339 facts | temporal `need` forms |

`block` accepts `bool` and `number` fields.

Kernel facts are host-controlled; prefer declared signals for portable
source.

## Name rules

CRL rejects ambiguous proof subjects: a name cannot mean two different
object kinds in the same compiled bundle. A collector and a signal, for
example, cannot share a name.

Signals may repeat a name only when they share the same kind **and**
the same expiry — one name maps to one fact with one freshness
contract. Prefer unique signal names for readability.

## Evaluation results

CRL evaluates fail-closed. Missing facts, expired facts, type
mismatches, unsupported operators, active blockers, and unmet quorum
all fail. Result precedence:

```text
AUTHORIZED
EXPIRED
BLOCKED
INSUFFICIENT_EVIDENCE
DENIED
```

`INSUFFICIENT_EVIDENCE` covers unknown facts and unmet quorum;
`DENIED` is the residual — predicates evaluated against present, fresh
evidence and failed. See
[semantics.md](semantics.md#outcome-selection).

## Canonicalization

Compilation returns canonical CRL text and a compiled hash.
Canonicalization:

- lowercases identifiers,
- trims names and source strings,
- normalizes `crl v1` to the internal version `crl/v1`,
- normalizes `expires <duration>` to `ttl <duration>`,
- renders strings, sources, numbers, and booleans consistently,
- sorts count-quorum subjects (they are set-like),
- may reorder logical `and`/`or` operands by rendered form,
- preserves declaration order for rules, clusters, collectors,
  signals, and predicates.

The compiled hash is over the normalized bundle object, not raw source
bytes — see [canonical-form.md](canonical-form.md).
