package identity_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
)

func TestAutomationStoreUsesControlPlaneAuthorizationWithoutCredentialDisclosure(t *testing.T) {
	pool := tokenTestPool(t, context.Background(), "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	defer pool.Close()
	digest := sha256.Sum256([]byte("service account credential"))
	authorizer := &automationAuthorizer{
		result: api.PrincipalTokenAuthorization{
			PrincipalID: "automation-one",
			Subject:     common.SubjectRef{Kind: "serviceAccount", Issuer: tokenTestIssuer, Subject: "service-automation-one"},
			Issuer:      tokenTestIssuer,
			TenantID:    "tenant-one",
			Application: api.IdentityApplicationAdmin,
			Scopes:      []string{"agents.get"},
		},
	}
	store, err := identity.NewAutomationStore(
		pool,
		tokenTestSigner(t, tokenTestRSAKey(t, 2048)),
		authorizer,
		func(context.Context) (identity.TokenSigningAuthority, error) {
			now := time.Now().UTC()
			return identity.TokenSigningAuthority{
				SecurityEpoch: 1, NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
				KeyNotBefore: now.Add(-time.Hour), KeyNotAfter: now.Add(time.Hour),
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.IssueAutomationTenantToken(context.Background(), api.IdentityApplicationAdmin, digest, api.TenantTokenIssueRequest{TenantID: "tenant-one"})
	if !errors.Is(err, identity.ErrUnavailable) || result != (api.TenantToken{}) {
		t.Fatalf("database failure leaked token: %#v, %v", result, err)
	}
	if authorizer.calls != 1 || authorizer.last.Application != api.IdentityApplicationAdmin ||
		authorizer.last.ClientID != identity.AutomationClientID || authorizer.last.TenantID != "tenant-one" ||
		authorizer.last.CredentialSHA256 != "sha256:"+hex.EncodeToString(digest[:]) {
		t.Fatalf("authorization request = %#v", authorizer.last)
	}
}

func TestAutomationStorePreservesCurrentAuthorizationDenial(t *testing.T) {
	pool := tokenTestPool(t, context.Background(), "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	defer pool.Close()
	authorizer := &automationAuthorizer{err: &api.ClientError{
		Status: 403,
		Problem: &common.Problem{
			Status: 403,
			Error:  common.StableError{Code: "AUTHORIZATION_DENIED"},
		},
	}}
	store, err := identity.NewAutomationStore(
		pool,
		tokenTestSigner(t, tokenTestRSAKey(t, 2048)),
		authorizer,
		func(context.Context) (identity.TokenSigningAuthority, error) {
			t.Fatal("denied authorization reached signing authority")
			return identity.TokenSigningAuthority{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.IssueAutomationTenantToken(context.Background(), api.IdentityApplicationUser, [32]byte{1}, api.TenantTokenIssueRequest{TenantID: "tenant-one"})
	if !errors.Is(err, identity.ErrForbidden) || result != (api.TenantToken{}) || authorizer.calls != 1 {
		t.Fatalf("denied result = %#v, error = %v, calls = %d", result, err, authorizer.calls)
	}
}

func TestAutomationStoreRejectsAuthorizationAuthorityMismatch(t *testing.T) {
	pool := tokenTestPool(t, context.Background(), "postgres://unused@127.0.0.1:1/unused?sslmode=disable")
	defer pool.Close()
	authorizer := &automationAuthorizer{result: api.PrincipalTokenAuthorization{
		PrincipalID: "automation-one",
		Subject:     common.SubjectRef{Kind: "serviceAccount", Issuer: tokenTestIssuer, Subject: "service-automation-one"},
		Issuer:      tokenTestIssuer,
		TenantID:    "other-tenant",
		Application: api.IdentityApplicationAdmin,
		Scopes:      []string{"agents.get"},
	}}
	store, err := identity.NewAutomationStore(
		pool,
		tokenTestSigner(t, tokenTestRSAKey(t, 2048)),
		authorizer,
		func(context.Context) (identity.TokenSigningAuthority, error) {
			t.Fatal("mismatched authorization reached signing authority")
			return identity.TokenSigningAuthority{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.IssueAutomationTenantToken(context.Background(), api.IdentityApplicationAdmin, [32]byte{1}, api.TenantTokenIssueRequest{TenantID: "tenant-one"})
	if !errors.Is(err, identity.ErrUnavailable) || result != (api.TenantToken{}) {
		t.Fatalf("mismatched result = %#v, error = %v", result, err)
	}
}

type automationAuthorizer struct {
	result api.PrincipalTokenAuthorization
	err    error
	calls  int
	last   api.PrincipalTokenAuthorizationRequest
}

func (authorizer *automationAuthorizer) AuthorizePrincipalToken(_ context.Context, _ string, request api.PrincipalTokenAuthorizationRequest) (api.PrincipalTokenAuthorization, error) {
	authorizer.calls++
	authorizer.last = request
	return authorizer.result, authorizer.err
}
