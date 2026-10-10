package identitytrust_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identitytrust"
)

const testNow = int64(1_800_000_000)

var (
	testKeysOnce sync.Once
	testKeys     [2]*rsa.PrivateKey
	testKeysErr  error
)

type memoryCheckpoint struct {
	state    identitytrust.State
	exists   bool
	casCount int
}

func (checkpoint *memoryCheckpoint) Load(_ context.Context, issuer string) (identitytrust.State, bool, error) {
	if checkpoint.exists && checkpoint.state.Issuer != issuer {
		return identitytrust.State{}, false, errors.New("issuer mismatch")
	}
	return checkpoint.state, checkpoint.exists, nil
}

func (checkpoint *memoryCheckpoint) CompareAndSwap(_ context.Context, issuer, expectedDigest string, state identitytrust.State) error {
	if issuer != state.Issuer || checkpoint.exists && checkpoint.state.AuthorityDigest != expectedDigest || !checkpoint.exists && expectedDigest != "" {
		return errors.New("checkpoint conflict")
	}
	checkpoint.state = state
	checkpoint.exists = true
	checkpoint.casCount++
	return nil
}

type racingCheckpoint struct {
	memoryCheckpoint
	winner       identitytrust.State
	conflictOnce bool
}

func (checkpoint *racingCheckpoint) CompareAndSwap(ctx context.Context, issuer, expectedDigest string, state identitytrust.State) error {
	if checkpoint.conflictOnce && expectedDigest != "" {
		checkpoint.conflictOnce = false
		checkpoint.state = checkpoint.winner
		checkpoint.exists = true
		return errors.New("checkpoint compare-and-swap lost")
	}
	return checkpoint.memoryCheckpoint.CompareAndSwap(ctx, issuer, expectedDigest, state)
}

func TestAdapterMapsPublisherRevisionToLocalGenerationAndRealVerification(t *testing.T) {
	keys := signingKeys(t)
	firstKey := lineageKey(t, keys[0], "key-1", true)
	secondKey := lineageKey(t, keys[1], "key-2", true)
	documents := [][]byte{
		encodeAuthority(t, "https://issuer.example", "7", "3", []api.IdentityJWKSLineageKey{firstKey}),
		encodeAuthority(t, "https://issuer.example", "19", "3", []api.IdentityJWKSLineageKey{
			withEnabled(firstKey, false), secondKey,
		}),
	}
	checkpoint := &memoryCheckpoint{}
	index := 0
	adapter := newAdapter(t, checkpoint, func(context.Context, string) ([]byte, error) {
		document := documents[index]
		index++
		return document, nil
	})

	pair, err := adapter.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pair.Revision != 7 || pair.User.Generation != 1 || pair.Admin.Generation != 1 || checkpoint.casCount != 1 {
		t.Fatalf("unexpected first mapping: revision=%d generations=%d/%d cas=%d", pair.Revision, pair.User.Generation, pair.Admin.Generation, checkpoint.casCount)
	}
	userVerifier, err := authn.NewConfiguredVerifier(pair.User)
	if err != nil {
		t.Fatal(err)
	}
	defer userVerifier.Invalidate()
	adminVerifier, err := authn.NewConfiguredVerifier(pair.Admin)
	if err != nil {
		t.Fatal(err)
	}
	defer adminVerifier.Invalidate()
	request := authn.VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"}
	if _, err := userVerifier.Verify(accessToken(t, keys[0], "key-1", "https://api.example", 3), request); err != nil {
		t.Fatalf("first real token verification failed: %v", err)
	}

	rotated, changed, err := adapter.Refresh(context.Background())
	if err != nil || !changed {
		t.Fatalf("rotation result changed=%t err=%v", changed, err)
	}
	if rotated.Revision != 19 || rotated.User.Generation != 2 || rotated.Admin.Generation != 2 || len(rotated.User.Keys) != 2 || checkpoint.casCount != 2 {
		t.Fatalf("publisher revision was not mapped to one local generation: %+v", rotated)
	}
	if rotated.User.Keys[0].Enabled || !rotated.User.Keys[1].Enabled {
		t.Fatal("retired and active key states were not preserved")
	}
	if err := userVerifier.ReloadTogether(rotated.User, adminVerifier, rotated.Admin); err != nil {
		t.Fatalf("real verifier pair rejected accepted rotation: %v", err)
	}
	if _, err := userVerifier.Verify(accessToken(t, keys[1], "key-2", "https://api.example", 3), request); err != nil {
		t.Fatalf("rotated real token verification failed: %v", err)
	}
	if _, err := userVerifier.Verify(accessToken(t, keys[0], "key-1", "https://api.example", 3), request); err == nil {
		t.Fatal("retired signing key remained active")
	}
}

func TestAdapterRecoversCumulativeLineageFromCheckpoint(t *testing.T) {
	keys := signingKeys(t)
	firstKey := lineageKey(t, keys[0], "key-1", false)
	secondKey := lineageKey(t, keys[1], "key-2", true)
	checkpoint := &memoryCheckpoint{}
	initial := newAdapter(t, checkpoint, fixedFetch(encodeAuthority(t, "https://issuer.example", "23", "4", []api.IdentityJWKSLineageKey{firstKey, secondKey})))
	if _, err := initial.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	restarted := newAdapter(t, checkpoint, func(context.Context, string) ([]byte, error) {
		return nil, fmt.Errorf("identity service unavailable: %w", identitytrust.ErrTransportUnavailable)
	})
	pair, err := restarted.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pair.Revision != 23 || pair.User.Generation != 1 || len(pair.User.Keys) != 2 || pair.User.Keys[0].Enabled || !pair.User.Keys[1].Enabled {
		t.Fatalf("restart did not reconstruct cumulative lineage: %+v", pair)
	}
	verifier, err := authn.NewConfiguredVerifier(pair.User)
	if err != nil {
		t.Fatal(err)
	}
	defer verifier.Invalidate()
	request := authn.VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"}
	if _, err := verifier.Verify(accessToken(t, keys[1], "key-2", "https://api.example", 4), request); err != nil {
		t.Fatalf("checkpoint-recovered verifier rejected current token: %v", err)
	}
}

func TestAdapterRejectsNonTransportStartupFailuresWithCheckpoint(t *testing.T) {
	keys := signingKeys(t)
	document := encodeAuthority(t, "https://issuer.example", "23", "4", []api.IdentityJWKSLineageKey{
		lineageKey(t, keys[0], "key-1", true),
	})
	checkpoint := &memoryCheckpoint{}
	initial := newAdapter(t, checkpoint, fixedFetch(document))
	if _, err := initial.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		ctx   func() context.Context
		fetch identitytrust.FetchOperation
	}{
		{
			name: "generic fetch failure",
			ctx:  context.Background,
			fetch: func(context.Context, string) ([]byte, error) {
				return nil, errors.New("invalid identity response")
			},
		},
		{
			name: "malformed authority",
			ctx:  context.Background,
			fetch: func(context.Context, string) ([]byte, error) {
				return []byte(`{"keys":`), nil
			},
		},
		{
			name: "canceled transport failure",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			fetch: func(context.Context, string) ([]byte, error) {
				return nil, fmt.Errorf("fetch canceled: %w", identitytrust.ErrTransportUnavailable)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := newAdapter(t, checkpoint, test.fetch)
			if _, err := adapter.Start(test.ctx()); !errors.Is(err, identitytrust.ErrInvalidAuthority) {
				t.Fatalf("startup failure used the checkpoint: %v", err)
			}
		})
	}
}

func TestAdapterRejectsAuthorityRollbackDriftAndKeyBoundaryFaults(t *testing.T) {
	keys := signingKeys(t)
	baseKey := lineageKey(t, keys[0], "key-1", true)
	base := encodeAuthority(t, "https://issuer.example", "7", "3", []api.IdentityJWKSLineageKey{baseKey})
	otherMaterial := lineageKey(t, keys[1], "key-1", true)
	tooMany := make([]api.IdentityJWKSLineageKey, 33)
	tooManyActive := make([]api.IdentityJWK, 33)
	for index := range tooMany {
		tooMany[index] = baseKey
		tooMany[index].JWK.Kid = fmt.Sprintf("key-%02d", index)
		tooManyActive[index] = tooMany[index].JWK
	}

	tests := []struct {
		name      string
		candidate []byte
	}{
		{name: "pinned issuer", candidate: encodeAuthority(t, "https://substituted.example", "8", "3", []api.IdentityJWKSLineageKey{baseKey})},
		{name: "same revision drift", candidate: encodeAuthorityAt(t, "https://issuer.example", "7", "3", testNow-50, testNow+900, []api.IdentityJWKSLineageKey{baseKey})},
		{name: "revision rollback", candidate: encodeAuthority(t, "https://issuer.example", "6", "3", []api.IdentityJWKSLineageKey{baseKey})},
		{name: "epoch rollback", candidate: encodeAuthority(t, "https://issuer.example", "8", "2", []api.IdentityJWKSLineageKey{baseKey})},
		{name: "missing permanent kid", candidate: encodeAuthority(t, "https://issuer.example", "8", "3", []api.IdentityJWKSLineageKey{lineageKey(t, keys[1], "key-2", true)})},
		{name: "kid material conflict", candidate: encodeAuthority(t, "https://issuer.example", "8", "3", []api.IdentityJWKSLineageKey{otherMaterial})},
		{name: "active projection mismatch", candidate: marshalAuthorityUnchecked(t, "https://issuer.example", "8", "3", []api.IdentityJWKSLineageKey{baseKey}, []api.IdentityJWK{})},
		{name: "invalid key interval", candidate: func() []byte {
			invalid := baseKey
			invalid.NotAfter = invalid.NotBefore
			return marshalAuthorityUnchecked(t, "https://issuer.example", "8", "3", []api.IdentityJWKSLineageKey{invalid}, []api.IdentityJWK{invalid.JWK})
		}()},
		{name: "lineage bound", candidate: marshalAuthorityUnchecked(t, "https://issuer.example", "8", "3", tooMany, tooManyActive)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkpoint := &memoryCheckpoint{}
			calls := 0
			adapter := newAdapter(t, checkpoint, func(context.Context, string) ([]byte, error) {
				calls++
				if calls == 1 {
					return base, nil
				}
				return test.candidate, nil
			})
			if _, err := adapter.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, changed, err := adapter.Refresh(context.Background()); changed || !errors.Is(err, identitytrust.ErrInvalidAuthority) {
				t.Fatalf("fault result changed=%t err=%v", changed, err)
			}
			if checkpoint.casCount != 1 || checkpoint.state.Revision != 7 {
				t.Fatal("rejected authority changed the checkpoint")
			}
		})
	}
}

func TestAdapterTreatsIdenticalRevisionAsNoOp(t *testing.T) {
	keys := signingKeys(t)
	document := encodeAuthority(t, "https://issuer.example", "9", "3", []api.IdentityJWKSLineageKey{lineageKey(t, keys[0], "key-1", true)})
	checkpoint := &memoryCheckpoint{}
	adapter := newAdapter(t, checkpoint, fixedFetch(document))
	first, err := adapter.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, changed, err := adapter.Refresh(context.Background())
	if err != nil || changed || checkpoint.casCount != 1 || second.User.Generation != first.User.Generation || second.AuthorityDigest != first.AuthorityDigest {
		t.Fatalf("identical authority was not a no-op: changed=%t err=%v cas=%d", changed, err, checkpoint.casCount)
	}
}

func TestAdapterRefreshPreservesTransportUnavailable(t *testing.T) {
	keys := signingKeys(t)
	base := encodeAuthority(t, "https://issuer.example", "7", "3", []api.IdentityJWKSLineageKey{lineageKey(t, keys[0], "key-1", true)})
	checkpoint := &memoryCheckpoint{}
	calls := 0
	adapter := newAdapter(t, checkpoint, func(context.Context, string) ([]byte, error) {
		calls++
		if calls == 1 {
			return base, nil
		}
		return nil, fmt.Errorf("identity endpoint offline: %w", identitytrust.ErrTransportUnavailable)
	})
	if _, err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := adapter.Refresh(context.Background()); changed || !errors.Is(err, identitytrust.ErrTransportUnavailable) || errors.Is(err, identitytrust.ErrInvalidAuthority) {
		t.Fatalf("transport refresh changed=%t err=%v", changed, err)
	}
}

func TestAdapterRefreshReconcilesCompatibleCASWinner(t *testing.T) {
	keys := signingKeys(t)
	first := lineageKey(t, keys[0], "key-1", true)
	second := lineageKey(t, keys[1], "key-2", true)
	base := encodeAuthority(t, "https://issuer.example", "7", "3", []api.IdentityJWKSLineageKey{first})
	candidate := encodeAuthority(t, "https://issuer.example", "8", "3", []api.IdentityJWKSLineageKey{first})
	winnerDocument := encodeAuthority(t, "https://issuer.example", "19", "4", []api.IdentityJWKSLineageKey{withEnabled(first, false), second})
	winner := checkpointState(t, winnerDocument)
	checkpoint := &racingCheckpoint{winner: winner, conflictOnce: true}
	calls := 0
	adapter := newAdapter(t, checkpoint, func(context.Context, string) ([]byte, error) {
		calls++
		if calls == 1 {
			return base, nil
		}
		return candidate, nil
	})
	if _, err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pair, changed, err := adapter.Refresh(context.Background())
	if err != nil || !changed || pair.Revision != 19 || pair.User.Generation != 2 || pair.User.SecurityEpoch != 4 || pair.AuthorityDigest != winner.AuthorityDigest {
		t.Fatalf("CAS winner was not reconciled: changed=%t pair=%+v err=%v", changed, pair, err)
	}
}

func TestAdapterRefreshAdvancesFromCompatibleEarlierCASWinner(t *testing.T) {
	keys := signingKeys(t)
	first := lineageKey(t, keys[0], "key-1", true)
	second := lineageKey(t, keys[1], "key-2", true)
	base := encodeAuthority(t, "https://issuer.example", "7", "3", []api.IdentityJWKSLineageKey{first})
	winnerDocument := encodeAuthority(t, "https://issuer.example", "8", "3", []api.IdentityJWKSLineageKey{first})
	candidate := encodeAuthority(t, "https://issuer.example", "19", "4", []api.IdentityJWKSLineageKey{withEnabled(first, false), second})
	checkpoint := &racingCheckpoint{winner: checkpointState(t, winnerDocument), conflictOnce: true}
	calls := 0
	adapter := newAdapter(t, checkpoint, func(context.Context, string) ([]byte, error) {
		calls++
		if calls == 1 {
			return base, nil
		}
		return candidate, nil
	})
	if _, err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	pair, changed, err := adapter.Refresh(context.Background())
	if err != nil || !changed || pair.Revision != 19 || checkpoint.state.Revision != 19 || checkpoint.casCount != 2 {
		t.Fatalf("candidate did not advance compatible CAS winner: changed=%t pair=%+v checkpoint=%+v err=%v", changed, pair, checkpoint.state, err)
	}
}

func TestAdapterRefreshRejectsDivergentCASWinner(t *testing.T) {
	keys := signingKeys(t)
	first := lineageKey(t, keys[0], "key-1", true)
	base := encodeAuthority(t, "https://issuer.example", "7", "3", []api.IdentityJWKSLineageKey{first})
	candidate := encodeAuthority(t, "https://issuer.example", "8", "3", []api.IdentityJWKSLineageKey{first})
	drifted := encodeAuthorityAt(t, "https://issuer.example", "8", "3", testNow-50, testNow+900, []api.IdentityJWKSLineageKey{first})
	checkpoint := &racingCheckpoint{winner: checkpointState(t, drifted), conflictOnce: true}
	calls := 0
	adapter := newAdapter(t, checkpoint, func(context.Context, string) ([]byte, error) {
		calls++
		if calls == 1 {
			return base, nil
		}
		return candidate, nil
	})
	if _, err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := adapter.Refresh(context.Background()); changed || !errors.Is(err, identitytrust.ErrInvalidAuthority) {
		t.Fatalf("divergent CAS winner changed=%t err=%v", changed, err)
	}
}

func checkpointState(t *testing.T, document []byte) identitytrust.State {
	t.Helper()
	checkpoint := &memoryCheckpoint{}
	adapter := newAdapter(t, checkpoint, fixedFetch(document))
	if _, err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return checkpoint.state
}

func newAdapter(t *testing.T, checkpoint identitytrust.Checkpoint, fetch identitytrust.FetchOperation) *identitytrust.Adapter {
	t.Helper()
	adapter, err := identitytrust.New(identitytrust.Config{
		Issuer: "https://issuer.example", UserAudience: "https://api.example", AdminAudience: "https://admin.example",
		JWKSURL: "https://issuer.example/.well-known/jwks.json", Clock: func() time.Time { return time.Unix(testNow, 0) },
		Checkpoint: checkpoint, Fetch: fetch,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func fixedFetch(document []byte) identitytrust.FetchOperation {
	return func(context.Context, string) ([]byte, error) { return document, nil }
}

func signingKeys(t *testing.T) [2]*rsa.PrivateKey {
	t.Helper()
	testKeysOnce.Do(func() {
		testKeys[0], testKeysErr = rsa.GenerateKey(rand.Reader, 2048)
		if testKeysErr == nil {
			testKeys[1], testKeysErr = rsa.GenerateKey(rand.Reader, 2048)
		}
	})
	if testKeysErr != nil {
		t.Fatal(testKeysErr)
	}
	return testKeys
}

func lineageKey(t *testing.T, key *rsa.PrivateKey, kid string, enabled bool) api.IdentityJWKSLineageKey {
	t.Helper()
	return api.IdentityJWKSLineageKey{
		JWK: api.IdentityJWK{
			Alg: "RS256", E: "AQAB", KeyOps: []string{"verify"}, Kid: kid,
			Kty: "RSA", N: base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), Use: "sig",
		},
		Enabled: enabled, NotBefore: testNow - 1000, NotAfter: testNow + 1000,
	}
}

func withEnabled(key api.IdentityJWKSLineageKey, enabled bool) api.IdentityJWKSLineageKey {
	key.Enabled = enabled
	return key
}

func encodeAuthority(t *testing.T, issuer, revision, epoch string, lineage []api.IdentityJWKSLineageKey) []byte {
	t.Helper()
	return encodeAuthorityAt(t, issuer, revision, epoch, testNow-100, testNow+1000, lineage)
}

func encodeAuthorityAt(t *testing.T, issuer, revision, epoch string, notBefore, expiresAt int64, lineage []api.IdentityJWKSLineageKey) []byte {
	t.Helper()
	active := make([]api.IdentityJWK, 0, len(lineage))
	for _, key := range lineage {
		if key.Enabled {
			active = append(active, key.JWK)
		}
	}
	return encodeAuthorityWithProjectionAt(t, issuer, revision, epoch, notBefore, expiresAt, lineage, active)
}

func encodeAuthorityWithProjectionAt(t *testing.T, issuer, revision, epoch string, notBefore, expiresAt int64, lineage []api.IdentityJWKSLineageKey, active []api.IdentityJWK) []byte {
	t.Helper()
	encoded, err := api.EncodeIdentityJWKSJSON(api.IdentityJWKS{
		Keys: active,
		CloudAgentsAuthority: api.IdentityJWKSAuthority{
			Issuer: issuer, Revision: revision, SecurityEpoch: epoch,
			NotBefore: notBefore, ExpiresAt: expiresAt, Lineage: lineage,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func marshalAuthorityUnchecked(t *testing.T, issuer, revision, epoch string, lineage []api.IdentityJWKSLineageKey, active []api.IdentityJWK) []byte {
	t.Helper()
	encoded, err := json.Marshal(api.IdentityJWKS{
		Keys: active,
		CloudAgentsAuthority: api.IdentityJWKSAuthority{
			Issuer: issuer, Revision: revision, SecurityEpoch: epoch,
			NotBefore: testNow - 100, ExpiresAt: testNow + 1000, Lineage: lineage,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func accessToken(t *testing.T, key *rsa.PrivateKey, kid, audience string, epoch int64) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "RS256", "kid": kid, "typ": "at+jwt"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := json.Marshal(map[string]any{
		"iss": "https://issuer.example", "sub": "user-1", "aud": audience,
		"exp": testNow + 300, "iat": testNow - 10, "jti": "token-1", "client_id": "client-1",
		"scope": "agents.get", "https://schemas.cloud-agents.dev/claims/subject-kind": "user",
		"https://schemas.cloud-agents.dev/claims/tenant-id":      "tenant-1",
		"https://schemas.cloud-agents.dev/claims/security-epoch": epoch,
		"https://schemas.cloud-agents.dev/claims/token-profile":  "cloud-agents-access-token/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	protected := base64.RawURLEncoding.EncodeToString(header)
	payload := base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(protected + "." + payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return protected + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
}
