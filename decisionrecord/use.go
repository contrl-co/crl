package decisionrecord

import (
	"context"
	"errors"
	"fmt"
	"time"

	crl "gitlab.com/contrl-group/crl"
)

var (
	// ErrContext marks a use-policy, request-context, or freshness mismatch.
	ErrContext = errors.New("decision record: context verification failed")
	// ErrNotAuthorized marks a correct, trusted decision whose outcome does not
	// permit the relying action.
	ErrNotAuthorized = errors.New("decision record: outcome is not authorized")
)

// ExpectedContext is trusted request identity supplied by the relying caller.
type ExpectedContext struct {
	Domain        string `json:"domain"`
	Subject       string `json:"subject"`
	CorrelationID string `json:"correlation_id"`
}

// ContextEvidence records the pinned use policy and exact context that passed.
type ContextEvidence struct {
	PolicyID      string `json:"policy_id"`
	PolicyVersion int64  `json:"policy_version"`
	PolicyHash    string `json:"policy_hash"`
	ReplayScope   string `json:"replay_scope"`
	VerifiedAt    string `json:"verified_at"`
	Domain        string `json:"domain"`
	Subject       string `json:"subject"`
	CorrelationID string `json:"correlation_id"`
	RecordID      string `json:"record_id"`
	RecordHash    string `json:"record_hash"`
	EvaluationAt  string `json:"evaluation_at"`
	CreatedAt     string `json:"created_at"`
}

// UseRequest supplies every caller-owned input required to verify and
// atomically consume a decision record.
type UseRequest struct {
	TrustPolicy     *TrustPolicy
	TrustPolicyHash string
	UsePolicy       *UsePolicy
	UsePolicyHash   string
	Expected        ExpectedContext
	VerifiedAt      time.Time
	ReplayStore     ReplayStore
}

// UseEvidence combines signature trust with context and freshness evidence.
type UseEvidence struct {
	Trust   TrustEvidence   `json:"trust"`
	Context ContextEvidence `json:"context"`
}

// VerifyContext verifies integrity, a pinned and current use policy, exact
// caller context, and all record freshness bounds. It does not consume replay
// state and therefore must not be used alone before an action.
func VerifyContext(record *Record, policy *UsePolicy, expectedPolicyHash string, expected ExpectedContext, verifiedAt time.Time) (ContextEvidence, error) {
	if err := VerifyIntegrity(record); err != nil {
		return ContextEvidence{}, err
	}
	return verifyContext(record, policy, expectedPolicyHash, expected, verifiedAt)
}

// VerifyForUse runs every verification layer, requires AUTHORIZED, and
// atomically consumes replay state only after all stateless checks succeed.
func VerifyForUse(ctx context.Context, record *Record, request UseRequest) (UseEvidence, error) {
	if ctx == nil {
		return UseEvidence{}, replayStoreError("context is nil")
	}
	if err := VerifyIntegrity(record); err != nil {
		return UseEvidence{}, err
	}
	trustEvidence, err := verifyTrust(record, request.TrustPolicy, request.TrustPolicyHash, request.VerifiedAt)
	if err != nil {
		return UseEvidence{}, err
	}
	if err := verifyDecision(record); err != nil {
		return UseEvidence{}, err
	}
	contextEvidence, err := verifyContext(record, request.UsePolicy, request.UsePolicyHash, request.Expected, request.VerifiedAt)
	if err != nil {
		return UseEvidence{}, err
	}
	if record.Evaluation.Outcome != crl.Authorized {
		return UseEvidence{}, fmt.Errorf("%w: got %s", ErrNotAuthorized, record.Evaluation.Outcome)
	}
	if request.ReplayStore == nil {
		return UseEvidence{}, replayStoreError("store is required")
	}
	claim := ReplayClaim{
		Domain:        record.Context.Domain,
		Subject:       record.Context.Subject,
		CorrelationID: record.Context.CorrelationID,
		RecordID:      record.RecordID,
		RecordHash:    record.RecordHash,
	}
	if err := request.ReplayStore.Consume(ctx, claim); err != nil {
		if errors.Is(err, ErrReplay) || errors.Is(err, ErrReplayStore) {
			return UseEvidence{}, err
		}
		return UseEvidence{}, replayStoreError("consume: %v", err)
	}
	return UseEvidence{Trust: trustEvidence, Context: contextEvidence}, nil
}

func verifyContext(record *Record, policy *UsePolicy, expectedPolicyHash string, expected ExpectedContext, verifiedAt time.Time) (ContextEvidence, error) {
	if err := VerifyUsePolicy(policy, expectedPolicyHash); err != nil {
		return ContextEvidence{}, err
	}
	if verifiedAt.IsZero() {
		return ContextEvidence{}, contextError("verification time is required")
	}
	if expected.Domain == "" || expected.Subject == "" || expected.CorrelationID == "" {
		return ContextEvidence{}, contextError("expected domain, subject, and correlation_id are required")
	}
	validFrom, err := time.Parse(time.RFC3339Nano, policy.ValidFrom)
	if err != nil {
		return ContextEvidence{}, usePolicyError("valid_from: %v", err)
	}
	validUntil, err := time.Parse(time.RFC3339Nano, policy.ValidUntil)
	if err != nil {
		return ContextEvidence{}, usePolicyError("valid_until: %v", err)
	}
	if verifiedAt.Before(validFrom) || !verifiedAt.Before(validUntil) {
		return ContextEvidence{}, contextError("use policy is not active at verification time")
	}
	if expected.Domain != policy.Domain || record.Context.Domain != expected.Domain {
		return ContextEvidence{}, contextError("domain does not match caller and policy")
	}
	if record.Context.Subject != expected.Subject {
		return ContextEvidence{}, contextError("subject does not match caller context")
	}
	if record.Context.CorrelationID != expected.CorrelationID {
		return ContextEvidence{}, contextError("correlation_id does not match caller context")
	}

	evaluatedAt, err := time.Parse(time.RFC3339Nano, record.Evaluation.At)
	if err != nil {
		return ContextEvidence{}, contextError("evaluation.at: %v", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil {
		return ContextEvidence{}, contextError("created_at: %v", err)
	}
	clockLimit := verifiedAt.Add(time.Duration(policy.MaxClockSkewSeconds) * time.Second)
	if evaluatedAt.After(clockLimit) {
		return ContextEvidence{}, contextError("evaluation.at is too far in the future")
	}
	if createdAt.After(clockLimit) {
		return ContextEvidence{}, contextError("created_at is too far in the future")
	}
	if createdAt.Sub(evaluatedAt) > time.Duration(policy.MaxRecordDelaySeconds)*time.Second {
		return ContextEvidence{}, contextError("record creation exceeds the evaluation delay limit")
	}
	if verifiedAt.After(evaluatedAt.Add(time.Duration(policy.MaxEvaluationAgeSeconds) * time.Second)) {
		return ContextEvidence{}, contextError("evaluation is too old")
	}
	if verifiedAt.After(createdAt.Add(time.Duration(policy.MaxRecordAgeSeconds) * time.Second)) {
		return ContextEvidence{}, contextError("record is too old")
	}

	return ContextEvidence{
		PolicyID:      policy.PolicyID,
		PolicyVersion: policy.Version,
		PolicyHash:    policy.PolicyHash,
		ReplayScope:   policy.ReplayScope,
		VerifiedAt:    verifiedAt.UTC().Format(time.RFC3339Nano),
		Domain:        record.Context.Domain,
		Subject:       record.Context.Subject,
		CorrelationID: record.Context.CorrelationID,
		RecordID:      record.RecordID,
		RecordHash:    record.RecordHash,
		EvaluationAt:  record.Evaluation.At,
		CreatedAt:     record.CreatedAt,
	}, nil
}

func contextError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrContext, fmt.Sprintf(format, args...))
}
