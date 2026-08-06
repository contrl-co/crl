# CRL Decision Use Policy v1

This document and [`decision-use-policy-v1.schema.json`](decision-use-policy-v1.schema.json)
define the caller-approved context, freshness, and replay controls applied after
a decision record passes structural, integrity, trust, and deterministic
decision verification.

## Trust boundary

The verifier selects exactly one current policy for the expected domain from an
approved configuration channel and pins `policy_hash`. The record must not
select its own policy. Policy selection uses the verifier's trusted clock, not a
record timestamp, so a record cannot backdate itself into an older policy.

The relying request supplies the exact expected domain, subject, and correlation
ID. All three must match the signed record byte for byte. A policy match alone
does not establish request context.

## Time policy

The policy independently limits:

- age of `evaluation.at` at verification time;
- age of `created_at` at verification time;
- delay from `evaluation.at` to `created_at`; and
- how far either record time may lead the trusted verification clock.

All boundaries are inclusive. Record parsing already requires
`evaluation.at <= created_at`. Zero is valid and means no elapsed time is
allowed for that bound. The policy itself is active on the half-open interval
`valid_from <= verification_time < valid_until`.

## Replay contract

`replay_scope` is exactly `record-and-correlation` in v1. After every prior
verification layer succeeds, the consumer must atomically consume both:

- `(domain, record_id)`; and
- `(domain, correlation_id)`.

If either identity was consumed, the use fails as a replay. The correlation key
is domain-wide rather than subject-scoped so one request ID cannot be reused for
a different subject. A store error fails closed. Checking and then inserting in
separate operations is invalid because concurrent consumers could both act.

Replay claims must survive process restarts and remain retained for as long as
any approved current or future policy could accept the record. Permanent
retention is safest. A policy change must not revive records whose claims were
expired under an older retention rule. The package memory store is for tests
and single-process tools only; production consumers need a durable store with a
unique transaction or equivalent atomic primitive.

## Policy hash

```text
unsigned_policy = every top-level field except policy_hash
policy_hash = SHA256("crl-decision-use-policy/v1" || 0x00 || CanonicalJSON(unsigned_policy))
```

The hash proves integrity, not authority. Authority comes from the caller's
approved pin. Versions and schemas are exact; verifiers must not guess or
downgrade.
