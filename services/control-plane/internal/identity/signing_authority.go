package identity

import (
	"context"
	"encoding/base64"
	"strconv"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SigningAuthorityStore struct {
	pool   *pgxpool.Pool
	signer *TokenSigner
}

func NewSigningAuthorityStore(pool *pgxpool.Pool, signer *TokenSigner) (*SigningAuthorityStore, error) {
	if pool == nil || signer == nil || !validSigningKey(signer.privateKey) {
		return nil, errInvalidConfiguration
	}
	return &SigningAuthorityStore{pool: pool, signer: signer}, nil
}

func InitializeSigningAuthority(ctx context.Context, bootstrap *pgxpool.Pool, signer *TokenSigner, setupDigest [32]byte, notBefore, notAfter time.Time) (bool, error) {
	if bootstrap == nil || signer == nil || !validSigningKey(signer.privateKey) ||
		!validNumericDate(notBefore.Unix()) || !validNumericDate(notAfter.Unix()) || !notBefore.Before(notAfter) {
		return false, errInvalidConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	var created bool
	if err := bootstrap.QueryRow(ctx, `SELECT cloud_agents_identity.initialize_signing_authority($1,$2,$3,$4,$5,$6)`,
		signer.issuer, setupDigest[:], signer.keyID, base64.RawURLEncoding.EncodeToString(signer.privateKey.N.Bytes()),
		notBefore.Unix(), notAfter.Unix()).Scan(&created); err != nil {
		return false, ErrUnavailable
	}
	return created, nil
}

func (store *SigningAuthorityStore) JWKS(ctx context.Context) (api.IdentityJWKS, error) {
	document, _, err := store.read(ctx)
	return document, err
}

func (store *SigningAuthorityStore) SigningAuthority(ctx context.Context) (TokenSigningAuthority, error) {
	_, authority, err := store.read(ctx)
	return authority, err
}

func (store *SigningAuthorityStore) read(ctx context.Context) (api.IdentityJWKS, TokenSigningAuthority, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	modulus := base64.RawURLEncoding.EncodeToString(store.signer.privateKey.N.Bytes())
	rows, err := store.pool.Query(ctx, `SELECT revision, security_epoch, not_before, expires_at,
		key_id, modulus, key_not_before, key_not_after, enabled, current_key
		FROM cloud_agents_identity.read_signing_authority($1,$2,$3)`, store.signer.issuer, store.signer.keyID, modulus)
	if err != nil {
		return api.IdentityJWKS{}, TokenSigningAuthority{}, ErrUnavailable
	}
	defer rows.Close()
	document := api.IdentityJWKS{Keys: []api.IdentityJWK{}, CloudAgentsAuthority: api.IdentityJWKSAuthority{Issuer: store.signer.issuer}}
	var authority TokenSigningAuthority
	foundCurrent := false
	for rows.Next() {
		var revision, epoch, notBefore, expiresAt, keyNotBefore, keyNotAfter int64
		var keyID, keyModulus string
		var enabled, current bool
		if len(document.CloudAgentsAuthority.Lineage) >= 32 || rows.Scan(&revision, &epoch, &notBefore, &expiresAt,
			&keyID, &keyModulus, &keyNotBefore, &keyNotAfter, &enabled, &current) != nil {
			return api.IdentityJWKS{}, TokenSigningAuthority{}, ErrUnavailable
		}
		published := &document.CloudAgentsAuthority
		if len(published.Lineage) == 0 {
			published.Revision, published.SecurityEpoch = strconv.FormatInt(revision, 10), strconv.FormatInt(epoch, 10)
			published.NotBefore, published.ExpiresAt = notBefore, expiresAt
		} else if published.Revision != strconv.FormatInt(revision, 10) || published.SecurityEpoch != strconv.FormatInt(epoch, 10) ||
			published.NotBefore != notBefore || published.ExpiresAt != expiresAt {
			return api.IdentityJWKS{}, TokenSigningAuthority{}, ErrUnavailable
		}
		key := api.IdentityJWK{Alg: "RS256", E: "AQAB", KeyOps: []string{"verify"}, Kid: keyID, Kty: "RSA", N: keyModulus, Use: "sig"}
		published.Lineage = append(published.Lineage, api.IdentityJWKSLineageKey{JWK: key, Enabled: enabled, NotBefore: keyNotBefore, NotAfter: keyNotAfter})
		if enabled {
			document.Keys = append(document.Keys, key)
		}
		if current {
			if foundCurrent || !enabled || keyID != store.signer.keyID || keyModulus != modulus {
				return api.IdentityJWKS{}, TokenSigningAuthority{}, ErrUnavailable
			}
			foundCurrent = true
			authority = TokenSigningAuthority{SecurityEpoch: epoch, NotBefore: time.Unix(notBefore, 0).UTC(),
				ExpiresAt: time.Unix(expiresAt, 0).UTC(), KeyNotBefore: time.Unix(keyNotBefore, 0).UTC(), KeyNotAfter: time.Unix(keyNotAfter, 0).UTC()}
		}
	}
	if rows.Err() != nil || !foundCurrent || ctx.Err() != nil {
		return api.IdentityJWKS{}, TokenSigningAuthority{}, ErrUnavailable
	}
	if _, err := api.EncodeIdentityJWKSJSON(document); err != nil {
		return api.IdentityJWKS{}, TokenSigningAuthority{}, ErrUnavailable
	}
	return document, authority, nil
}
