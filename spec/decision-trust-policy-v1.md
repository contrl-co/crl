# CRL Decision Trust Policy v1

This document and [`decision-trust-policy-v1.schema.json`](decision-trust-policy-v1.schema.json)
define the public-key and role policy used to trust CRL decision-record
signatures. The policy contains public verification material only. Private-key
generation, storage, access, recovery, and destruction remain deployment
controls and must never appear in a policy document.

## Trust boundary

The verifier, not the decision record, selects the policy for the record's
exact `context.domain`. The caller must obtain the policy from an approved
configuration channel and pin its `policy_hash`. A policy supplied by the
record or another untrusted input is not a trust root.

Policy resolution must produce exactly one approved policy whose domain and
validity interval match the verifier's trusted clock. No match or overlapping
matches are an error. A record's self-asserted time must never select an older
policy, because that would permit downgrade around a later key revocation.
Deployments must use distinct domains and keys across production, staging, and
test environments so a valid lower-environment signature cannot cross the
trust boundary.

The version and schema name are exact. A verifier must reject an unknown
version; it must not downgrade or guess. The policy is closed to unknown
fields.

## Roles and keys

Every role has a positive signature threshold and a maximum delay after the
record's `created_at`. Every key is bound to exactly one role, one key ID, one
Ed25519 public key, and one validity window. Policy validation rejects duplicate
roles, key IDs, or public-key bytes, and rejects a role threshold larger than
the number of keys authorized for that role.

Arrays are canonical: `roles` is ordered by `role`, `keys` by `key_id`, and
extension names lexicographically. Non-canonical order is invalid.

## Policy and key time

Intervals are half-open:

```text
policy: valid_from <= verification_time < valid_until
key:    not_before <= signature.signed_at < not_after
```

`policy.created_at` cannot be after `valid_from`. A signature cannot predate
the record, exceed its role's maximum delay, or be more than
`max_clock_skew_seconds` ahead of the verifier's clock.

If `revoked_at` is present, the key is untrusted for every signature. The time
is audit metadata, not a cutoff based on `signed_at`: that timestamp is asserted
by the signer, so a compromised key could backdate a new signature. Decision
trust-policy v1 has no independently trusted timestamp proof and therefore
cannot preserve pre-revocation trust safely. `revoked_at` may fall outside the
key validity window when compromise is discovered after expiry. Retaining an
expired, unrevoked public key permits historical verification; it does not
reactivate the key.
Expired or superseded policies remain audit artifacts and are not valid current
trust roots.

## Extensions

Every record extension must appear in `allowed_extensions`, and every
`required_extensions` entry must appear in both the allow-list and the record.
Unknown or missing required extensions fail trust verification closed.

## Policy hash and recovery

The policy is content-addressed:

```text
unsigned_policy = every top-level field except policy_hash
policy_hash = SHA256("crl-decision-trust-policy/v1" || 0x00 || CanonicalJSON(unsigned_policy))
```

The hash proves integrity, not authority. Authority comes from the caller's
pinned configuration. Rotation adds a new key or policy version before the old
window closes. Compromise recovery publishes a new pinned policy, records
`revoked_at`, removes signing access to the old private key, re-verifies or
re-signs affected records, and retains the old public policy for audit.

## Verification result

Trust succeeds only when every supplied signature is structurally valid,
authorized for its role, inside all time bounds, mathematically valid over the
domain-separated decision signature payload, and every role threshold is met.
An unsigned record, an unknown key or role, an invalid extra signature, or an
absent policy fails closed.
