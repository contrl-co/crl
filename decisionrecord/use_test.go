package decisionrecord

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	crl "gitlab.com/contrl-group/crl"
)

func TestVerifyContext(t *testing.T) {
	record := parseTestRecord(t)
	policy := testUsePolicy(t, record)
	evidence, err := VerifyContext(record, policy, policy.PolicyHash, expectedRecordContext(record), verifierTime())
	if err != nil {
		t.Fatalf("VerifyContext: %v", err)
	}
	if evidence.PolicyHash != policy.PolicyHash || evidence.ReplayScope != policy.ReplayScope || evidence.RecordHash != record.RecordHash || evidence.CorrelationID != record.Context.CorrelationID {
		t.Fatalf("unexpected context evidence: %+v", evidence)
	}
}

func TestVerifyContextRejectsMismatchesAndStaleRecords(t *testing.T) {
	tests := []struct {
		name   string
		want   error
		mutate func(*testing.T, *Record, *UsePolicy, *ExpectedContext, *time.Time)
	}{
		{name: "wrong policy pin", want: ErrUsePolicy, mutate: func(_ *testing.T, _ *Record, policy *UsePolicy, _ *ExpectedContext, _ *time.Time) {
			policy.PolicyHash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
		}},
		{name: "policy not active", want: ErrContext, mutate: func(t *testing.T, _ *Record, policy *UsePolicy, _ *ExpectedContext, _ *time.Time) {
			policy.ValidFrom = verifierTime().Add(time.Second).Format(time.RFC3339Nano)
			rehashUsePolicy(t, policy)
		}},
		{name: "policy expired", want: ErrContext, mutate: func(t *testing.T, _ *Record, policy *UsePolicy, _ *ExpectedContext, _ *time.Time) {
			policy.ValidUntil = verifierTime().Format(time.RFC3339Nano)
			rehashUsePolicy(t, policy)
		}},
		{name: "missing expected context", want: ErrContext, mutate: func(_ *testing.T, _ *Record, _ *UsePolicy, expected *ExpectedContext, _ *time.Time) {
			*expected = ExpectedContext{}
		}},
		{name: "wrong domain", want: ErrContext, mutate: func(_ *testing.T, _ *Record, _ *UsePolicy, expected *ExpectedContext, _ *time.Time) {
			expected.Domain = "contrl.co/other"
		}},
		{name: "wrong subject", want: ErrContext, mutate: func(_ *testing.T, _ *Record, _ *UsePolicy, expected *ExpectedContext, _ *time.Time) {
			expected.Subject = "example:record:other"
		}},
		{name: "wrong correlation", want: ErrContext, mutate: func(_ *testing.T, _ *Record, _ *UsePolicy, expected *ExpectedContext, _ *time.Time) {
			expected.CorrelationID = "01989f7e-7b80-7000-8000-000000000099"
		}},
		{name: "evaluation in future", want: ErrContext, mutate: func(t *testing.T, record *Record, _ *UsePolicy, _ *ExpectedContext, _ *time.Time) {
			future := verifierTime().Add(6 * time.Second).Format(time.RFC3339Nano)
			record.Evaluation.At = future
			record.CreatedAt = future
			rehashRecord(t, record)
		}},
		{name: "record created in future", want: ErrContext, mutate: func(t *testing.T, record *Record, _ *UsePolicy, _ *ExpectedContext, _ *time.Time) {
			record.CreatedAt = verifierTime().Add(6 * time.Second).Format(time.RFC3339Nano)
			rehashRecord(t, record)
		}},
		{name: "record creation delayed", want: ErrContext, mutate: func(t *testing.T, record *Record, _ *UsePolicy, _ *ExpectedContext, verifiedAt *time.Time) {
			record.CreatedAt = verifierTime().Add(61 * time.Second).Format(time.RFC3339Nano)
			rehashRecord(t, record)
			*verifiedAt = verifierTime().Add(61 * time.Second)
		}},
		{name: "evaluation too old", want: ErrContext, mutate: func(_ *testing.T, _ *Record, _ *UsePolicy, _ *ExpectedContext, verifiedAt *time.Time) {
			*verifiedAt = verifierTime().Add(301 * time.Second)
		}},
		{name: "record too old", want: ErrContext, mutate: func(_ *testing.T, _ *Record, _ *UsePolicy, _ *ExpectedContext, verifiedAt *time.Time) {
			*verifiedAt = verifierTime().Add(121 * time.Second)
		}},
		{name: "verification time missing", want: ErrContext, mutate: func(_ *testing.T, _ *Record, _ *UsePolicy, _ *ExpectedContext, verifiedAt *time.Time) {
			*verifiedAt = time.Time{}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := parseTestRecord(t)
			policy := testUsePolicy(t, record)
			expected := expectedRecordContext(record)
			verifiedAt := verifierTime()
			test.mutate(t, record, policy, &expected, &verifiedAt)
			_, err := VerifyContext(record, policy, policy.PolicyHash, expected, verifiedAt)
			if !errors.Is(err, test.want) {
				t.Fatalf("VerifyContext error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestVerifyContextAcceptsExactTimeBoundaries(t *testing.T) {
	record := parseTestRecord(t)
	policy := testUsePolicy(t, record)
	if _, err := VerifyContext(record, policy, policy.PolicyHash, expectedRecordContext(record), verifierTime().Add(120*time.Second)); err != nil {
		t.Fatalf("age boundary: %v", err)
	}

	record = parseTestRecord(t)
	record.CreatedAt = verifierTime().Add(60 * time.Second).Format(time.RFC3339Nano)
	rehashRecord(t, record)
	if _, err := VerifyContext(record, policy, policy.PolicyHash, expectedRecordContext(record), verifierTime().Add(60*time.Second)); err != nil {
		t.Fatalf("record-delay boundary: %v", err)
	}

	record = parseTestRecord(t)
	future := verifierTime().Add(5 * time.Second).Format(time.RFC3339Nano)
	record.Evaluation.At = future
	record.CreatedAt = future
	rehashRecord(t, record)
	if _, err := VerifyContext(record, policy, policy.PolicyHash, expectedRecordContext(record), verifierTime()); err != nil {
		t.Fatalf("clock-skew boundary: %v", err)
	}
}

func TestVerifyForUseConsumesOnlyFullyVerifiedRecord(t *testing.T) {
	record, trustPolicy, _ := trustedTestRecord(t)
	usePolicy := testUsePolicy(t, record)
	store := NewMemoryReplayStore()
	request := testUseRequest(record, trustPolicy, usePolicy, store)

	evidence, err := VerifyForUse(context.Background(), record, request)
	if err != nil {
		t.Fatalf("VerifyForUse: %v", err)
	}
	if evidence.Trust.PolicyHash != trustPolicy.PolicyHash || evidence.Context.PolicyHash != usePolicy.PolicyHash {
		t.Fatalf("unexpected use evidence: %+v", evidence)
	}
	if _, err := VerifyForUse(context.Background(), record, request); !errors.Is(err, ErrReplay) {
		t.Fatalf("second VerifyForUse error = %v, want ErrReplay", err)
	}
}

func TestVerifyForUseDoesNotConsumeFailedVerification(t *testing.T) {
	for _, test := range []struct {
		name   string
		want   error
		mutate func(*testing.T, *Record, *TrustPolicy, map[string]ed25519.PrivateKey, *UseRequest)
	}{
		{name: "untrusted signature", want: ErrSignature, mutate: func(_ *testing.T, record *Record, _ *TrustPolicy, _ map[string]ed25519.PrivateKey, _ *UseRequest) {
			record.Signatures[0].Signature = record.Signatures[1].Signature
		}},
		{name: "incorrect decision", want: ErrDecision, mutate: func(t *testing.T, record *Record, _ *TrustPolicy, keys map[string]ed25519.PrivateKey, _ *UseRequest) {
			record.Evaluation.Outcome = "DENIED"
			rehashRecord(t, record)
			resignAllTestSignatures(t, record, keys)
		}},
		{name: "wrong request context", want: ErrContext, mutate: func(_ *testing.T, _ *Record, _ *TrustPolicy, _ map[string]ed25519.PrivateKey, request *UseRequest) {
			request.Expected.Subject = "example:record:other"
		}},
		{name: "non-authorized outcome", want: ErrNotAuthorized, mutate: func(t *testing.T, record *Record, _ *TrustPolicy, keys map[string]ed25519.PrivateKey, _ *UseRequest) {
			record.Evaluation.Facts["application_complete"] = false
			compiled, err := crl.CompileEdition(record.Rule.Source, record.Rule.Edition)
			if err != nil {
				t.Fatal(err)
			}
			at, err := time.Parse(time.RFC3339Nano, record.Evaluation.At)
			if err != nil {
				t.Fatal(err)
			}
			trace, err := normalizedTrace(compiled.EvaluateAt(record.Evaluation.Facts, at))
			if err != nil {
				t.Fatal(err)
			}
			record.Evaluation.Trace = trace
			record.Evaluation.Outcome = crl.Result(trace["result"].(string))
			rehashTrace(t, record)
			rehashRecord(t, record)
			resignAllTestSignatures(t, record, keys)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record, trustPolicy, keys := trustedTestRecord(t)
			usePolicy := testUsePolicy(t, record)
			store := &countingReplayStore{}
			request := testUseRequest(record, trustPolicy, usePolicy, store)
			test.mutate(t, record, trustPolicy, keys, &request)
			if _, err := VerifyForUse(context.Background(), record, request); !errors.Is(err, test.want) {
				t.Fatalf("VerifyForUse error = %v, want %v", err, test.want)
			}
			if got := store.calls.Load(); got != 0 {
				t.Fatalf("replay store calls = %d, want 0", got)
			}
		})
	}
}

func TestVerifyForUseFailsClosedOnReplayStoreError(t *testing.T) {
	record, trustPolicy, _ := trustedTestRecord(t)
	usePolicy := testUsePolicy(t, record)
	request := testUseRequest(record, trustPolicy, usePolicy, replayStoreFunc(func(context.Context, ReplayClaim) error {
		return errors.New("database unavailable")
	}))
	if _, err := VerifyForUse(context.Background(), record, request); !errors.Is(err, ErrReplayStore) {
		t.Fatalf("VerifyForUse error = %v, want ErrReplayStore", err)
	}
	request.ReplayStore = nil
	if _, err := VerifyForUse(context.Background(), record, request); !errors.Is(err, ErrReplayStore) {
		t.Fatalf("VerifyForUse nil store error = %v, want ErrReplayStore", err)
	}
}

func TestMemoryReplayStoreIsAtomicAcrossBothIdentities(t *testing.T) {
	store := NewMemoryReplayStore()
	first := replayTestClaim("record-a", "correlation-a", "subject-a")
	if err := store.Consume(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(context.Background(), replayTestClaim("record-b", "correlation-a", "subject-b")); !errors.Is(err, ErrReplay) {
		t.Fatalf("correlation replay error = %v, want ErrReplay", err)
	}
	if err := store.Consume(context.Background(), replayTestClaim("record-b", "correlation-b", "subject-b")); err != nil {
		t.Fatalf("failed consume inserted a partial record key: %v", err)
	}
	if err := store.Consume(context.Background(), replayTestClaim("record-a", "correlation-c", "subject-c")); !errors.Is(err, ErrReplay) {
		t.Fatalf("record replay error = %v, want ErrReplay", err)
	}
}

func TestMemoryReplayStoreAllowsExactlyOneConcurrentConsumer(t *testing.T) {
	store := NewMemoryReplayStore()
	claim := replayTestClaim("record-a", "correlation-a", "subject-a")
	var successes atomic.Int64
	var replays atomic.Int64
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := store.Consume(context.Background(), claim)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrReplay):
				replays.Add(1)
			default:
				t.Errorf("Consume: %v", err)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || replays.Load() != 31 {
		t.Fatalf("successes=%d replays=%d, want 1 and 31", successes.Load(), replays.Load())
	}
}

func TestMemoryReplayStoreRejectsInvalidOrCancelledInput(t *testing.T) {
	var zero MemoryReplayStore
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := zero.Consume(ctx, replayTestClaim("record-a", "correlation-a", "subject-a")); !errors.Is(err, ErrReplayStore) {
		t.Fatalf("cancelled context error = %v, want ErrReplayStore", err)
	}
	if err := zero.Consume(context.Background(), ReplayClaim{}); !errors.Is(err, ErrReplayStore) {
		t.Fatalf("incomplete claim error = %v, want ErrReplayStore", err)
	}
	var nilStore *MemoryReplayStore
	if err := nilStore.Consume(context.Background(), replayTestClaim("record-a", "correlation-a", "subject-a")); !errors.Is(err, ErrReplayStore) {
		t.Fatalf("nil store error = %v, want ErrReplayStore", err)
	}
}

func testUsePolicy(t *testing.T, record *Record) *UsePolicy {
	t.Helper()
	policy := &UsePolicy{
		SchemaVersion:           "crl-decision-use-policy/v1",
		PolicyID:                "01989f7e-7b80-7000-8000-000000000020",
		Version:                 1,
		CreatedAt:               "2025-12-15T00:00:00Z",
		Domain:                  record.Context.Domain,
		ValidFrom:               "2026-01-01T00:00:00Z",
		ValidUntil:              "2027-01-01T00:00:00Z",
		MaxEvaluationAgeSeconds: 300,
		MaxRecordAgeSeconds:     120,
		MaxRecordDelaySeconds:   60,
		MaxClockSkewSeconds:     5,
		ReplayScope:             "record-and-correlation",
	}
	rehashUsePolicy(t, policy)
	return policy
}

func rehashUsePolicy(t *testing.T, policy *UsePolicy) {
	t.Helper()
	digest, err := domainDigest(usePolicyDomain, policy.unsigned())
	if err != nil {
		t.Fatal(err)
	}
	policy.PolicyHash = digest
}

func expectedRecordContext(record *Record) ExpectedContext {
	return ExpectedContext{
		Domain:        record.Context.Domain,
		Subject:       record.Context.Subject,
		CorrelationID: record.Context.CorrelationID,
	}
}

func testUseRequest(record *Record, trustPolicy *TrustPolicy, usePolicy *UsePolicy, store ReplayStore) UseRequest {
	return UseRequest{
		TrustPolicy:     trustPolicy,
		TrustPolicyHash: trustPolicy.PolicyHash,
		UsePolicy:       usePolicy,
		UsePolicyHash:   usePolicy.PolicyHash,
		Expected:        expectedRecordContext(record),
		VerifiedAt:      verifierTime(),
		ReplayStore:     store,
	}
}

func replayTestClaim(recordID, correlationID, subject string) ReplayClaim {
	return ReplayClaim{
		Domain:        "contrl.co/example",
		Subject:       subject,
		CorrelationID: correlationID,
		RecordID:      recordID,
		RecordHash:    fmt.Sprintf("hash-%s", recordID),
	}
}

type replayStoreFunc func(context.Context, ReplayClaim) error

func (function replayStoreFunc) Consume(ctx context.Context, claim ReplayClaim) error {
	return function(ctx, claim)
}

type countingReplayStore struct {
	calls atomic.Int64
}

func (store *countingReplayStore) Consume(context.Context, ReplayClaim) error {
	store.calls.Add(1)
	return nil
}
