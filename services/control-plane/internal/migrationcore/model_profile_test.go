package migrationcore

import "testing"

func TestExecutionPolicyClosesCatalogAuthorityProfilePairs(t *testing.T) {
	policy := func(catalogProfile, authorityPath string) ExecutionPolicy {
		return ExecutionPolicy{
			StatementProfile: "postgresql-ddl-v1",
			CatalogProfile:   catalogProfile,
			AuthorityContract: ArtifactRecord{
				Path: authorityPath, Mode: "100644", SizeBytes: 1, SHA256: DigestBytes([]byte("x")),
			},
			IsolationLevel:                    "serializable",
			AccessMode:                        "read_write",
			PostgresMajorMin:                  15,
			PostgresMajorMax:                  17,
			StatementTimeoutMS:                300000,
			LockTimeoutMS:                     30000,
			IdleInTransactionSessionTimeoutMS: 60000,
			MaxAttempts:                       3,
			LineageQuotaProfile:               LineageQuotaProfileV4,
		}
	}
	for _, pair := range []struct {
		catalog   string
		authority string
	}{
		{CatalogProfileV1, AuthorityContractV1Path},
		{CatalogProfileV2, AuthorityContractV2Path},
	} {
		if err := policy(pair.catalog, pair.authority).Validate(); err != nil {
			t.Fatalf("valid profile pair %s / %s: %v", pair.catalog, pair.authority, err)
		}
	}
	for _, pair := range []struct {
		catalog   string
		authority string
	}{
		{CatalogProfileV1, AuthorityContractV2Path},
		{CatalogProfileV2, AuthorityContractV1Path},
	} {
		if err := policy(pair.catalog, pair.authority).Validate(); err == nil {
			t.Fatalf("mismatched profile pair accepted: %s / %s", pair.catalog, pair.authority)
		}
	}
}
