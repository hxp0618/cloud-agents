package migration

import (
	"testing"
	"time"
)

func TestDecisionRecoveryArtifactProfileFormulaBindsEveryField(t *testing.T) {
	profile := fixedDecisionRecoveryArtifactProfile()
	if got := mustCanonicalDigest(t, profile); got != decisionRecoveryArtifactProfileDigest {
		t.Fatalf("fixed profile digest drifted: got=%s want=%s", got, decisionRecoveryArtifactProfileDigest)
	}
	if err := validateDecisionRecoveryArtifactProfileDigest(testDigest("wrong-profile")); !IsCode(err, CodeUntrusted) {
		t.Fatalf("profile mismatch was accepted: %v", err)
	}

	faults := map[string]func(*decisionRecoveryArtifactProfile){
		"domain":                func(value *decisionRecoveryArtifactProfile) { value.Domain += "-changed" },
		"format_version":        func(value *decisionRecoveryArtifactProfile) { value.FormatVersion += "-changed" },
		"canonicalization":      func(value *decisionRecoveryArtifactProfile) { value.Canonicalization = "changed" },
		"base64url":             func(value *decisionRecoveryArtifactProfile) { value.Base64URL = "changed" },
		"identity_max_bytes":    func(value *decisionRecoveryArtifactProfile) { value.IdentityMaxBytes++ },
		"encoded_field_max":     func(value *decisionRecoveryArtifactProfile) { value.EncodedFieldMaxBytes++ },
		"projection_inputs_max": func(value *decisionRecoveryArtifactProfile) { value.ProjectionInputsMax++ },
		"catalog_inputs_max":    func(value *decisionRecoveryArtifactProfile) { value.CatalogInputsMax++ },
		"kind_rank": func(value *decisionRecoveryArtifactProfile) {
			value.KindRank[0], value.KindRank[1] = value.KindRank[1], value.KindRank[0]
		},
		"max_size_bytes": func(value *decisionRecoveryArtifactProfile) { value.MaxSizeBytes++ },
	}
	for name, mutate := range faults {
		t.Run(name, func(t *testing.T) {
			mutated := fixedDecisionRecoveryArtifactProfile()
			mutate(&mutated)
			if got := mustCanonicalDigest(t, mutated); got == decisionRecoveryArtifactProfileDigest {
				t.Fatalf("%s mutation did not change the profile digest", name)
			}
		})
	}
}

func TestRecoveryPolicySubjectFormulaBindsEveryField(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	subject := validRecoveryPolicySignedSubject(now, []oldDecisionAuthorization{{
		OldRunnerProjectionDecisionDigest: testDigest("old-decision"),
		AllowExpired:                      true, AllowRevoked: true, AllowCompromised: true,
	}})
	baseline := mustCanonicalDigest(t, subject)
	faults := map[string]func(*recoveryPolicySignedSubject){
		"domain":                     func(value *recoveryPolicySignedSubject) { value.Domain += "-changed" },
		"issuer_key_identity_digest": func(value *recoveryPolicySignedSubject) { value.IssuerKeyIdentityDigest = testDigest("other-issuer") },
		"expires_at": func(value *recoveryPolicySignedSubject) {
			value.ExpiresAt = now.Add(3 * time.Hour).Format(time.RFC3339)
		},
		"security_epoch":             func(value *recoveryPolicySignedSubject) { value.SecurityEpoch++ },
		"minimum_old_security_epoch": func(value *recoveryPolicySignedSubject) { value.MinimumOldSecurityEpoch++ },
		"old_revocation_policy_digest": func(value *recoveryPolicySignedSubject) {
			value.OldRevocationPolicyDigest = testDigest("other-revocation-policy")
		},
		"old_decision_digest": func(value *recoveryPolicySignedSubject) {
			value.OldDecisionAuthorizations[0].OldRunnerProjectionDecisionDigest = testDigest("other-old-decision")
		},
		"allow_expired":     func(value *recoveryPolicySignedSubject) { value.OldDecisionAuthorizations[0].AllowExpired = false },
		"allow_revoked":     func(value *recoveryPolicySignedSubject) { value.OldDecisionAuthorizations[0].AllowRevoked = false },
		"allow_compromised": func(value *recoveryPolicySignedSubject) { value.OldDecisionAuthorizations[0].AllowCompromised = false },
	}
	for name, mutate := range faults {
		t.Run(name, func(t *testing.T) {
			mutated := cloneRecoveryPolicySignedSubject(subject)
			mutate(&mutated)
			if got := mustCanonicalDigest(t, mutated); got == baseline {
				t.Fatalf("%s mutation did not change the policy digest", name)
			}
		})
	}
}

func TestRecoveryPolicyRejectsExpiryEpochOrderDuplicateAndCurrentDecision(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	oldA, oldB := testDigest("old-a"), testDigest("old-b")
	if oldB < oldA {
		oldA, oldB = oldB, oldA
	}
	valid := validRecoveryPolicySignedSubject(now, []oldDecisionAuthorization{
		{OldRunnerProjectionDecisionDigest: oldA, AllowExpired: true},
		{OldRunnerProjectionDecisionDigest: oldB, AllowRevoked: true},
	})
	if _, err := bindVerifiedRecoveryPolicySubject(valid, 2, now); err != nil {
		t.Fatalf("valid recovery policy failed: %v", err)
	}

	for name, change := range map[string]func(*recoveryPolicySignedSubject, *uint64){
		"expired":               func(subject *recoveryPolicySignedSubject, _ *uint64) { subject.ExpiresAt = now.Format(time.RFC3339) },
		"below current minimum": func(subject *recoveryPolicySignedSubject, minimum *uint64) { *minimum = subject.SecurityEpoch + 1 },
		"zero current minimum":  func(_ *recoveryPolicySignedSubject, minimum *uint64) { *minimum = 0 },
		"zero policy epoch":     func(subject *recoveryPolicySignedSubject, _ *uint64) { subject.SecurityEpoch = 0 },
		"zero old minimum":      func(subject *recoveryPolicySignedSubject, _ *uint64) { subject.MinimumOldSecurityEpoch = 0 },
		"unsorted": func(subject *recoveryPolicySignedSubject, _ *uint64) {
			subject.OldDecisionAuthorizations[0], subject.OldDecisionAuthorizations[1] = subject.OldDecisionAuthorizations[1], subject.OldDecisionAuthorizations[0]
		},
		"duplicate": func(subject *recoveryPolicySignedSubject, _ *uint64) {
			subject.OldDecisionAuthorizations[1].OldRunnerProjectionDecisionDigest = subject.OldDecisionAuthorizations[0].OldRunnerProjectionDecisionDigest
		},
	} {
		t.Run(name, func(t *testing.T) {
			subject := cloneRecoveryPolicySignedSubject(valid)
			minimum := uint64(2)
			change(&subject, &minimum)
			if _, err := bindVerifiedRecoveryPolicySubject(subject, minimum, now); !IsCode(err, CodeUntrusted) {
				t.Fatalf("invalid recovery policy was accepted: %v", err)
			}
		})
	}

	policy, err := bindVerifiedRecoveryPolicySubject(validRecoveryPolicySignedSubject(now, []oldDecisionAuthorization{{
		OldRunnerProjectionDecisionDigest: testDigest("current-combined"),
	}}), 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectCurrentRecoveryAuthorization(policy, testDigest("current-combined")); !IsCode(err, CodeUntrusted) {
		t.Fatalf("current combined decision self-reference was accepted: %v", err)
	}
}

func TestRecoveryPolicyBindingRejectsAliasWrapperSwapProfileFaultAndChangesCombinedDigest(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	callerAuthorizations := []oldDecisionAuthorization{{OldRunnerProjectionDecisionDigest: testDigest("old-decision"), AllowExpired: true}}
	callerSubject := validRecoveryPolicySignedSubject(fixture.now, callerAuthorizations)
	policy, err := bindVerifiedRecoveryPolicySubject(callerSubject, 1, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	fixture.recoveryPolicy = policy
	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, fixture.catalogs, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}

	callerSubject.OldDecisionAuthorizations[0].OldRunnerProjectionDecisionDigest = testDigest("caller-alias-mutation")
	if _, err := bound.runnerProjectionBindings(); err != nil {
		t.Fatalf("caller authorization alias reached the bound policy: %v", err)
	}

	mutated := bindings.ownedCopy()
	mutated.verifiedRecoveryPolicy.subject.OldDecisionAuthorizations[0].AllowRevoked = true
	if err := mutated.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("deep policy alias mutation was accepted: %v", err)
	}
	mutated = bindings.ownedCopy()
	mutated.verifiedRecoveryPolicy.expiresAt = fixture.now.Add(-time.Second)
	if err := mutated.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("policy expiry mutation was accepted: %v", err)
	}
	mutated = bindings.ownedCopy()
	mutated.verifiedRecoveryPolicy.securityEpoch++
	if err := mutated.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("policy epoch mutation was accepted: %v", err)
	}
	mutated = bindings.ownedCopy()
	mutated.decisionRecoveryArtifactProfileDigest = testDigest("wrong-profile")
	if err := mutated.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("profile mismatch was accepted: %v", err)
	}

	alternatePolicy, err := bindVerifiedRecoveryPolicySubject(validRecoveryPolicySignedSubject(fixture.now, []oldDecisionAuthorization{{
		OldRunnerProjectionDecisionDigest: testDigest("other-old-decision"), AllowCompromised: true,
	}}), 1, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	mutated = bindings.ownedCopy()
	mutated.verifiedRecoveryPolicy = alternatePolicy
	if err := mutated.validateAt(fixture.now); !IsCode(err, CodeUntrusted) {
		t.Fatalf("another individually valid recovery wrapper was accepted: %v", err)
	}

	alternateBound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, alternatePolicy, fixture.initialScope, fixture.catalogs, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	alternateBindings, err := alternateBound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	if bindings.runnerProjectionDecisionDigest == alternateBindings.runnerProjectionDecisionDigest {
		t.Fatal("recovery policy change did not change the combined decision digest")
	}
	if bindings.releaseTrustDecisionDigest != alternateBindings.releaseTrustDecisionDigest ||
		bindings.authorityProfileDigest != alternateBindings.authorityProfileDigest ||
		bindings.authorityBindingDigest != alternateBindings.authorityBindingDigest {
		t.Fatal("recovery policy comparison changed release or authority digests")
	}
	if !runnerCanonicalEqual(catalogDecisionSubjects(bindings.executableCatalogs), catalogDecisionSubjects(alternateBindings.executableCatalogs)) {
		t.Fatal("recovery policy comparison changed catalog decision digests")
	}
	if bindings.executionLineageDigest != alternateBindings.executionLineageDigest {
		t.Fatal("recovery policy change altered the stable execution lineage")
	}
}

func validRecoveryPolicySignedSubject(now time.Time, authorizations []oldDecisionAuthorization) recoveryPolicySignedSubject {
	return recoveryPolicySignedSubject{
		Domain: recoveryPolicySubjectDomain, IssuerKeyIdentityDigest: testDigest("recovery-policy-issuer"),
		ExpiresAt: now.Add(2 * time.Hour).Format(time.RFC3339), SecurityEpoch: 2, MinimumOldSecurityEpoch: 1,
		OldRevocationPolicyDigest: testDigest("old-revocation-policy"),
		OldDecisionAuthorizations: append([]oldDecisionAuthorization{}, authorizations...),
	}
}
