package authn

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"testing"
)

func TestGeneratedIdentityVerifierProfileExactFacts(t *testing.T) {
	profile := generatedIdentityVerifierProfile()
	if !profile.valid() {
		t.Fatal("generated identity verifier profile is invalid")
	}
	if profile.formatVersion != "cloud-agents-platform-identity-verifier-registry/v1" ||
		profile.registryID != "cloud-agents/platform/identity-verifier" ||
		profile.profileID != "platform-identity-verifier/v1" {
		t.Fatalf("generated identity changed: %+v", profile)
	}
	digestPattern := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for _, digest := range []string{
		profile.registryDigest,
		profile.profileDigest,
		identityVerifierRegistryDigest,
		identityVerifierProfileDigest,
	} {
		if !digestPattern.MatchString(digest) {
			t.Fatalf("generated digest is invalid: %q", digest)
		}
	}
	if profile.registryDigest != identityVerifierRegistryDigest ||
		profile.profileDigest != identityVerifierProfileDigest {
		t.Fatal("generated digest constants and profile facts diverged")
	}

	assertArray(t, "accepted token types", profile.token.acceptedTypes, [2]string{"application/at+jwt", "at+jwt"})
	assertArray(t, "algorithms", profile.algorithm.accepted, [1]string{"RS256"})
	assertArray(t, "protected members", profile.protectedHeader.requiredMembers, [3]string{"alg", "kid", "typ"})
	assertArray(t, "JWK key operations", profile.jwk.keyOps, [1]string{"verify"})
	assertArray(t, "required custom claims", profile.claims.requiredCustom, [4]string{
		"https://schemas.cloud-agents.dev/claims/security-epoch",
		"https://schemas.cloud-agents.dev/claims/subject-kind",
		"https://schemas.cloud-agents.dev/claims/tenant-id",
		"https://schemas.cloud-agents.dev/claims/token-profile",
	})
	assertArray(t, "optional custom claims", profile.claims.optionalCustom, [1]string{
		"https://schemas.cloud-agents.dev/claims/project-id",
	})
	assertArray(t, "stable error categories", profile.errorRules.categories, [15]string{
		"audience_mismatch",
		"epoch_mismatch",
		"internal_failure",
		"invalid_signature",
		"issuer_mismatch",
		"malformed",
		"project_mismatch",
		"revoked_key",
		"revoked_token",
		"scope_mismatch",
		"tenant_mismatch",
		"time_invalid",
		"unknown_key",
		"unsupported_algorithm",
		"unsupported_profile",
	})
	assertArray(t, "redacted facts", profile.errorRules.redactedFacts, [4]string{
		"jwk_private_material",
		"secret_bearing_request_headers",
		"signature_bytes",
		"token_bytes",
	})
	assertArray(t, "redaction surfaces", profile.errorRules.redactionSurfaces, [4]string{
		"errors",
		"fixtures",
		"logs",
		"review_records",
	})
	assertArray(t, "subject kinds", profile.claims.subjectKinds, [3]string{"serviceAccount", "user", "workload"})
	assertArray(t, "token time checks", profile.timeRules.tokenChecks, [6]string{
		"exp-iat<=3600",
		"iat<=nowSecond+60",
		"iat<exp",
		"nbf<=nowSecond+60_when_present",
		"nbf<exp_when_present",
		"nowSecond<exp+60",
	})

	strings := map[string]string{
		"token serialization":        profile.token.serialization,
		"canonical token type":       profile.token.canonicalType,
		"algorithm implementation":   profile.algorithm.implementation,
		"JWK type":                   profile.jwk.kty,
		"JWK algorithm":              profile.jwk.alg,
		"JWK exponent":               profile.jwk.exponentBase64urlUInt,
		"subject kind claim":         profile.claims.subjectKindClaim,
		"tenant claim":               profile.claims.tenantIDClaim,
		"project claim":              profile.claims.projectIDClaim,
		"security epoch claim":       profile.claims.securityEpochClaim,
		"token profile claim":        profile.claims.tokenProfileClaim,
		"token profile value":        profile.claims.tokenProfileValue,
		"scope pattern":              profile.lexicalRules.scopeItemPattern,
		"subject lexical rule":       profile.lexicalRules.subject,
		"digest framing":             profile.digestRules.framing,
		"profile digest domain":      profile.digestRules.domains.profile,
		"registry digest domain":     profile.digestRules.domains.registry,
		"snapshot digest domain":     profile.digestRules.domains.trustSnapshot,
		"token input digest domain":  profile.digestRules.domains.tokenInput,
		"principal digest domain":    profile.digestRules.domains.verifiedPrincipal,
		"permanent kid binding":      profile.keyLineage.kidBinding,
		"snapshot authority":         profile.trustSnapshot.authority,
		"context authority":          profile.verificationContext.authority,
		"audience authority":         profile.verificationContext.audienceAuthority,
		"principal consumption":      profile.verifiedPrincipal.consumption,
		"principal lease scope":      profile.verifiedPrincipal.leaseScope,
		"duplicate member parsing":   profile.parsingRules.duplicateDecodedMembers,
		"JSON encoding parsing":      profile.parsingRules.jsonEncoding,
		"top-level parsing":          profile.parsingRules.topLevel,
		"numeric-date parsing":       profile.parsingRules.numericDates,
		"bounded parsing":            profile.parsingRules.sizeAndDepthAdmission,
		"base64url parsing":          profile.parsingRules.base64url,
		"claims object parsing":      profile.parsingRules.claimsObject,
		"issuer binding":             profile.bindingRules.issuer,
		"key binding":                profile.bindingRules.key,
		"audience binding":           profile.bindingRules.audience,
		"token type binding":         profile.bindingRules.tokenType,
		"time binding":               profile.bindingRules.time,
		"revocation binding":         profile.bindingRules.revocation,
		"security epoch binding":     profile.bindingRules.securityEpoch,
		"subject binding":            profile.bindingRules.subject,
		"tenant binding":             profile.bindingRules.tenant,
		"project binding":            profile.bindingRules.project,
		"permission binding":         profile.bindingRules.permission,
		"profile binding":            profile.bindingRules.profile,
		"inference binding":          profile.bindingRules.inference,
		"error stability":            profile.errorRules.stability,
		"HTTP boundary":              profile.implementationNonClaims.httpSurface,
		"provider boundary":          profile.implementationNonClaims.providerSideEffects,
		"production database writes": profile.implementationNonClaims.productionDatabaseWrites,
		"gate boundary":              profile.implementationNonClaims.gateStatus,
	}
	wantStrings := map[string]string{
		"token serialization":        "compact_jws",
		"canonical token type":       "at+jwt",
		"algorithm implementation":   "go_standard_library_crypto_rsa_pkcs1v15_sha256",
		"JWK type":                   "RSA",
		"JWK algorithm":              "RS256",
		"JWK exponent":               "AQAB",
		"subject kind claim":         "https://schemas.cloud-agents.dev/claims/subject-kind",
		"tenant claim":               "https://schemas.cloud-agents.dev/claims/tenant-id",
		"project claim":              "https://schemas.cloud-agents.dev/claims/project-id",
		"security epoch claim":       "https://schemas.cloud-agents.dev/claims/security-epoch",
		"token profile claim":        "https://schemas.cloud-agents.dev/claims/token-profile",
		"token profile value":        "cloud-agents-access-token/v1",
		"scope pattern":              `^[a-z][a-z0-9-]*\.(create|get|list|watch|update|delete|act|bind)$`,
		"subject lexical rule":       "non_empty_valid_utf8_exact_decoded_unicode_scalar_sequence_1..256_deliberately_c0_del_allowed",
		"digest framing":             "UTF8(domain)||0x00||payload",
		"profile digest domain":      "cloud-agents/platform-identity-verifier/profile/v1",
		"registry digest domain":     "cloud-agents/platform-identity-verifier/registry/v1",
		"snapshot digest domain":     "cloud-agents/platform-identity-verifier/trust-snapshot/v1",
		"token input digest domain":  "cloud-agents/platform-identity-verifier/token-input/v1",
		"principal digest domain":    "cloud-agents/platform-identity-verifier/verified-principal/v1",
		"permanent kid binding":      "permanent_one_issuer_profile_kid_to_one_rsa_public_key",
		"snapshot authority":         "package_private_owned_immutable_generation",
		"context authority":          "package_private_trusted_resource_server_context",
		"audience authority":         "snapshot_owned_single_resource_audience",
		"principal consumption":      "atomic_consume_reacquire_exact_generation_lease_and_revalidate",
		"principal lease scope":      "through_authorization_typed_operation_and_transaction_settlement",
		"duplicate member parsing":   "reject_in_every_object_including_json_escape_equivalent_names",
		"JSON encoding parsing":      "valid_utf8_only",
		"top-level parsing":          "one_complete_json_object_no_trailing_input",
		"numeric-date parsing":       "base10_json_integer_only_no_fraction_or_exponent",
		"bounded parsing":            "declared_token_segment_decoded_size_and_json_depth_bounds_before_unbounded_allocation",
		"base64url parsing":          "unpadded_canonical_base64url_ascii_admitted_before_decode_and_token_input_digest",
		"claims object parsing":      "complete_parse_unknown_signed_claims_ignored_as_ordinary_data",
		"issuer binding":             "token_iss_exactly_equals_snapshot_issuer_decoded_unicode_scalar_sequence",
		"key binding":                "selected_key_belongs_to_exact_snapshot_profile_issuer_epoch_algorithm_and_issuance_interval_and_is_enabled_and_not_revoked",
		"audience binding":           "token_aud_is_one_string_exactly_equal_to_snapshot_owned_resource_audience",
		"token type binding":         "protected_typ_is_admitted_access_token_type_and_canonicalizes_to_at+jwt",
		"time binding":               "all_generated_half_open_token_key_and_snapshot_time_inequalities_hold_for_one_owned_clock_instant",
		"revocation binding":         "kid_and_jti_absent_from_active_generation_revocation_sets",
		"security epoch binding":     "token_security_epoch_exactly_equals_active_snapshot_epoch",
		"subject binding":            "subject_kind_is_serviceAccount_user_or_workload_and_issuer_and_sub_are_exact_decoded_values",
		"tenant binding":             "token_tenant_exactly_equals_context_owned_target_tenant",
		"project binding":            "project_bound_token_requires_exact_project_target_and_unbound_token_may_reach_narrower_project_only_subject_to_scope_and_rbac",
		"permission binding":         "context_owned_required_permission_is_present_in_token_scope_set",
		"profile binding":            "token_profile_value_generated_profile_digest_and_generated_registry_digest_are_exact",
		"inference binding":          "audience_tenant_project_permission_and_scope_comparisons_infer_no_hierarchy_or_authorization",
		"error stability":            "exact_generated_categories_no_raw_authority_material",
		"HTTP boundary":              "not_implemented",
		"provider boundary":          "forbidden",
		"production database writes": "not_authorized",
		"gate boundary":              "all_gates_open",
	}
	if !reflect.DeepEqual(strings, wantStrings) {
		t.Fatalf("generated exact string facts changed:\n got: %#v\nwant: %#v", strings, wantStrings)
	}

	limits := profile.limits
	if profile.token.segmentCount != 3 || jwkExponent(profile) != 65537 ||
		limits.compactTokenBytes != 16384 || limits.decodedProtectedHeaderBytes != 1024 ||
		limits.decodedClaimsBytes != 12288 || limits.jsonDepth != 4 ||
		limits.trustSnapshotBytes != 262144 || limits.lifetimeKeyLineageRecords != 32 ||
		limits.audiences != 1 || limits.scopes != 64 || limits.revokedTokenIDs != 4096 ||
		limits.rsaModulusBitsMin != 2048 || limits.rsaModulusBitsMax != 4096 ||
		limits.tokenLifetimeSeconds != 3600 || limits.clockSkewSeconds != 60 ||
		limits.trustSnapshotValiditySeconds != 86400 ||
		profile.keyLineage.generationStart != 1 || profile.keyLineage.generationStep != 1 {
		t.Fatalf("generated exact numeric facts changed: %+v", profile)
	}
}

func TestIdentityVerifierProfileWholeValueEqualityAndTamperRejection(t *testing.T) {
	profile := generatedIdentityVerifierProfile()
	ordinaryCopy := profile
	if ordinaryCopy != profile || !ordinaryCopy.valid() {
		t.Fatal("ordinary whole-value copy lost the generated identity")
	}
	if (identityVerifierProfile{}).valid() {
		t.Fatal("zero profile was accepted")
	}

	mutations := []func(*identityVerifierProfile){
		func(value *identityVerifierProfile) { value.registryDigest = identityVerifierProfileDigest },
		func(value *identityVerifierProfile) { value.profileID = "caller-selected/v1" },
		func(value *identityVerifierProfile) { value.token.acceptedTypes[0] = "JWT" },
		func(value *identityVerifierProfile) { value.algorithm.accepted[0] = "HS256" },
		func(value *identityVerifierProfile) { value.jwk.exponentDecimal++ },
		func(value *identityVerifierProfile) { value.claims.tenantIDClaim += "/caller" },
		func(value *identityVerifierProfile) { value.limits.audiences++ },
		func(value *identityVerifierProfile) { value.timeRules.tokenChecks[0] = "caller-selected" },
		func(value *identityVerifierProfile) { value.parsingRules.jsonEncoding = "caller-selected" },
		func(value *identityVerifierProfile) { value.bindingRules.audience = "caller-selected" },
		func(value *identityVerifierProfile) { value.errorRules.categories[0] = "caller-selected" },
		func(value *identityVerifierProfile) { value.digestRules.domains.profile += "/caller" },
		func(value *identityVerifierProfile) { value.keyLineage.generationStep++ },
		func(value *identityVerifierProfile) { value.trustSnapshot.requiredFacts[0] = "caller-selected" },
		func(value *identityVerifierProfile) { value.verificationContext.productionConstructor = "present" },
		func(value *identityVerifierProfile) { value.verifiedPrincipal.boundFacts[0] = "caller-selected" },
		func(value *identityVerifierProfile) { value.implementationNonClaims.gateStatus = "closed" },
	}
	for index, mutate := range mutations {
		candidate := profile
		mutate(&candidate)
		if candidate == profile || candidate.valid() {
			t.Fatalf("tampered profile %d was accepted", index)
		}
	}

	mutatedFormerBaseline := generatedIdentityVerifierProfile()
	mutatedFormerBaseline.profileID = "forged-baseline/v1"
	mutatedFormerBaseline.claims.requiredCustom[0] = "forged-claim"
	fresh := generatedIdentityVerifierProfile()
	if mutatedFormerBaseline.valid() || !fresh.valid() || fresh.profileID != "platform-identity-verifier/v1" {
		t.Fatal("local baseline mutation changed generated authority")
	}
}

func TestIdentityVerifierProfileHasNoExternalSurface(t *testing.T) {
	for _, path := range []string{"profile.go", "profile_generated.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(file.Imports) != 0 {
			t.Fatalf("%s imports runtime or external dependencies", path)
		}
		for _, declaration := range file.Decls {
			switch item := declaration.(type) {
			case *ast.FuncDecl:
				isProfileValidator := path == "profile.go" && item.Name.Name == "valid" && item.Recv != nil
				isFreshGeneratedLiteral := path == "profile_generated.go" && item.Name.Name == "generatedIdentityVerifierProfile" && item.Recv == nil
				if !isProfileValidator && !isFreshGeneratedLiteral {
					t.Fatalf("%s defines unexpected function %q", path, item.Name.Name)
				}
				if isFreshGeneratedLiteral {
					assertFreshGeneratedProfileLiteral(t, item)
				}
				if ast.IsExported(item.Name.Name) {
					t.Fatalf("%s exports function %q", path, item.Name.Name)
				}
			case *ast.GenDecl:
				if path == "profile_generated.go" && item.Tok != token.CONST {
					t.Fatalf("generated profile contains mutable or non-constant declaration %s", item.Tok)
				}
				for _, spec := range item.Specs {
					switch value := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(value.Name.Name) {
							t.Fatalf("%s exports type %q", path, value.Name.Name)
						}
					case *ast.ValueSpec:
						for _, name := range value.Names {
							if ast.IsExported(name.Name) {
								t.Fatalf("%s exports value %q", path, name.Name)
							}
						}
					}
				}
			}
		}
	}
}

func assertArray[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s changed: got %#v want %#v", name, got, want)
	}
}

func jwkExponent(profile identityVerifierProfile) uint32 {
	return profile.jwk.exponentDecimal
}

func assertFreshGeneratedProfileLiteral(t *testing.T, declaration *ast.FuncDecl) {
	t.Helper()
	if declaration.Body == nil || len(declaration.Body.List) != 1 {
		t.Fatal("generated profile function must contain one fresh literal return")
	}
	statement, ok := declaration.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(statement.Results) != 1 {
		t.Fatal("generated profile function does not directly return one value")
	}
	if _, ok := statement.Results[0].(*ast.CompositeLit); !ok {
		t.Fatal("generated profile function does not return a fresh composite literal")
	}
}

var _ = map[identityVerifierProfile]struct{}{}
