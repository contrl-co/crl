# CRL v1 Evaluation Semantics

A compiled bundle is evaluated against **facts** — a flat map of
normalized names to values — at an explicit evaluation instant
(the *clock*). The result is exactly one of five outcomes.

## Facts

- A signal's value is looked up under its (lowercase) name:
  `facts["application_complete"]`.
- A signal's observation time, required by `ttl` freshness, is looked
  up under `observed_at.<signal>` as an RFC3339 timestamp.
- Quorum subjects are looked up under the subject name and, failing
  that, under `provider.<subject>`, `provider:<subject>`,
  `rule.<subject>`, and `cluster.<subject>`.
- `time` values are RFC3339 strings (or native timestamps when
  embedding the Go API).

A subject is **truthy** when its fact is boolean `true`, a string
that is non-empty after trimming whitespace, or a non-zero number.

## The five outcomes

| Outcome | Meaning |
|---|---|
| `AUTHORIZED` | every required condition proved against present, fresh evidence |
| `DENIED` | a required condition evaluated against present, fresh evidence and does not hold |
| `BLOCKED` | an explicit blocker is active |
| `INSUFFICIENT_EVIDENCE` | a required fact is absent, or a quorum is out of reach on the evidence present |
| `EXPIRED` | required evidence exists but its freshness cannot be proven (stale, missing or unparseable observation time, or no evaluation clock), including a quorum that only staleness keeps below its threshold |

Only `AUTHORIZED` advances anything; the other four all withhold.
Consumers must handle all five spellings exactly as written.

### Outcome selection

Every predicate evaluation produces a *check* with a pass/fail flag
and, on failure, a reason. If all relevant checks pass, the outcome is
`AUTHORIZED`. Otherwise the outcome is chosen by scanning the failure
reasons in a fixed precedence:

1. any expiry failure → `EXPIRED`
2. else any active blocker → `BLOCKED`
3. else any missing fact or unmet quorum → `INSUFFICIENT_EVIDENCE`
4. else → `DENIED`

`DENIED` is therefore the residual outcome: evidence was present and
fresh, and a condition simply does not hold. The same precedence is
applied per rule and for the bundle as a whole. A cluster applies it to
its own checks first, then raises its result to the most severe outcome
among its unauthorized member rules (ranked
`EXPIRED` > `BLOCKED` > `INSUFFICIENT_EVIDENCE` > `DENIED`): a cluster
whose own predicates all pass but which is unauthorized because a member
failed reports that member's outcome, not a generic `DENIED`.

## Freshness (fail closed)

Freshness is the load-bearing safety property: **evidence whose
freshness cannot be proven never satisfies a rule.**

For a signal declared with `ttl <duration>`:

- fresh ⇔ `observed_at.<signal> <= clock <= observed_at.<signal> + duration`;
- a missing or unparseable `observed_at.<signal>` fact means the age
  is unknowable → **expired**;
- an `observed_at.<signal>` later than the clock cannot prove freshness
  — nothing is observed in the future — so it is treated as unprovable
  → **expired**, rather than granting a full ttl window from a future
  instant;
- evaluating without a clock (the clockless entry point) means
  freshness cannot be evaluated → **expired**.

For a signal declared with `expires <RFC3339>`: fresh ⇔ the clock is
not past the instant; no observation time is needed, but a clock still
is.

Staleness is enforced everywhere evidence is consulted, not just in
`need`:

- a stale subject does not count toward a **count quorum**;
- a stale subject in a **boolean quorum expression** is *unknown* —
  never true, so it cannot carry a branch, and never false, so a
  negated stale subject does not read as "cleared";
- a stale `time` signal used as a **temporal reference** fails the
  temporal check as expired.

A quorum subject that names a **collector** is judged on the signals
that collector declares. A collector has no observation time of its
own — its subject fact is bare presence — so without this a quorum over
collectors would consult no expiry at all and evidence of any age would
satisfy it. A collector's signal counts toward that judgement once the
facts put it *in play* by carrying either its value or an
`observed_at.<signal>` entry; a signal that was never supplied is
absent, not stale. Supplying a value without an observation time
therefore leaves the age unprovable → **expired**.

Subjects that name a rule or a cluster need no separate gate: a rule
already fails its own checks on stale evidence and publishes `false`,
which removes it from the quorum.

## Predicates

### need (comparison)

Look up the field; a missing fact fails as unknown
(`INSUFFICIENT_EVIDENCE` at outcome selection). A stale fact fails as
expired. Otherwise compare with the declared operator; a type
mismatch between the fact's runtime value and the declared kind fails
the check. Numbers compare numerically; `bool` and `string` support
only `==`/`!=`.

### need (temporal)

The field must be a declared `time` signal; the fact must parse as a
timestamp. The reference (`now`, an RFC3339 literal, or another
`time` signal) is resolved at evaluation time. Relations:

- `before` / `after`: strict comparison against the reference;
- `within D before R`: `R - D <= field <= R` (inclusive);
- `within D after R`: `R <= field <= R + D` (inclusive);
- `age <= D` / `age >= D`: the field's age relative to the clock;
  requires a clock and a field not in the future.

A failed `within … before/after` or `age <=` check reports as
expired (the evidence exists but is outside its window); a failed
`before`/`after`/`age >=` check reports as a plain failed condition
(`DENIED` at outcome selection).

### block

The field must be a `bool` or `number` signal. The blocker is active
when the fact is `true` or non-zero; an active blocker fails the check
as blocked. An absent fact is unknown (as with `need`), and a stale
fact is expired.

An active blocker with fresh evidence always reports `BLOCKED`.
Staleness is checked first, as with every signal: a stale blocker fact
reports `EXPIRED`. What never happens is `EXPIRED` inferred from the
field's *name* — expiry comes only from declared expiry semantics
(`ttl`/`expires` or temporal predicates), never from naming
conventions; the linter flags expiry-shaped blocker names (`CRL208`).

### quorum (count form)

Count the listed subjects that are present — truthy **and** fresh —
and pass when `count >= n`. A stale subject *reduces* the count; it
does not disqualify the quorum. Two fresh sources out of three
therefore satisfy a threshold of two even when the third has gone
stale, which is what "two of three independent sources, currently
fresh" means.

An unmet quorum is spelled by what is missing. If re-observing the
stale subjects — at the values they already carry — would reach the
threshold, the shortfall *is* the staleness and the check fails as
expired (`EXPIRED`). Otherwise the threshold is out of reach on the
evidence itself and the check fails as quorum-not-met
(`INSUFFICIENT_EVIDENCE`). One fresh source of three with two stale is
`EXPIRED`; one fresh source of three with two that never reported is
`INSUFFICIENT_EVIDENCE`.

### quorum (boolean form)

Evaluate the expression with three-valued (Kleene) logic over subject
state. A subject is **true** when its fact is present, truthy, and
fresh, **false** when present and fresh but not truthy, and **unknown**
when no fact entry exists under the subject name or any of its
`provider.`, `provider:`, `rule.`, or `cluster.` spellings — or when
its evidence is present but not provably fresh. `not unknown` is
unknown; `unknown and false` is false; `unknown or true` is true; every
other combination involving unknown is unknown. The check passes only
when the expression evaluates to **true**, so neither missing nor stale
evidence can be negated into a clearance (`not <absent>` and
`not <stale>` do not read as satisfied), while an `or` with one
present, fresh, satisfied branch still passes.

Staleness therefore behaves the same way it does in the count form: it
withdraws one subject rather than disqualifying the expression, so a
two-of-three written as a disjunction still passes on the branches whose
evidence is fresh. An unmet expression is spelled by the same test as
the count form — expired when re-observing the stale subjects would
satisfy it, quorum-not-met otherwise.

## What a predicate may reference

Visibility depends on where the predicate sits:

| Predicate scope | `need`/`block` fields | quorum subjects (boolean) | quorum subjects (count) |
|---|---|---|---|
| rule | signals, kernel facts | signals, collectors, kernel facts | collectors |
| cluster | + rule names (as booleans) | + rule names | + rule names |
| bundle (global) | + cluster names (as booleans) | + cluster names | + cluster names |

Signals declared in *any* rule are visible bundle-wide: a rule may
reference a signal declared under another rule's collector, and its
freshness contract applies wherever it is used.

### Kernel facts

Exactly one reserved fact name exists in v1: `min_provider_trust`, a
`number` the evaluating host may supply. It may be referenced like a
signal without being declared. Hosts that do not supply it simply
leave predicates over it unprovable (absent fact). No other names are
reserved.

## Rules, clusters, and the bundle

Evaluation is layered, in declaration order:

1. **Rules** evaluate independently. Each rule's outcome is computed
   from its own checks, and its boolean authorization is then
   published into the working facts under `<rule_name>` and
   `rule.<rule_name>` — later predicates can reference rules as
   booleans.
2. **Clusters** evaluate after rules: a cluster authorizes when every
   member rule authorized and every cluster predicate passes. Cluster
   authorization is published under `<cluster_name>` and
   `cluster.<cluster_name>`.
3. **Global predicates** (declared at bundle top level) evaluate last,
   against the working facts including the published rule and cluster
   booleans.

Watch the placement in indentation form: an unindented `need`,
`block`, or `quorum` immediately after a rule body still belongs to
that rule (the rule-body carve-out). To declare a global final policy
in indentation form, put it before the rules; in object-block form,
anywhere inside the `bundle { }` block but outside every rule works.

**Bundle authorization**: if the bundle declares any global
predicates, they alone decide — the global predicates are the final
policy, and rules/clusters contribute through the booleans the final
policy references. With no global predicates, the bundle authorizes
only when every rule and every cluster authorized.

Because an unreferenced rule would be silently dead under a final
policy, the compiler rejects a bundle whose global predicates do not
reach every rule and cluster (each rule must be referenced directly or
via a referenced cluster):

```crl
# docs-lint: expect-error
crl v1
bundle example.deadrule {
	rule reachable {
		target a.b
		collector c1 org api from /x.json {
			signal s1 bool from x.y ttl 30d
		}
		need s1 == true
	}
	rule dead {
		target a.c
		collector c2 org api from /y.json {
			signal s2 bool from y.z ttl 30d
		}
		need s2 == true
	}
	need reachable == true
}
```

The linter separately warns (`CRL203`) when multiple rules or clusters
have **no** final policy, since "everything must authorize" may not be
what the author meant.

A final policy must be monotone in every rule and cluster: a rule or
cluster may be *required* (`need r == true`, an un-negated quorum
subject, a count-quorum provider) but never gated on *failing*. Because
an unproven rule or cluster is a definite `false`, a negated reference to
it — `quorum not r`, `block r`, `need r == false`, or even `r & !r2` —
is satisfied precisely when its evidence is absent, which would
authorize a decision with no evidence. All such policies are compile
errors, and the same rule applies to a cluster's own predicates (a
cluster publishes a boolean the policy consumes). (Negating a *signal*
is fine: an absent signal is unknown, not false, so it fails closed.)

### Rule inheritance

`extends` is compile-time expansion, not dynamic dispatch:

- the parent chain is resolved first (cycles are compile errors);
- the parent's collectors and predicates are **prepended** to the
  child's, in parent-then-child order;
- the child's `target` wins; a child without one inherits the
  parent's;
- abstract rules are expanded away — they do not exist in the compiled
  bundle, are not evaluated, and are not visible to quorum or
  cluster references.

The expansion is observable in the canonical text: a concrete rule
renders with its full inherited body, and the hash covers the expanded
form.

## The clock

Evaluation takes an explicit instant. The clockless entry point exists
for pure structural checks and **fails closed on every
time-dependent rule**: all `ttl`/`expires` signals evaluate as
expired, and temporal predicates cannot prove anything. A host that
wants freshness genuinely evaluated must pass a real clock — and
record it, because the decision is a function of it.

There is no hidden `time.Now()` anywhere in compilation or
evaluation; determinism extends to time.

## Aspect

An evaluation reports the *aspect* it authorizes: the rule's `target`
when the bundle contains exactly one rule and nothing else, and the
fixed string `rule_bundle` otherwise.
