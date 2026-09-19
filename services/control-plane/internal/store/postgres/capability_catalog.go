package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrCapabilityNotFound = errors.New("managed capability was not found")
var ErrCapabilityIdempotencyConflict = errors.New("managed capability idempotency key conflicts")
var ErrCapabilityResourceVersionConflict = errors.New("managed capability resource version conflicts")

type McpServerPage struct {
	Servers      []platform.McpServer
	NextServerID string
}

type SkillBundlePage struct {
	Bundles      []platform.SkillBundle
	NextBundleID string
}

type capabilityRow struct {
	ID               string
	Version          string
	Digest           string
	Transport        string
	ConnectionRef    string
	CredentialRef    string
	NetworkPolicyRef string
	Permissions      []string
	SourceRef        string
	SignatureRef     string
	SigningKeyID     string
	Providers        []string
	Status           string
	ResourceVersion  int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	RevokedAt        *time.Time
}

type mcpServerPageRow struct {
	TenantID         string     `json:"tenant_id"`
	ProjectID        string     `json:"project_uid"`
	ServerID         string     `json:"server_uid"`
	Version          string     `json:"version"`
	Digest           string     `json:"digest"`
	Transport        string     `json:"transport"`
	ConnectionRef    string     `json:"connection_ref"`
	CredentialRef    string     `json:"credential_ref"`
	NetworkPolicyRef string     `json:"network_policy_ref"`
	Permissions      []string   `json:"permissions"`
	Status           string     `json:"status"`
	ResourceVersion  int64      `json:"resource_version"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	RevokedAt        *time.Time `json:"revoked_at"`
}

type skillBundlePageRow struct {
	TenantID            string     `json:"tenant_id"`
	ProjectID           string     `json:"project_uid"`
	BundleID            string     `json:"bundle_uid"`
	Version             string     `json:"version"`
	Digest              string     `json:"digest"`
	SourceRef           string     `json:"source_ref"`
	SignatureRef        string     `json:"signature_ref"`
	SigningKeyID        string     `json:"signing_key_id"`
	CompatibleProviders []string   `json:"compatible_providers"`
	Status              string     `json:"status"`
	ResourceVersion     int64      `json:"resource_version"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	RevokedAt           *time.Time `json:"revoked_at"`
}

const (
	createMcpServerSQL   = `SELECT server_uid, version, digest, transport, connection_ref, credential_ref, network_policy_ref, permissions, status, resource_version, created_at, updated_at, revoked_at FROM cloud_agents.create_mcp_server_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
	revokeMcpServerSQL   = `SELECT server_uid, version, digest, transport, connection_ref, credential_ref, network_policy_ref, permissions, status, resource_version, created_at, updated_at, revoked_at FROM cloud_agents.revoke_mcp_server_v1($1,$2,$3,$4,$5,$6)`
	getMcpServerSQL      = `SELECT server_uid, version, digest, transport, connection_ref, credential_ref, network_policy_ref, permissions, status, resource_version, created_at, updated_at, revoked_at FROM cloud_agents.mcp_servers WHERE tenant_id=cloud_agents.require_tenant_id() AND project_uid=$1 AND server_uid=$2`
	listMcpServersSQL    = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(item) ORDER BY item.server_uid),'[]'::jsonb) FROM (SELECT tenant_id,project_uid,server_uid,version,digest,transport,connection_ref,credential_ref,network_policy_ref,permissions,status,resource_version,created_at,updated_at,revoked_at FROM cloud_agents.mcp_servers WHERE tenant_id=cloud_agents.require_tenant_id() AND project_uid=$1 AND server_uid>$2 ORDER BY server_uid LIMIT $3) AS item`
	createSkillBundleSQL = `SELECT bundle_uid, version, digest, source_ref, signature_ref, signing_key_id, compatible_providers, status, resource_version, created_at, updated_at, revoked_at FROM cloud_agents.create_skill_bundle_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	revokeSkillBundleSQL = `SELECT bundle_uid, version, digest, source_ref, signature_ref, signing_key_id, compatible_providers, status, resource_version, created_at, updated_at, revoked_at FROM cloud_agents.revoke_skill_bundle_v1($1,$2,$3,$4,$5,$6)`
	getSkillBundleSQL    = `SELECT bundle_uid, version, digest, source_ref, signature_ref, signing_key_id, compatible_providers, status, resource_version, created_at, updated_at, revoked_at FROM cloud_agents.skill_bundles WHERE tenant_id=cloud_agents.require_tenant_id() AND project_uid=$1 AND bundle_uid=$2`
	listSkillBundlesSQL  = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(item) ORDER BY item.bundle_uid),'[]'::jsonb) FROM (SELECT tenant_id,project_uid,bundle_uid,version,digest,source_ref,signature_ref,signing_key_id,compatible_providers,status,resource_version,created_at,updated_at,revoked_at FROM cloud_agents.skill_bundles WHERE tenant_id=cloud_agents.require_tenant_id() AND project_uid=$1 AND bundle_uid>$2 ORDER BY bundle_uid LIMIT $3) AS item`
)

func (service *DurableCoordinationService) CreateMcpServer(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, idempotencyKey string, input platform.McpServerCreateRequest) (platform.McpServer, error) {
	if service == nil || service.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validIdempotencyKey(idempotencyKey) || !validCapabilityCreateMcp(input) {
		return platform.McpServer{}, ErrCoordinationInvalidInput
	}
	encoded, err := platform.EncodeMcpServerCreateRequestJSON(input)
	if err != nil {
		return platform.McpServer{}, ErrCoordinationInvalidInput
	}
	var row capabilityRow
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, projectID, func(handle *tenantReadHandle) error {
		return scanMcpServer(handle.transaction.queryRow(ctx, createMcpServerSQL, tenantID, projectID, input.ServerID, input.Version, input.Digest, input.Transport, input.ConnectionRef, input.CredentialRef, input.NetworkPolicyRef, input.Permissions, idempotencyKey, capabilityDigest("mcp.create", encoded)), &row)
	})
	return mcpServerResource(tenantID, projectID, row), mapCapabilityError(err)
}

func (service *DurableCoordinationService) RevokeMcpServer(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, serverID, idempotencyKey string, input platform.McpServerRevokeRequest) (platform.McpServer, error) {
	if service == nil || service.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(serverID) || !validIdempotencyKey(idempotencyKey) {
		return platform.McpServer{}, ErrCoordinationInvalidInput
	}
	expected, err := strconv.ParseInt(input.ExpectedResourceVersion, 10, 64)
	if err != nil || expected < 1 {
		return platform.McpServer{}, ErrCoordinationInvalidInput
	}
	encoded, err := platform.EncodeMcpServerRevokeRequestJSON(input)
	if err != nil {
		return platform.McpServer{}, ErrCoordinationInvalidInput
	}
	var row capabilityRow
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, projectID, func(handle *tenantReadHandle) error {
		return scanMcpServer(handle.transaction.queryRow(ctx, revokeMcpServerSQL, tenantID, projectID, serverID, expected, idempotencyKey, capabilityDigest("mcp.revoke", encoded)), &row)
	})
	return mcpServerResource(tenantID, projectID, row), mapCapabilityError(err)
}

func (service *DurableCoordinationService) GetMcpServer(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, serverID string) (platform.McpServer, error) {
	if service == nil || service.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(serverID) {
		return platform.McpServer{}, ErrCoordinationInvalidInput
	}
	var row capabilityRow
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				return scanMcpServer(handle.transaction.queryRow(readContext, getMcpServerSQL, projectID, serverID), &row)
			})
		})
	})
	return mcpServerResource(tenantID, projectID, row), mapCapabilityError(err)
}

func (service *DurableCoordinationService) ListMcpServers(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, after string, limit int) (McpServerPage, error) {
	if service == nil || service.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || after != "" && !validMutationIdentifier(after) || limit < 1 || limit > 200 {
		return McpServerPage{}, ErrCoordinationInvalidInput
	}
	var raw []byte
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if after != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, `SELECT 1 FROM cloud_agents.mcp_servers WHERE tenant_id=cloud_agents.require_tenant_id() AND project_uid=$1 AND server_uid=$2`, projectID, after).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				return handle.transaction.queryRow(readContext, listMcpServersSQL, projectID, after, limit+1).Scan(&raw)
			})
		})
	})
	if err != nil {
		return McpServerPage{}, mapCapabilityError(err)
	}
	var rows []mcpServerPageRow
	if json.Unmarshal(raw, &rows) != nil || len(rows) > limit+1 {
		return McpServerPage{}, ErrCoordinationResultDrift
	}
	page := McpServerPage{Servers: make([]platform.McpServer, 0, len(rows))}
	for _, item := range rows {
		if item.TenantID != tenantID || item.ProjectID != projectID || !validMutationIdentifier(item.ServerID) || !validMutationIdentifier(item.Version) || !validCoordinationDigest(item.Digest) {
			return McpServerPage{}, ErrCoordinationResultDrift
		}
		page.Servers = append(page.Servers, mcpServerResource(tenantID, projectID, capabilityRow{ID: item.ServerID, Version: item.Version, Digest: item.Digest, Transport: item.Transport, ConnectionRef: item.ConnectionRef, CredentialRef: item.CredentialRef, NetworkPolicyRef: item.NetworkPolicyRef, Permissions: item.Permissions, Status: item.Status, ResourceVersion: item.ResourceVersion, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, RevokedAt: item.RevokedAt}))
	}
	if len(page.Servers) > limit {
		page.Servers = page.Servers[:limit]
		page.NextServerID = page.Servers[len(page.Servers)-1].Metadata.UID
	}
	return page, nil
}

func (service *DurableCoordinationService) CreateSkillBundle(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, idempotencyKey string, input platform.SkillBundleCreateRequest) (platform.SkillBundle, error) {
	if service == nil || service.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validIdempotencyKey(idempotencyKey) || !validCapabilityCreateSkill(input) {
		return platform.SkillBundle{}, ErrCoordinationInvalidInput
	}
	encoded, err := platform.EncodeSkillBundleCreateRequestJSON(input)
	if err != nil {
		return platform.SkillBundle{}, ErrCoordinationInvalidInput
	}
	var row capabilityRow
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, projectID, func(handle *tenantReadHandle) error {
		return scanSkillBundle(handle.transaction.queryRow(ctx, createSkillBundleSQL, tenantID, projectID, input.BundleID, input.Version, input.Digest, input.SourceRef, input.SignatureRef, input.SigningKeyID, input.CompatibleProviders, idempotencyKey, capabilityDigest("skill.create", encoded)), &row)
	})
	return skillBundleResource(tenantID, projectID, row), mapCapabilityError(err)
}

func (service *DurableCoordinationService) RevokeSkillBundle(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, bundleID, idempotencyKey string, input platform.SkillBundleRevokeRequest) (platform.SkillBundle, error) {
	if service == nil || service.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(bundleID) || !validIdempotencyKey(idempotencyKey) {
		return platform.SkillBundle{}, ErrCoordinationInvalidInput
	}
	expected, err := strconv.ParseInt(input.ExpectedResourceVersion, 10, 64)
	if err != nil || expected < 1 {
		return platform.SkillBundle{}, ErrCoordinationInvalidInput
	}
	encoded, err := platform.EncodeSkillBundleRevokeRequestJSON(input)
	if err != nil {
		return platform.SkillBundle{}, ErrCoordinationInvalidInput
	}
	var row capabilityRow
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, projectID, func(handle *tenantReadHandle) error {
		return scanSkillBundle(handle.transaction.queryRow(ctx, revokeSkillBundleSQL, tenantID, projectID, bundleID, expected, idempotencyKey, capabilityDigest("skill.revoke", encoded)), &row)
	})
	return skillBundleResource(tenantID, projectID, row), mapCapabilityError(err)
}

func (service *DurableCoordinationService) GetSkillBundle(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, bundleID string) (platform.SkillBundle, error) {
	if service == nil || service.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(bundleID) {
		return platform.SkillBundle{}, ErrCoordinationInvalidInput
	}
	var row capabilityRow
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				return scanSkillBundle(handle.transaction.queryRow(readContext, getSkillBundleSQL, projectID, bundleID), &row)
			})
		})
	})
	return skillBundleResource(tenantID, projectID, row), mapCapabilityError(err)
}

func (service *DurableCoordinationService) ListSkillBundles(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, after string, limit int) (SkillBundlePage, error) {
	if service == nil || service.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || after != "" && !validMutationIdentifier(after) || limit < 1 || limit > 200 {
		return SkillBundlePage{}, ErrCoordinationInvalidInput
	}
	var raw []byte
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if after != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, `SELECT 1 FROM cloud_agents.skill_bundles WHERE tenant_id=cloud_agents.require_tenant_id() AND project_uid=$1 AND bundle_uid=$2`, projectID, after).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				return handle.transaction.queryRow(readContext, listSkillBundlesSQL, projectID, after, limit+1).Scan(&raw)
			})
		})
	})
	if err != nil {
		return SkillBundlePage{}, mapCapabilityError(err)
	}
	var rows []skillBundlePageRow
	if json.Unmarshal(raw, &rows) != nil || len(rows) > limit+1 {
		return SkillBundlePage{}, ErrCoordinationResultDrift
	}
	page := SkillBundlePage{Bundles: make([]platform.SkillBundle, 0, len(rows))}
	for _, item := range rows {
		if item.TenantID != tenantID || item.ProjectID != projectID || !validMutationIdentifier(item.BundleID) || !validMutationIdentifier(item.Version) || !validCoordinationDigest(item.Digest) {
			return SkillBundlePage{}, ErrCoordinationResultDrift
		}
		page.Bundles = append(page.Bundles, skillBundleResource(tenantID, projectID, capabilityRow{ID: item.BundleID, Version: item.Version, Digest: item.Digest, SourceRef: item.SourceRef, SignatureRef: item.SignatureRef, SigningKeyID: item.SigningKeyID, Providers: item.CompatibleProviders, Status: item.Status, ResourceVersion: item.ResourceVersion, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, RevokedAt: item.RevokedAt}))
	}
	if len(page.Bundles) > limit {
		page.Bundles = page.Bundles[:limit]
		page.NextBundleID = page.Bundles[len(page.Bundles)-1].Metadata.UID
	}
	return page, nil
}

func scanMcpServer(row rowScanner, result *capabilityRow) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.ID, &result.Version, &result.Digest, &result.Transport, &result.ConnectionRef, &result.CredentialRef, &result.NetworkPolicyRef, &result.Permissions, &result.Status, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt, &result.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCapabilityNotFound
		}
		return err
	}
	return validateCapabilityRow(*result, true)
}
func scanSkillBundle(row rowScanner, result *capabilityRow) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.ID, &result.Version, &result.Digest, &result.SourceRef, &result.SignatureRef, &result.SigningKeyID, &result.Providers, &result.Status, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt, &result.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCapabilityNotFound
		}
		return err
	}
	return validateCapabilityRow(*result, false)
}

func validateCapabilityRow(row capabilityRow, mcp bool) error {
	if !validMutationIdentifier(row.ID) || !validMutationIdentifier(row.Version) || !validCoordinationDigest(row.Digest) || row.ResourceVersion < 1 || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() || row.Status != "active" && row.Status != "revoked" || row.Status == "active" && row.RevokedAt != nil || row.Status == "revoked" && row.RevokedAt == nil {
		return ErrCoordinationResultDrift
	}
	if mcp {
		if row.Transport != "stdio" && row.Transport != "sse" && row.Transport != "streamable-http" || !validMutationIdentifier(row.ConnectionRef) || !validMutationIdentifier(row.CredentialRef) || !validMutationIdentifier(row.NetworkPolicyRef) || len(row.Permissions) < 1 {
			return ErrCoordinationResultDrift
		}
	} else if !validMutationIdentifier(row.SourceRef) || !validMutationIdentifier(row.SignatureRef) || !validMutationIdentifier(row.SigningKeyID) || len(row.Providers) < 1 {
		return ErrCoordinationResultDrift
	}
	return nil
}

func resourceMetadata(tenantID string, row capabilityRow) common.ResourceMetadata {
	return common.ResourceMetadata{UID: row.ID, Name: row.ID, TenantRef: common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: tenantID}, ResourceVersion: strconv.FormatInt(row.ResourceVersion, 10), CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}
func mcpServerResource(tenantID, projectID string, row capabilityRow) platform.McpServer {
	revoked := ""
	if row.RevokedAt != nil {
		revoked = row.RevokedAt.UTC().Format(time.RFC3339Nano)
	}
	return platform.McpServer{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "McpServer", Metadata: resourceMetadata(tenantID, row)}, Spec: platform.McpServerSpec{ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID}, Version: row.Version, Digest: row.Digest, Transport: row.Transport, ConnectionRef: row.ConnectionRef, CredentialRef: row.CredentialRef, NetworkPolicyRef: row.NetworkPolicyRef, Permissions: append([]string(nil), row.Permissions...), Status: row.Status, RevokedAt: revoked}}
}
func skillBundleResource(tenantID, projectID string, row capabilityRow) platform.SkillBundle {
	revoked := ""
	if row.RevokedAt != nil {
		revoked = row.RevokedAt.UTC().Format(time.RFC3339Nano)
	}
	return platform.SkillBundle{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "SkillBundle", Metadata: resourceMetadata(tenantID, row)}, Spec: platform.SkillBundleSpec{ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID}, Version: row.Version, Digest: row.Digest, SourceRef: row.SourceRef, SignatureRef: row.SignatureRef, SigningKeyID: row.SigningKeyID, CompatibleProviders: append([]string(nil), row.Providers...), MountReadOnly: true, Status: row.Status, RevokedAt: revoked}}
}
func capabilityDigest(operation string, body []byte) string {
	sum := sha256.Sum256(append([]byte("cloud-agents/capability/"+operation+"\x00"), body...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validCapabilityCreateMcp(input platform.McpServerCreateRequest) bool {
	if !validMutationIdentifier(input.ServerID) || !validMutationIdentifier(input.Version) || !validCoordinationDigest(input.Digest) || input.Transport != "stdio" && input.Transport != "sse" && input.Transport != "streamable-http" || !validMutationIdentifier(input.ConnectionRef) || !validMutationIdentifier(input.CredentialRef) || !validMutationIdentifier(input.NetworkPolicyRef) || len(input.Permissions) < 1 || len(input.Permissions) > 64 {
		return false
	}
	seen := make(map[string]struct{}, len(input.Permissions))
	for _, permission := range input.Permissions {
		if !validCapabilityPermission(permission) {
			return false
		}
		if _, ok := seen[permission]; ok {
			return false
		}
		seen[permission] = struct{}{}
	}
	return true
}

func validCapabilityCreateSkill(input platform.SkillBundleCreateRequest) bool {
	if !validMutationIdentifier(input.BundleID) || !validMutationIdentifier(input.Version) || !validCoordinationDigest(input.Digest) || !validMutationIdentifier(input.SourceRef) || !validMutationIdentifier(input.SignatureRef) || !validMutationIdentifier(input.SigningKeyID) || len(input.CompatibleProviders) < 1 || len(input.CompatibleProviders) > 4 {
		return false
	}
	seen := make(map[string]struct{}, len(input.CompatibleProviders))
	for _, provider := range input.CompatibleProviders {
		if provider != "codex" && provider != "claude-code" && provider != "pi" && provider != "deepseek-harness" {
			return false
		}
		if _, ok := seen[provider]; ok {
			return false
		}
		seen[provider] = struct{}{}
	}
	return true
}

func validCapabilityPermission(value string) bool {
	if len(value) == 0 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if character != '.' && character != '_' && character != ':' && character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func mapCapabilityError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, authz.ErrOperationDenied) {
		return ErrMutationDenied
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return ErrCapabilityNotFound
		case "23505":
			return ErrCapabilityIdempotencyConflict
		case "40001":
			return ErrCapabilityResourceVersionConflict
		case "22023", "22003", "23514":
			return ErrCoordinationInvalidInput
		}
		return fmt.Errorf("capability database operation: %w", err)
	}
	return err
}
