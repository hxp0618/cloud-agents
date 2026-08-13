package migration

import (
	"reflect"
	"testing"
)

func TestHistoricalVerificationFactsRebuildExactOrdinarySubjects(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	bundle, catalog := exactPlanBundle(t, fixture.decision.expectedSchemaBundleDigest, fixture.initialScope.BoundPrecondition(), fixture.authorityProfile)
	bundle.Manifest.ExecutionPolicy.MaxAttempts = 3
	var err error
	bundle.ownedInputs, err = bindVerifiedRuntimeBundleInputs(bundle.Manifest, bundle.Files, bundle.ownedInputs.outerArtifactDigest, bundle.ownedInputs.outerArtifactSize)
	if err != nil {
		t.Fatal(err)
	}
	catalogRaw := mustJSON(t, catalog)
	catalogSubject, err := bindVerifiedExecutableCatalogSubject(catalogRaw, DigestBytes(catalogRaw), fixture.expiresAt, 1, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, []verifiedExecutableCatalogSubject{catalogSubject}, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	facts, err := buildHistoricalVerificationFacts(bundle, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if facts.maxAttempts != uint32(bundle.ownedInputs.manifest.ExecutionPolicy.MaxAttempts) || !reflect.DeepEqual(facts.orderedMigrations, []string{"000001"}) || len(facts.statementSubjects["000001"]) != 1 || len(facts.ledgerRows) != 1 || facts.finalCatalogDigest["000001"] == ([32]byte{}) {
		t.Fatalf("historical facts are incomplete: %+v", facts)
	}
	plans, err := buildExactStatementPlans(bundle, bindings, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	want, err := admissionStatementPlanSubjectFromPlan(plans[0])
	if err != nil || facts.statementSubjects["000001"][0] != want {
		t.Fatalf("statement subject differs: got=%x want=%x err=%v", facts.statementSubjects["000001"][0], want, err)
	}
	entry := bundle.ownedInputs.manifest.SchemaBundle.Migrations[0]
	if !canonicalEqual(facts.ledgerRows[0], commitIntentLedgerRow(entry, bundle.ownedInputs.manifest.SchemaBundleDigest)) {
		t.Fatal("historical ledger row differs from immutable manifest")
	}

	// The historical path validates at the shared pre-expiry point, while the
	// live execution path rejects the same binding after expiry.
	if _, err := buildExactStatementPlans(bundle, bindings, fixture.expiresAt); !IsCode(err, CodeUntrusted) {
		t.Fatalf("expired live plan was accepted: %v", err)
	}
	if _, err := buildHistoricalVerificationFacts(bundle, bindings); err != nil {
		t.Fatalf("immutable historical facts were rejected after elapsed clock: %v", err)
	}

	mutated := bindings.ownedCopy()
	mutated.schemaBundleDigest = testDigest("historical-facts-schema-swap")
	if _, err := buildHistoricalVerificationFacts(bundle, mutated); !IsCode(err, CodeUntrusted) {
		t.Fatalf("mutated historical binding was accepted: %v", err)
	}
}

func TestHistoricalVerificationFactsContainNoExecutablePlan(t *testing.T) {
	typeOf := reflect.TypeOf(admissionHistoricalVerificationFacts{})
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		if field.Type == reflect.TypeOf(StatementPlan{}) || field.Type == reflect.TypeOf([]StatementPlan{}) || field.Name == "sqlBytes" {
			t.Fatalf("ordinary historical facts expose executable plan field %s", field.Name)
		}
	}
}
