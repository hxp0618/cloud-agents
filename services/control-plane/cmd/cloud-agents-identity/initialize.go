package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type initializeResult struct {
	RealmCreated            bool `json:"realmCreated"`
	SigningAuthorityCreated bool `json:"signingAuthorityCreated"`
}

func initializeIdentity(ctx context.Context, config initializeConfig) (initializeResult, error) {
	if ctx == nil {
		return initializeResult{}, errors.New("identity initialization context is required")
	}
	signer, err := loadTokenSigner(config.Issuer, config.AdminAudience, config.UserAudience, config.SigningKeyID, config.SigningPrivateKeyFile)
	if err != nil {
		return initializeResult{}, err
	}
	setupProof, err := readCanonicalProof(config.SetupProofFile)
	if err != nil {
		return initializeResult{}, errors.New("identity setup proof is invalid")
	}
	setupDigest, err := browserauth.ProofDigest(setupProof)
	if err != nil {
		return initializeResult{}, errors.New("identity setup proof is invalid")
	}
	passwordHash, err := readSecretText(config.PasswordHashFile, 256)
	if err != nil || !browserauth.ValidPasswordHash(passwordHash) {
		return initializeResult{}, errors.New("identity password hash is invalid")
	}
	if common.ValidateIdentifier(config.UserID, "/userId") != nil || !validInitialEmail(config.Email) ||
		!validBoundedUnicodeText(config.DisplayName, 1, 160) {
		return initializeResult{}, errors.New("identity initial account is invalid")
	}
	notBefore, err := parseCanonicalTime(config.KeyNotBefore)
	if err != nil {
		return initializeResult{}, errors.New("identity signing key interval is invalid")
	}
	notAfter, err := parseCanonicalTime(config.KeyNotAfter)
	if err != nil || !notBefore.Before(notAfter) {
		return initializeResult{}, errors.New("identity signing key interval is invalid")
	}
	databaseURL, err := readSecretText(config.BootstrapDatabaseURLFile, maximumIdentitySecretBytes)
	if err != nil {
		return initializeResult{}, errors.New("identity bootstrap database secret is invalid")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return initializeResult{}, errors.New("identity bootstrap database configuration is invalid")
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return initializeResult{}, errors.New("identity bootstrap database is unavailable")
	}
	defer pool.Close()
	if err := verifyIdentityBootstrapAuthority(ctx, pool); err != nil {
		return initializeResult{}, err
	}
	eventID, correlationID, err := newInitializationIDs()
	if err != nil {
		return initializeResult{}, errors.New("identity initialization ID is unavailable")
	}
	operationContext, cancel := context.WithTimeout(ctx, identityDatabaseTimeout)
	defer cancel()
	var realmCreated bool
	if err := pool.QueryRow(operationContext, `SELECT cloud_agents_identity.initialize_realm($1,$2,$3,$4,$5,$6,$7,$8)`,
		config.Issuer, setupDigest[:], config.UserID, config.Email, config.DisplayName, passwordHash, eventID, correlationID).Scan(&realmCreated); err != nil {
		return initializeResult{}, errors.New("identity realm initialization failed")
	}
	authorityCreated, err := identity.InitializeSigningAuthority(operationContext, pool, signer, setupDigest, notBefore, notAfter)
	if err != nil {
		return initializeResult{}, errors.New("identity signing authority initialization failed")
	}
	return initializeResult{RealmCreated: realmCreated, SigningAuthorityCreated: authorityCreated}, nil
}

func hashPasswordFile(passwordPath, outputPath string) error {
	passwordBytes, err := readRegularFile(passwordPath, 1024, true)
	if err != nil || strings.ContainsAny(string(passwordBytes), "\x00\r\n") {
		return errors.New("password secret file is invalid")
	}
	encoded, err := browserauth.HashPassword(string(passwordBytes))
	if err != nil {
		return errors.New("password secret file is invalid")
	}
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("password hash output file could not be created")
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(outputPath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return errors.New("password hash output file could not be secured")
	}
	if _, err := file.WriteString(encoded); err != nil {
		return errors.New("password hash output file could not be written")
	}
	if err := file.Sync(); err != nil {
		return errors.New("password hash output file could not be written")
	}
	if err := file.Close(); err != nil {
		return errors.New("password hash output file could not be written")
	}
	complete = true
	return nil
}

func verifyIdentityBootstrapAuthority(ctx context.Context, pool *pgxpool.Pool) error {
	checkContext, cancel := context.WithTimeout(ctx, identityDatabaseTimeout)
	defer cancel()
	var safe bool
	err := pool.QueryRow(checkContext, `SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
		AND NOT rolreplication AND NOT rolbypassrls
		AND pg_has_role(current_user, 'cloud_agents_bootstrap_admin', 'USAGE')
		AND NOT pg_has_role(current_user, 'cloud_agents_identity_service', 'USAGE')
		AND NOT pg_has_role(current_user, 'cloud_agents_runtime', 'USAGE')
		AND NOT pg_has_role(current_user, 'cloud_agents_migration_owner', 'USAGE')
		FROM pg_roles WHERE rolname = current_user`).Scan(&safe)
	if err != nil || !safe {
		return errors.New("identity bootstrap database authority is unsafe")
	}
	return nil
}

func parseCanonicalTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.UTC().Format(time.RFC3339) != value {
		return time.Time{}, errors.New("invalid time")
	}
	return parsed.UTC(), nil
}

func validInitialEmail(value string) bool {
	if len(value) < 3 || len(value) > 254 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t ") {
		return false
	}
	at := strings.LastIndexByte(value, '@')
	if at < 1 || at == len(value)-1 || strings.Contains(value[:at], "@") {
		return false
	}
	domain, err := browserauth.NormalizeEmailDomain(value)
	return err == nil && value[at+1:] == domain
}

func newInitializationID(prefix string) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(random[:]), nil
}

func newInitializationIDs() (string, string, error) {
	eventID, err := newInitializationID("")
	if err != nil {
		return "", "", err
	}
	correlationID, err := newInitializationID("identity-init-request-")
	if err != nil {
		return "", "", err
	}
	return eventID, correlationID, nil
}
