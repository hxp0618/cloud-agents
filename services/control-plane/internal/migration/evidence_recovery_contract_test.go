package migration

import (
	"reflect"
	"testing"
)

type recoveryPolicyFixtureInput struct {
	IssuerKeyIdentityDigest   Digest                     `json:"issuer_key_identity_digest"`
	ExpiresAt                 string                     `json:"expires_at"`
	SecurityEpoch             uint64                     `json:"security_epoch"`
	MinimumOldSecurityEpoch   uint64                     `json:"minimum_old_security_epoch"`
	OldRevocationPolicyDigest Digest                     `json:"old_revocation_policy_digest"`
	OldDecisionAuthorizations []oldDecisionAuthorization `json:"old_decision_authorizations"`
}

func TestRecoveryPolicyChainPrivateSubjectsSameBits(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/recovery-policy-chain-v1.json"))
	currentDecision := fixtureDigest(t, fixture, "current_decision")

	policyVector := fixtureObjectValue(t, fixture["current_signed_policy_subject"], "current policy vector")
	var policyInput recoveryPolicyFixtureInput
	policyCanonical, policyDigest := decodeSameBitsInput(t, policyVector, &policyInput)
	policySubject := recoveryPolicySignedSubject{
		Domain: recoveryPolicySubjectDomain, IssuerKeyIdentityDigest: policyInput.IssuerKeyIdentityDigest,
		ExpiresAt: policyInput.ExpiresAt, SecurityEpoch: policyInput.SecurityEpoch,
		MinimumOldSecurityEpoch:   policyInput.MinimumOldSecurityEpoch,
		OldRevocationPolicyDigest: policyInput.OldRevocationPolicyDigest,
		OldDecisionAuthorizations: policyInput.OldDecisionAuthorizations,
	}
	gotPolicyDigest, err := recoveryPolicySubjectDigestFromSigned(policySubject)
	if err != nil || gotPolicyDigest != policyDigest {
		t.Fatalf("current policy same-bits: %v / %s != %s", err, gotPolicyDigest, policyDigest)
	}
	if policyCanonical == "" {
		t.Fatal("empty current policy canonical body")
	}

	transitionValues, ok := fixture["transitions"].([]JSONValue)
	if !ok || len(transitionValues) != 2 {
		t.Fatal("recovery fixture is not A to B to C")
	}
	transitions := make([]ownedHistoricalTransition, 0, len(transitionValues))
	for index, rawTransition := range transitionValues {
		transition := fixtureObjectValue(t, rawTransition, "transition")
		old := fixtureDigest(t, transition, "old_decision")
		successor := fixtureDigest(t, transition, "successor_decision")

		var historical historicalRecoveryPolicySubject
		_, historicalDigest := decodeSameBitsInput(t, fixtureObjectValue(t, transition["historical_policy"], "historical vector"), &historical)
		if historical.RecoveryPolicySubjectDigest != policyDigest || historical.OldRunnerProjectionDecisionDigest != old || historical.SuccessorRunnerProjectionDecisionDigest != successor {
			t.Fatalf("transition %d historical identity", index)
		}

		var execution recoveryExecutionBindingsSubject
		_, _ = decodeSameBitsInput(t, fixtureObjectValue(t, transition["recovery_execution_bindings"], "execution vector"), &execution)
		var authority lineageSupersessionAuthoritySubject
		_, authorityDigest := decodeSameBitsInput(t, fixtureObjectValue(t, transition["supersession_authority"], "authority vector"), &authority)
		if err := validateRecoveryAuthorityBindings(currentDecision, historical, execution, authority); err != nil {
			t.Fatalf("transition %d total cross-bind: %v", index, err)
		}

		var planned GenerationReserved
		plannedCanonical := decodeFixtureValue(t, transition["planned_generation_reserved"], &planned)
		if err := planned.Validate(); err != nil {
			t.Fatalf("transition %d planned reservation: %v", index, err)
		}
		plannedDigest := fixtureDigest(t, transition, "planned_generation_reserved_digest")
		if DigestBytes(plannedCanonical) != plannedDigest || planned.RunnerProjectionDecisionDigest != successor || planned.SchemaBundleDigest != authority.SuccessorSchemaBundleDigest || !canonicalEqual(planned.Continuation, authority.Continuation) {
			t.Fatalf("transition %d planned reservation binding", index)
		}
		if got, err := authority.ComputeDigest(); err != nil || got != authorityDigest {
			t.Fatalf("transition %d authority digest: %v / %s", index, err, got)
		}
		if got, err := historical.ComputeDigest(); err != nil || got != historicalDigest {
			t.Fatalf("transition %d historical digest: %v / %s", index, err, got)
		}
		transitions = append(transitions, ownedHistoricalTransition{old, successor, historical, execution, authority, planned, plannedDigest})
	}
	receiptValues, ok := fixture["durable_artifact_receipts"].([]JSONValue)
	if !ok {
		t.Fatal("content receipts")
	}
	receipts := make([]ownedHistoricalContentReceipt, len(receiptValues))
	for index, raw := range receiptValues {
		object := fixtureObjectValue(t, raw, "content receipt")
		receipts[index] = ownedHistoricalContentReceipt{
			decision: fixtureDigest(t, object, "decision"), runtimeSHA256: fixtureDigest(t, object, "runtime_sha256"),
			runtimeSizeBytes: object["runtime_size_bytes"].(uint64), recoverySHA256: fixtureDigest(t, object, "recovery_sha256"), recoverySizeBytes: object["recovery_size_bytes"].(uint64),
		}
	}
	verified, err := bindHistoricalRecoveryChain(currentDecision, policySubject, receipts, transitions)
	if err != nil || verified.currentDecision != currentDecision || len(verified.authorities) != 2 {
		t.Fatalf("verified historical chain: %v", err)
	}
}

func TestDecisionRecoveryInputsArePrivateContentABI(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/decision-recovery-inputs-v1.json"))
	var input decisionRecoveryVerificationInputs
	decodeFixtureValue(t, fixture["same_bits_input"], &input)
	if err := input.validate(); err != nil {
		t.Fatal(err)
	}

	nonNFC := input
	nonNFC.RepositoryIdentity = "repo-e\u0301"
	if err := nonNFC.validate(); err == nil {
		t.Fatal("accepted non-NFC recovery identity")
	}
	overCount := input
	overCount.ProjectionSubjectInputs = make([]decisionRecoveryProjectionSubjectInput, 4100)
	if err := overCount.validate(); err == nil {
		t.Fatal("accepted projection input count above 4099")
	}

	var frame EvidenceFrame
	raw := decodeFixtureValue(t, fixture["same_bits_input"], &input)
	if _, err := DecodeStrict(raw, &frame); err == nil {
		t.Fatal("decision recovery content was accepted as EvidenceFrame")
	}
}

func TestCurrentRecoveryPolicyExpiryIsCanonicalRFC3339UTC(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/recovery-policy-chain-v1.json"))
	vector := fixtureObjectValue(t, fixture["current_signed_policy_subject"], "policy vector")
	var input recoveryPolicyFixtureInput
	decodeSameBitsInput(t, vector, &input)
	subject := recoveryPolicySignedSubject{
		Domain: recoveryPolicySubjectDomain, IssuerKeyIdentityDigest: input.IssuerKeyIdentityDigest,
		ExpiresAt: input.ExpiresAt, SecurityEpoch: input.SecurityEpoch, MinimumOldSecurityEpoch: input.MinimumOldSecurityEpoch,
		OldRevocationPolicyDigest: input.OldRevocationPolicyDigest, OldDecisionAuthorizations: input.OldDecisionAuthorizations,
	}
	for _, invalid := range []string{"2026-08-12T00:00:00+00:00", "2026-08-12t00:00:00Z", "2026-08-12T00:00:00.000Z"} {
		subject.ExpiresAt = invalid
		if _, err := recoveryPolicySubjectDigestFromSigned(subject); err == nil {
			t.Fatalf("accepted non-canonical expiry %q", invalid)
		}
	}
}

func decodeSameBitsInput(t *testing.T, vector map[string]JSONValue, target any) (string, Digest) {
	t.Helper()
	if !reflect.DeepEqual(sortedJSONKeys(vector), []string{"canonical_rfc8785_utf8", "digest", "input"}) {
		t.Fatal("same-bits vector shape")
	}
	canonical := decodeFixtureValue(t, vector["input"], target)
	claimedCanonical, ok := vector["canonical_rfc8785_utf8"].(string)
	if !ok || claimedCanonical != string(canonical) {
		t.Fatal("same-bits canonical bytes")
	}
	claimed := fixtureDigest(t, vector, "digest")
	return claimedCanonical, claimed
}
