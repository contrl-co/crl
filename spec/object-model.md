# CRL Object Model

CRL v1 defines the authoring model for rule bundles: package and bundle
declarations, constructors, abstract rules, concrete rules, collectors,
typed signals, predicates, clusters, quorum, and deterministic canonical
compilation.

## Authoring objects

| Object | Purpose |
|--------|---------|
| `package` | Names the authoring namespace for a bundle. |
| `bundle` | Names the source-file bundle and may wrap its contents in a block. |
| `constructor` | Defines reusable collector and predicate structure that expands into concrete rules. |
| `abstract rule` | Defines reusable rule structure and may extend another constructor or abstract rule. |
| `rule` | Defines a concrete authorization rule and may extend a constructor or abstract rule. |
| `collector` | Defines an external evidence source: provider type, connector kind, source, and optional schema. |
| `signal` | Defines a typed fact, optional unit, optional requiredness, and a freshness rule. |
| `cluster` | Composes concrete rule results. |
| `need` | Defines comparison or temporal proof obligations. |
| `block` | Defines fail-closed blockers. |
| `quorum` | Defines Boolean or count-based source/rule/cluster quorum. |

## Composition model

Constructors and abstract rules are authoring-time objects. They do not
authorize by themselves and do not appear as runtime rules. During
compilation, their collectors and predicates are expanded into concrete
rules before normalization and hashing.

```crl
crl v1
package examples.permits
bundle permit.launch

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

rule permit_ready extends no_active_hold {
    target permit.application
    collector application_file municipality file_upload from /bundles/application.json {
        signal application_complete bool from application.complete ttl 30d
    }
    need application_complete == true
    quorum application_file & registry_record
}
```

The compiled canonical bundle contains only the expanded concrete rule.

## Temporal predicates

CRL supports `time` signals and temporal `need` forms:

```text
signal confirmed_at time from power.confirmed_at ttl 10y
signal construction_start time from project.construction_start optional ttl 30d

need confirmed_at before construction_start
need confirmed_at after "2026-01-01T00:00:00Z"
need confirmed_at within 10y before construction_start
need confirmed_at within 30d after permit_issued_at
need confirmed_at age <= 10y
need confirmed_at age >= 1d
```

Temporal references may be another visible `time` signal, an RFC3339
timestamp, or `now`. Age checks require the evaluator to receive a
non-zero evaluation time — see
[semantics.md](semantics.md#need-temporal).

## Collector and signal contracts

Collectors and signals carry the fields an integration needs to
normalize payloads consistently:

```text
collector utility_record utility file_upload from /bundles/power.json schema utility_power_v1 {
    signal capacity_kw number from power.capacity_kw unit kw ttl 10y
    signal power_built bool from power.built ttl 10y
    signal confirmed_at time from power.confirmed_at ttl 10y
    signal notes string from power.notes optional ttl 30d
}
```

Rules:

- `provider_type` and `connector_kind` are normalized identifiers;
- `schema` is a normalized identifier for the external payload contract;
- signal kinds are `number`, `bool`, `string`, and `time`;
- `unit` is allowed only on `number` signals;
- signals are required by default; `optional` marks a signal as
  non-required for external payload normalization;
- predicates use signal names, not external source field paths.

## Compile contract

All authoring-time objects expand into the normalized bundle before
hashing. Canonical output preserves package, bundle, collector schema,
signal unit, optional markers, temporal predicates, and the expanded
concrete rule content — see
[canonical-form.md](canonical-form.md).

CRL has no runtime side effects. It does not import code, call
services, fetch evidence, mutate state, or run host-language functions.
A host compiles source, evaluates the compiled bundle against facts it
supplies, and owns everything on the other side of that boundary.
