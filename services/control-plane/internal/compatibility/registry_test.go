package compatibility

import "testing"

func TestGeneratedCompatibilityRecoveryOperationsAreClosedAndUnique(t *testing.T) {
	t.Parallel()
	operations := allGeneratedOperations()
	if len(operations) != 26 {
		t.Fatalf("generated operation count = %d", len(operations))
	}
	identities := []map[string]struct{}{{}, {}, {}, {}}
	for _, operation := range operations {
		if !operation.Valid() || operation.ProfileID() == "" || operation.ProfileDigest() == "" ||
			operation.OperationID() == "" || operation.SQLFunction() == "" ||
			operation.ServiceMethod() == "" || operation.Capability() == "" {
			t.Fatalf("invalid generated operation: %#v", operation)
		}
		values := []string{operation.OperationID(), operation.SQLFunction(), operation.ServiceMethod(), operation.Capability()}
		for index, value := range values {
			if _, exists := identities[index][value]; exists {
				t.Fatalf("duplicate generated identity %q", value)
			}
			identities[index][value] = struct{}{}
		}
		if operation.IsMutation() != (operation.Mode() == "mutation") ||
			operation.IsMutation() != (operation.UnknownOutcome() == "reconcile_required_no_write_retry") {
			t.Fatalf("generated operation mode/unknown policy drifted: %#v", operation)
		}
	}
	if (Operation{}).Valid() {
		t.Fatal("zero operation was valid")
	}
}

func TestGeneratedCompatibilityRecoveryBindingsAreExact(t *testing.T) {
	t.Parallel()
	if RegistryFormatVersion != "cloud-agents-compatibility-recovery-registry/v2" ||
		RegistryID != "cloud-agents/platform/compatibility-recovery" ||
		RegistryDigest != "sha256:d5ca128f28d637349dd6f8515f9ca6bb182fb0778a3160e24d731712589f2973" ||
		StateMachineDigest != "sha256:41ed340b8a1106341f8b797210492af0f9c022d8d43803977ff8079d52251863" ||
		PolicyDigest != "sha256:20f5b6e30e7d7254baabc97894aba2af2d2bcf40f4175f504d195b4e3a832708" ||
		SchemaHead != "000010" ||
		SchemaCatalogDigest != "sha256:a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236" ||
		SchemaMigrationDigest != "sha256:ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6" {
		t.Fatal("generated registry/schema binding drifted")
	}
	operation := AcquireBackfillLeaseOperation()
	if !operation.Valid() || operation.ProfileID() != "backfill/v2" ||
		operation.OperationID() != "backfill-acquire-lease/v2" ||
		operation.SQLFunction() != "cloud_agents.compatibility_recovery_backfill_acquire_lease_v2" ||
		operation.ServiceMethod() != "AcquireBackfillLease" ||
		operation.Capability() != "compatibility_recovery.backfill.acquire_lease" {
		t.Fatalf("representative generated operation drifted: %#v", operation)
	}
}
