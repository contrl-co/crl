# Decision security and key operations

This is the production security contract for CRL decision records. CRL verifies
public inputs; the consuming platform owns private keys, approved policy pins,
the trusted clock, and durable replay state.

## Trust boundaries

- A decision record is untrusted until `VerifyForUse` succeeds.
- Trust and use policies come from approved configuration, never from the
  record. Their content hashes are pinned by the caller.
- Verification time comes from the runtime's trusted clock, not record data.
- Replay state must be durable and atomically consume both record and
  correlation identities. `MemoryReplayStore` is not a production store.
- Private keys never enter this repository, a policy document, process
  arguments, logs, test fixtures, or build artifacts.

## Inventory

| Asset or trust input | Environment boundary | Accountable owner | Production location |
|---|---|---|---|
| Decision Ed25519 private keys, one per domain and role | Production, staging, and test use different keys | Security owner and role operator | Non-exportable KMS/HSM key; provisioning is tracked in CONTRL-14 |
| Decision public keys, roles, thresholds, validity, and revocation | Exact decision domain | Security owner and service owner | Versioned trust policy in approved configuration, pinned by `policy_hash` |
| Context, freshness, and replay limits | Exact decision domain | Service owner and security reviewer | Versioned use policy in approved configuration, pinned by `policy_hash` |
| Trusted verification clock | Runtime environment | Platform operations | Authenticated, monitored host time; record timestamps are not authoritative |
| Replay claims and store credential | Runtime environment and decision domain | Platform operations | Durable transactional store and secret manager; never the in-memory reference store |
| Release signing identity | Canonical repository release workflow | Repository maintainers | CI OIDC identity, Fulcio certificate, and Rekor entry; no long-lived release key |
| Repository workflow token | Single workflow run | Repository maintainers | Ephemeral, least-privilege `GITHUB_TOKEN` |

SHA-256 source, bundle, record, trace, and policy hashes are integrity evidence,
not secrets or trust roots. Authority comes from an approved pin or signature
policy.

## Threat model

The protected outcome is that only a correct, current, authorized decision can
be used once in its exact application context.

| Attacker capability | Required control |
|---|---|
| Modify or fabricate a record | Strict parsing, canonical hashes, and Ed25519 verification |
| Substitute an attacker policy | Caller-owned policy source and exact hash pin |
| Replay a valid authorization | Atomic durable record-and-correlation consumption |
| Move a record across tenant, subject, request, or environment | Exact domain, subject, and correlation binding; separate environment keys |
| Backdate after key compromise | Current policy selected by trusted time; a revoked key invalidates every v1 signature |
| Downgrade version or algorithm | Exact schema/version checks; Ed25519 only in v1; no fallback |
| Steal a signing key | Non-exportable storage, least privilege, separated roles, rotation, and immediate revocation |
| Compromise the runtime or policy channel | Separation of duties, attributable policy approval, immutable audit evidence, and incident recovery |

CRL does not defend a relying process that is already controlled by the
attacker. That process owns policy distribution, clock integrity, replay-store
availability, and enforcement after verification.

## Key lifecycle

### Create and activate

1. Generate a new Ed25519 key inside the approved KMS/HSM. Do not export the
   private key.
2. Record a unique key ID, domain, role, environment, public-key fingerprint,
   owner, creation evidence, validity window, and recovery disposition.
3. Add the public key to a new trust-policy version. Obtain independent
   security and service-owner approval.
4. Deploy and verify the new policy hash pin before granting signing access.
5. Activate the signer with only the required role and environment permission.

### Planned rotation

1. Add the replacement key and deploy the new policy pin.
2. Switch signing to the replacement and verify new records end to end.
3. Remove signing access from the old key after the maximum signing and use
   windows pass.
4. Retain the old, unrevoked public key when historical verification is
   required. Expiry prevents new use without erasing audit evidence.

### Loss, revocation, and compromise

- Lost but not exposed: disable signing, activate a replacement, and retain the
  old public key for historical verification. Restore the private key only from
  an approved, access-logged recovery mechanism.
- Suspected or confirmed exposure: disable and quarantine the key, rotate every
  credential that could access it, and publish a newly approved current policy
  with `revoked_at`. V1 then rejects all records signed by that key, including
  apparently older records, because signer-controlled timestamps cannot prove
  pre-compromise origin.
- Re-verify affected records from original evidence and reissue only those that
  still produce `AUTHORIZED`. Do not copy the old signature or replay state.
- Preserve the old policy, affected hashes, approval record, timeline, and
  remediation evidence. Never rewrite or reuse a released policy version.

Destroy retired private material only after retention and recovery requirements
are met. Destruction requires KMS/HSM evidence and independent confirmation.

## Production gate

Production remains blocked until CONTRL-14 records the named human owners,
approved KMS/HSM, environment-specific keys, policy distribution path, durable
replay store, clock monitoring, recovery test, and independent security review.
No code or document in this repository provisions those controls.
