package identity

import (
	"context"
	"errors"
	"net/http"
	"testing"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
)

type providerMaterialResolverFake struct {
	secretRef string
	rootRef   string
}

func (resolver *providerMaterialResolverFake) ClientSecret(reference string) (string, error) {
	resolver.secretRef = reference
	if reference == "missing" {
		return "", errors.New("missing")
	}
	return "provider-client-secret", nil
}
func (resolver *providerMaterialResolverFake) HTTPClient(reference string) (*http.Client, error) {
	resolver.rootRef = reference
	return &http.Client{Transport: http.DefaultTransport}, nil
}

func TestRuntimeProviderFactoryUsesOnlyConfiguredMaterialReferences(t *testing.T) {
	resolver := &providerMaterialResolverFake{}
	factory, err := NewRuntimeProviderFactory(resolver)
	if err != nil {
		t.Fatal(err)
	}
	client := ProviderClientSnapshot{
		ID: "github-main", Application: api.IdentityApplicationUser, Kind: "github",
		Issuer: githubProviderIssuer, ClientID: "client-id", RedirectURL: "https://user.example/callback",
		SecretRef: "github-secret", RootCARef: "public-roots", Scopes: []string{"read:user", "user:email"}, Revision: 1,
	}
	if _, err := factory.Provider(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if resolver.secretRef != client.SecretRef || resolver.rootRef != client.RootCARef {
		t.Fatalf("material references = %q, %q", resolver.secretRef, resolver.rootRef)
	}
	client.Issuer = "https://attacker.example"
	if _, err := factory.Provider(context.Background(), client); err == nil {
		t.Fatal("accepted mutable issuer for fixed GitHub adapter")
	}
}

func TestRuntimeProviderFactoryRequiresExplicitCorporateTrustOrganization(t *testing.T) {
	factory, err := NewRuntimeProviderFactory(&providerMaterialResolverFake{})
	if err != nil {
		t.Fatal(err)
	}
	client := ProviderClientSnapshot{
		ID: "feishu-main", Application: api.IdentityApplicationAdmin, Kind: "feishu",
		Issuer: feishuProviderIssuer, ClientID: "client-id", RedirectURL: "https://admin.example/callback",
		SecretRef: "feishu-secret", Scopes: []string{"contact:user.email:readonly"}, Revision: 1, TrustProviderEmail: true,
	}
	if _, err := factory.Provider(context.Background(), client); err == nil {
		t.Fatal("accepted provider-email trust without an organization allowlist")
	}
}
