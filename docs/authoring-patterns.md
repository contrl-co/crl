# Authoring Patterns

Common shapes for CRL v1 bundles, and when to reach for each. Every
example compiles (CI checks), and the two counterexamples provably do
not. Runnable variants with facts files live in
[examples/](../examples/README.md).

## One rule, one evidence bundle

```crl
crl v1
package examples.permits
bundle permit.application

rule permit_application
    target permit.application
    collector application_file municipality file_upload from /bundles/application.json
        signal application_complete bool from application.complete ttl 30d
        signal permit_hold_active bool from permit.hold_active ttl 30d
    need application_complete == true
    block permit_hold_active
    quorum application_file
```

Use this shape when one uploaded evidence bundle proves one aspect.

## Long-lived evidence with temporal bounds

```crl
crl v1
package examples.utility

bundle buildability.power {
    rule power_to_site {
        target utility.power
        collector utility_record utility file_upload from /bundles/power.json schema utility_power_v1 {
            signal power_built bool from power.built ttl 10y
            signal capacity_kw number from power.capacity_kw unit kw ttl 10y
            signal confirmed_at time from power.confirmed_at ttl 10y
            signal construction_start time from project.construction_start optional ttl 30d
            signal grid_hold bool from power.grid_hold ttl 30d
        }
        need power_built == true
        need capacity_kw >= 2000
        need confirmed_at within 10y before construction_start
        need confirmed_at age <= 10y
        block grid_hold
        quorum utility_record
    }
}
```

Use this shape for evidence gathered once that stays relevant to later
checks. The rule is inert on its own — it authorizes something only
when a host binds it to something it governs.

## Multiple rules under a final policy

```crl
crl v1
package examples.launch
bundle permit.launch

need launch_ready == true
quorum launch_ready

rule application_ready
    target permit.application
    collector application_file municipality file_upload from /bundles/application.json
        signal application_complete bool from application.complete ttl 30d
        signal permit_hold_active bool from permit.hold_active ttl 30d
    need application_complete == true
    block permit_hold_active
    quorum application_file

rule capital_ready
    target finance.capital
    collector capital_record finance webhook from /bundles/capital.json
        signal committed_usd number from capital.committed_usd ttl 30d
        signal escrow_open bool from capital.escrow_open ttl 30d
    need committed_usd >= 250000
    need escrow_open == true
    quorum capital_record

cluster launch_ready
    rules application_ready + capital_ready
    quorum application_ready & capital_ready
```

Because this bundle has global predicates, they alone decide it — and
every rule and cluster must be reachable from them. In indentation
form the final policy comes first: an unindented predicate directly
after a rule body would belong to that rule instead.

## Independent corroboration (count quorum)

```crl
crl v1
package examples.inspection
bundle construction.inspection

rule inspection_ready
    target construction.inspection
    collector field_upload inspection file_upload from /bundles/inspection.json
        signal inspection_passed bool from inspection.passed ttl 14d
    collector permit_registry registry webhook from /bundles/permit.json
        signal permit_current bool from permit.current ttl 30d
    collector utility_record utility webhook from /bundles/utility.json
        signal service_available bool from utility.service_available ttl 30d
    need inspection_passed == true
    need permit_current == true
    need service_available == true
    quorum count(field_upload, permit_registry, utility_record) >= 2
```

Use count quorum when more than one independent evidence source can
support the same authorization. `quorum 2 of 3 a b c` is equivalent
sugar.

The count is over sources that are **currently fresh**: a collector
counts only while the signals it declares are inside their window, so
the rule above reads "two of these three sources corroborate, right
now". A source that goes stale drops out of the count and the other two
still authorize; when staleness alone is what puts the count below the
threshold, the decision is `EXPIRED` rather than
`INSUFFICIENT_EVIDENCE`, which tells the consumer to re-collect rather
than to go find another source.

The count is only honest when the collectors are genuinely distinct
sources. Two collectors that read the **same** `source` do not
corroborate independently — counting both overstates how many separate
sources agree. Compilation rejects this as **CRL121**. Give each independent
input its own source, or count only over distinct sources.

## Multi-party acceptance (counterparty attestation)

A public-private partnership milestone often must not authorize until
named parties have accepted it — a granting authority *and* a
concessionaire, or two of three where a lender's technical adviser can
stand in for one of them. CRL expresses this with what it already has:
`attestation` is a `connector_kind`, each accepting party is its own
collector, and quorum spans parties exactly as it spans any other
evidence source. There is no separate signature or approval construct.

```crl
crl v1
package examples.concession
bundle milestone.acceptance

rule milestone_accepted
    target concession.milestone
    collector authority_acceptance granting_authority attestation from /bundles/authority-acceptance.json
        signal authority_accepted bool from acceptance.accepted ttl 90d
    collector concessionaire_acceptance concessionaire attestation from /bundles/concessionaire-acceptance.json
        signal concessionaire_accepted bool from acceptance.accepted ttl 90d
    need authority_accepted == true
    need concessionaire_accepted == true
    quorum authority_acceptance & concessionaire_acceptance
```

Each party attests through its own collector against its own source,
so each acceptance is an independent input with its own freshness
contract — the `CRL211` distinctness rule above applies here too. The
`need` predicates check what was accepted; the quorum checks that both
parties' attestations are present at all.

An acceptance that has not arrived is a missing fact, not a `false`
one. The rule yields `INSUFFICIENT_EVIDENCE` and authorizes nothing —
acceptance gates effect, and this fails closed. Silence is never read
as consent, and no ordering of arrivals can produce a decision that
one of the named parties never made.

When one party's acceptance may be substituted, give each acceptance
its own rule and count the rules in the bundle's final policy. Count
quorum at rule scope counts collectors; at global scope it counts
rules, which is what lets an entire party's acceptance be optional.

```crl
crl v1
package examples.concession
bundle milestone.acceptance

quorum 2 of 3 authority_accepts concessionaire_accepts adviser_accepts

rule authority_accepts
    target concession.milestone
    collector authority_acceptance granting_authority attestation from /bundles/authority-acceptance.json
        signal authority_accepted bool from acceptance.accepted ttl 90d
    need authority_accepted == true
    quorum authority_acceptance

rule concessionaire_accepts
    target concession.milestone
    collector concessionaire_acceptance concessionaire attestation from /bundles/concessionaire-acceptance.json
        signal concessionaire_accepted bool from acceptance.accepted ttl 90d
    need concessionaire_accepted == true
    quorum concessionaire_acceptance

rule adviser_accepts
    target concession.milestone
    collector adviser_acceptance technical_adviser attestation from /bundles/adviser-acceptance.json
        signal adviser_accepted bool from acceptance.accepted ttl 90d
    need adviser_accepted == true
    quorum adviser_acceptance
```

Each rule proves one party accepted: present evidence, fresh, and
affirmative. The final policy needs any two of the three to have done
so, so the adviser can substitute for either counterparty. A party who
has not accepted contributes nothing to the count — an unproven rule
is a definite `false` and can never be negated into a clearance.

If one party is not substitutable, say so in boolean form instead. Here
the granting authority must always accept, and the adviser may stand in
only for the concessionaire:

```crl
crl v1
package examples.concession
bundle milestone.acceptance

quorum authority_accepts & (concessionaire_accepts | adviser_accepts)

rule authority_accepts
    target concession.milestone
    collector authority_acceptance granting_authority attestation from /bundles/authority-acceptance.json
        signal authority_accepted bool from acceptance.accepted ttl 90d
    need authority_accepted == true
    quorum authority_acceptance

rule concessionaire_accepts
    target concession.milestone
    collector concessionaire_acceptance concessionaire attestation from /bundles/concessionaire-acceptance.json
        signal concessionaire_accepted bool from acceptance.accepted ttl 90d
    need concessionaire_accepted == true
    quorum concessionaire_acceptance

rule adviser_accepts
    target concession.milestone
    collector adviser_acceptance technical_adviser attestation from /bundles/adviser-acceptance.json
        signal adviser_accepted bool from acceptance.accepted ttl 90d
    need adviser_accepted == true
    quorum adviser_acceptance
```

The `ttl` on an attestation signal is a governance decision worth
making explicitly: it is how long an acceptance stays good for. Under
`ttl 90d` an acceptance is usable for ninety days after it was
observed; past that the rule reports `EXPIRED` until the party attests
again. An acceptance that must be renewed as circumstances change is a
different instrument from one given once and standing indefinitely, and
CRL makes you write down which one you mean. Use `expires <RFC3339>`
instead when acceptance lapses on a fixed calendar date — a consent
period ending with a financial year, say — rather than on a rolling
window from whenever it was given.

## Object-block style

```crl
crl v1
package examples.compliance
bundle compliance.credentials

rule credential_gate {
    target compliance.credentials
    collector credential_review credential file_upload from /bundles/credentials.json {
        signal credential_approved bool from credential.approved ttl 30d
        signal credential_reference string from credential.reference ttl 30d
        signal credential_revoked bool from credential.revoked ttl 30d
    }
    need credential_approved == true
    need credential_reference == "CRD-001"
    block credential_revoked
    quorum credential_review
}
```

Object-block style is useful when teams prefer explicit delimiters
over indentation. Both styles compile to identical canonical text and
hashes.

## Reuse through composition

```crl
crl v1
package examples.permits
bundle permit.ready

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

The constructor and abstract rule expand into `permit_ready` during
compilation; only the concrete expanded rule participates in
authorization.

## Invalid: field uses source path instead of signal name

```crl
# docs-lint: expect-error
crl v1

rule invalid_power
    target utility.power
    collector utility_record utility file_upload from /bundles/power.json
        signal capacity_kw number from power.capacity_kw ttl 10y
    need power.capacity_kw >= 2000
    quorum utility_record
```

The predicate must use `capacity_kw` — the declared signal name — not
the connector-local source path.

## Invalid: ambiguous object name

```crl
# docs-lint: expect-error
crl v1

rule bad_names
    target permit.application
    collector permit_ready registry webhook from /bundles/permit.json
        signal permit_ready bool from permit.ready ttl 30d
    need permit_ready == true
    quorum permit_ready
```

This tries to use `permit_ready` as both a collector and a signal; CRL
rejects ambiguous proof subjects.
