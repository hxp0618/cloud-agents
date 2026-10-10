package identity_test

import (
	"context"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSigningAuthorityPostgresFlow(t *testing.T) {
	serviceURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TEST_DATABASE_URL")
	bootstrapURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TEST_BOOTSTRAP_DATABASE_URL")
	fixtureURL := os.Getenv("CLOUD_AGENTS_IDENTITY_TEST_FIXTURE_DATABASE_URL")
	if serviceURL == "" || bootstrapURL == "" || fixtureURL == "" {
		t.Skip("signing authority test requires an initialized isolated realm with no signing authority")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	open := func(databaseURL string) *pgxpool.Pool {
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			t.Fatal("signing authority database unavailable")
		}
		t.Cleanup(pool.Close)
		return pool
	}
	service, bootstrap, fixture := open(serviceURL), open(bootstrapURL), open(fixtureURL)
	var issuer string
	var proof []byte
	if err := fixture.QueryRow(ctx, "SELECT issuer, setup_digest FROM cloud_agents_identity.realm").Scan(&issuer, &proof); err != nil || len(proof) != 32 {
		t.Fatal("initialized realm fixture unavailable")
	}
	var digest [32]byte
	copy(digest[:], proof)
	key := tokenTestRSAKey(t, 2048)
	signer, err := identity.NewTokenSigner(identity.TokenSignerConfig{
		Issuer: issuer, AdminAudience: tokenTestAdminAudience, UserAudience: tokenTestUserAudience,
		KeyID: "authority-key", PrivateKey: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	notBefore, notAfter := now.Add(-24*time.Hour), now.Add(24*time.Hour)
	if created, err := identity.InitializeSigningAuthority(ctx, bootstrap, signer, digest, notBefore, notAfter); err != nil || !created {
		t.Fatalf("initialize signing authority: created=%v err=%v", created, err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := fixture.Exec(cleanup, "DELETE FROM cloud_agents_identity.signing_authority WHERE current_key_id = 'authority-key'"); err != nil {
			t.Error("could not remove isolated authority fixture")
		}
		if _, err := fixture.Exec(cleanup, "DELETE FROM cloud_agents_identity.signing_keys WHERE key_id = 'authority-key'"); err != nil {
			t.Error("could not remove isolated signing key fixture")
		}
	})
	if created, err := identity.InitializeSigningAuthority(ctx, bootstrap, signer, digest, notBefore, notAfter); err != nil || created {
		t.Fatal("exact bootstrap retry was not idempotent")
	}
	if _, err := identity.InitializeSigningAuthority(ctx, bootstrap, signer, digest, notBefore, notAfter.Add(time.Hour)); err == nil {
		t.Fatal("bootstrap retry changed permanent key interval")
	}
	if _, err := identity.InitializeSigningAuthority(ctx, service, signer, digest, notBefore, notAfter); err == nil {
		t.Fatal("service principal initialized signing authority")
	}
	store, err := identity.NewSigningAuthorityStore(service, signer)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.JWKS(ctx)
	if err != nil || first.CloudAgentsAuthority.Revision != "1" || len(first.Keys) != 1 {
		t.Fatalf("initial JWKS: %v", err)
	}
	restarted, _ := identity.NewSigningAuthorityStore(service, signer)
	second, err := restarted.JWKS(ctx)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("restart changed persisted JWKS")
	}
	authority, err := restarted.SigningAuthority(ctx)
	if err != nil || authority.SecurityEpoch != 1 || !authority.KeyNotAfter.Equal(notAfter) {
		t.Fatal("signer authority differs from published key")
	}
	if _, err := service.Exec(ctx, "SELECT * FROM cloud_agents_identity.signing_keys"); err == nil {
		t.Fatal("identity service read signing tables directly")
	}
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents_identity.signing_authority
		SET not_before = not_before - 7*3600, expires_at = expires_at - 7*3600`); err != nil {
		t.Fatal("could not age isolated authority fixture")
	}
	var wait sync.WaitGroup
	results := make([]api.IdentityJWKS, 2)
	errors := make([]error, 2)
	for index := range results {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index], errors[index] = store.JWKS(ctx)
		}(index)
	}
	wait.Wait()
	if errors[0] != nil || errors[1] != nil || results[0].CloudAgentsAuthority.Revision != "2" || !reflect.DeepEqual(results[0], results[1]) {
		t.Fatal("concurrent refresh did not publish exactly one new durable revision")
	}
	wrongSigner, _ := identity.NewTokenSigner(identity.TokenSignerConfig{
		Issuer: issuer, AdminAudience: tokenTestAdminAudience, UserAudience: tokenTestUserAudience,
		KeyID: "authority-key", PrivateKey: tokenTestRSAKey(t, 2048),
	})
	wrong, _ := identity.NewSigningAuthorityStore(service, wrongSigner)
	if _, err := wrong.JWKS(ctx); err == nil {
		t.Fatal("changed key material was accepted")
	}
	if _, err := fixture.Exec(ctx, `UPDATE cloud_agents_identity.signing_authority
		SET not_before = not_before + 3600, expires_at = expires_at + 3600`); err != nil {
		t.Fatal("could not advance isolated authority fixture clock")
	}
	if _, err := store.JWKS(ctx); err == nil {
		t.Fatal("authority clock rollback was accepted")
	}
}
