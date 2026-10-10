package authz

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubjectRefCanonicalFixtureAndExactIdentity(t *testing.T) {
	var fixture struct {
		Instance struct {
			Kind    string `json:"kind"`
			Issuer  string `json:"issuer"`
			Subject string `json:"subject"`
		} `json:"instance"`
		CanonicalUTF8 string `json:"canonicalUtf8"`
		Digest        string `json:"digest"`
	}
	readFixture(t, "contracts/common/v1alpha1/fixtures/golden/subject-ref-canonical.json", &fixture)
	subject := SubjectRef{
		Kind:    fixture.Instance.Kind,
		Issuer:  fixture.Instance.Issuer,
		Subject: fixture.Instance.Subject,
	}
	canonical, err := subject.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(canonical) != fixture.CanonicalUTF8 {
		t.Fatalf("canonical subject = %q, want %q", canonical, fixture.CanonicalUTF8)
	}
	digest, err := subject.Digest()
	if err != nil || digest != fixture.Digest {
		t.Fatalf("subject digest = %q err=%v, want %q", digest, err, fixture.Digest)
	}
	caseChanged := subject
	caseChanged.Issuer = "https://issuer.example/%7Etenant"
	caseDigest, err := caseChanged.Digest()
	if err != nil || caseDigest == digest {
		t.Fatalf("issuer case change did not change digest: %q err=%v", caseDigest, err)
	}
}

func TestSubjectRefCanonicalStringEscaping(t *testing.T) {
	subject := SubjectRef{
		Kind:    "user",
		Issuer:  "https://identity.example.test/",
		Subject: "a\x00\b\t\n\f\r\x1f\"\\中",
	}
	canonical, err := subject.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"issuer":"https://identity.example.test/","kind":"user","subject":"a\u0000\b\t\n\f\r\u001f\"\\中"}`
	if string(canonical) != want {
		t.Fatalf("canonical subject = %q, want %q", canonical, want)
	}
}

func TestSubjectRefIssuerUsesClosedAbsoluteURIProfile(t *testing.T) {
	tests := []struct {
		name   string
		issuer string
		valid  bool
	}{
		{name: "https", issuer: "https://identity.example.test/%7etenant", valid: true},
		{name: "urn", issuer: "urn:cloud-agents:tenant-alpha", valid: true},
		{name: "missing scheme", issuer: "identity.example.test", valid: false},
		{name: "leading digit scheme", issuer: "1https://identity.example.test/", valid: false},
		{name: "invalid percent escape", issuer: "https://identity.example.test/%zz", valid: false},
		{name: "short percent escape", issuer: "https://identity.example.test/%a", valid: false},
		{name: "newline", issuer: "https://identity.example.test/\ncontrol", valid: false},
		{name: "delete", issuer: "https://identity.example.test/\x7fcontrol", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			subject := SubjectRef{Kind: "user", Issuer: test.issuer, Subject: "user-alpha"}
			err := subject.Validate()
			if test.valid && err != nil {
				t.Fatalf("valid issuer rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid issuer accepted")
			}
		})
	}
}

func TestBuiltinCatalogFixtureAndDrift(t *testing.T) {
	catalog := builtinCatalogFixture(t)
	if err := catalog.Validate(); err != nil {
		t.Fatalf("valid catalog rejected: %v", err)
	}

	faults := []struct {
		name   string
		mutate func(*Catalog)
	}{
		{name: "role order", mutate: func(value *Catalog) {
			value.Roles[0], value.Roles[1] = value.Roles[1], value.Roles[0]
		}},
		{name: "permission expansion", mutate: func(value *Catalog) {
			value.Roles[5].Permissions = append(value.Roles[5].Permissions, "projects.update")
		}},
		{name: "scope", mutate: func(value *Catalog) {
			value.Roles[5].ScopeLevel = ScopeOrganization
		}},
		{name: "version", mutate: func(value *Catalog) {
			value.Roles[5].Version++
		}},
		{name: "publication", mutate: func(value *Catalog) {
			value.Roles[5].PublishedAt = "2026-08-17T00:00:01Z"
		}},
	}
	for _, fault := range faults {
		t.Run(fault.name, func(t *testing.T) {
			drifted := cloneCatalog(catalog)
			fault.mutate(&drifted)
			if err := drifted.Validate(); !errors.Is(err, ErrCatalogDrift) {
				t.Fatalf("catalog drift error = %v, want ErrCatalogDrift", err)
			}
		})
	}
}

func TestBuiltinCatalogV2PreservesV1AndClosesPlatformAdminPermissions(t *testing.T) {
	if err := builtinCatalogFixture(t).Validate(); err != nil {
		t.Fatalf("frozen v1 catalog rejected: %v", err)
	}
	catalog := builtinCatalogV2Fixture(t)
	if err := catalog.Validate(); err != nil {
		t.Fatalf("valid v2 catalog rejected: %v", err)
	}
	role, ok := catalog.Role("platform.admin", 2)
	if !ok {
		t.Fatal("platform.admin v2 missing")
	}
	for _, forbidden := range []string{"sessions.get", "turns.get", "artifacts.get", "credentials.get"} {
		if containsPermission(role.Permissions, forbidden) {
			t.Fatalf("platform.admin v2 contains content permission %q", forbidden)
		}
	}
	if !containsPermission(role.Permissions, "projects.act") || !containsPermission(role.Permissions, "operations.list") || !containsPermission(role.Permissions, "projects.update") || !containsPermission(role.Permissions, "tenants.update") {
		t.Fatalf("platform.admin v2 permissions = %v", role.Permissions)
	}

	drifted := cloneCatalog(catalog)
	drifted.Roles[1].Permissions = append(drifted.Roles[1].Permissions, "tenants.watch")
	if err := drifted.Validate(); !errors.Is(err, ErrCatalogDrift) {
		t.Fatalf("expanded catalog error = %v, want ErrCatalogDrift", err)
	}
	drifted = cloneCatalog(catalog)
	drifted.Roles[0].CatalogRevision = 2
	drifted.Roles[0].PublishedAt = builtinCatalogV2PublishedAt
	if err := drifted.Validate(); !errors.Is(err, ErrCatalogDrift) {
		t.Fatalf("rewritten inherited role error = %v, want ErrCatalogDrift", err)
	}
}

func TestPlatformAdminV2UsesExplicitAdminApplicationPolicy(t *testing.T) {
	now := time.Date(2026, time.October, 8, 1, 2, 3, 0, time.UTC)
	subject := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	project := ScopePath{Level: ScopeProject, TenantID: "tenant-alpha", OrganizationID: "organization-alpha", ProjectID: "project-alpha"}
	binding := &GlobalRoleBindingFact{
		UserID: "alpha", Subject: subject, SubjectHash: digest,
		RoleName: "platform.admin", RoleVersion: 2, State: BindingActive,
	}
	snapshot := Snapshot{
		TenantID: "tenant-alpha", Application: "admin", Scope: project, ScopeResolved: true,
		Catalog: builtinCatalogV2Fixture(t), GlobalBinding: binding,
	}
	request := authorizationRequest{Subject: subject, Permission: "projects.update", Resource: ScopeRef{Level: ScopeProject, ID: "project-alpha"}}
	decision, err := evaluate(snapshot, request, now)
	if err != nil || !decision.Allowed || decision.evidence == nil || decision.evidence.RoleName != "platform.admin" {
		t.Fatalf("platform admin decision = %#v err=%v", decision, err)
	}

	for _, test := range []struct {
		name   string
		mutate func(*Snapshot, *authorizationRequest)
		err    error
	}{
		{name: "user application", mutate: func(value *Snapshot, _ *authorizationRequest) { value.Application = "user" }, err: nil},
		{name: "unknown content permission", mutate: func(_ *Snapshot, value *authorizationRequest) { value.Permission = "sessions.get" }, err: nil},
		{name: "revoked global binding", mutate: func(value *Snapshot, _ *authorizationRequest) { value.GlobalBinding.State = BindingRevoked }, err: nil},
		{name: "v1 catalog", mutate: func(value *Snapshot, _ *authorizationRequest) { value.Catalog = builtinCatalogFixture(t) }, err: nil},
		{name: "missing application", mutate: func(value *Snapshot, _ *authorizationRequest) { value.Application = "" }, err: ErrSnapshotMalformed},
		{name: "subject digest mismatch", mutate: func(value *Snapshot, _ *authorizationRequest) {
			value.GlobalBinding.SubjectHash = "sha256:" + stringsOf("0", 64)
		}, err: ErrSnapshotMalformed},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := cloneSnapshot(snapshot)
			requestCopy := request
			test.mutate(&value, &requestCopy)
			got, gotErr := evaluate(value, requestCopy, now)
			if test.err != nil {
				if !errors.Is(gotErr, test.err) {
					t.Fatalf("error = %v, want %v", gotErr, test.err)
				}
				return
			}
			if gotErr != nil || got.Allowed || got.Reason != denyNoEligibleBinding {
				t.Fatalf("decision = %#v err=%v, want closed deny", got, gotErr)
			}
		})
	}
}

func TestEvaluateTokenScopesUsesOperationEvaluator(t *testing.T) {
	now := time.Date(2026, time.October, 8, 1, 2, 3, 0, time.UTC)
	subject := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	project := ScopePath{Level: ScopeProject, TenantID: "tenant-alpha", OrganizationID: "organization-alpha", ProjectID: "project-alpha"}
	snapshot := Snapshot{
		TenantID: "tenant-alpha", Application: "admin", Scope: project, ScopeResolved: true,
		Catalog: builtinCatalogV2Fixture(t),
		GlobalBinding: &GlobalRoleBindingFact{
			UserID: "alpha", Subject: subject, SubjectHash: digest,
			RoleName: "platform.admin", RoleVersion: 2, State: BindingActive,
		},
	}
	scopes, err := EvaluateTokenScopes(snapshot, subject, now)
	if err != nil {
		t.Fatal(err)
	}
	for index, scope := range scopes {
		if index > 0 && scopes[index-1] >= scope {
			t.Fatalf("scopes are not strictly sorted: %v", scopes)
		}
	}
	for _, required := range []string{"audit.list", "mcp-servers.create", "network-policies.update", "operations.list", "profiles.list", "projects.act", "projects.get", "remote-worker-enrollments.create", "sandboxes.act", "snapshots.create", "storage-policies.update", "targets.create", "workers.list"} {
		if !containsPermission(scopes, required) {
			t.Fatalf("scopes %v missing %q", scopes, required)
		}
	}
	if len(scopes) != 46 {
		t.Fatalf("admin project-token scope count = %d, want 46: %v", len(scopes), scopes)
	}
	for _, forbidden := range []string{"memberships.create", "tenants.update", "environment-profiles.list", "remote-worker-bootstrap.act", "sessions.get", "credentials.get"} {
		if containsPermission(scopes, forbidden) {
			t.Fatalf("scopes %v contain %q", scopes, forbidden)
		}
	}
	snapshot.Application = "user"
	if _, err := EvaluateTokenScopes(snapshot, subject, now); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("user application scope error = %v, want ErrOperationDenied", err)
	}

	member := allowedSnapshot(t, subject, "tenant-alpha", "project-alpha")
	member.Application = "user"
	member.Scope = ScopePath{Level: ScopeTenant, TenantID: "tenant-alpha"}
	memberScopes, err := EvaluateTokenScopes(member, subject, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(memberScopes) != 3 || !containsPermission(memberScopes, "projects.get") || !containsPermission(memberScopes, "projects.list") || !containsPermission(memberScopes, "projects.watch") {
		t.Fatalf("project member tenant-token scopes = %v", memberScopes)
	}
	for _, forbidden := range []string{"environment-profiles.list", "environments.get", "sandboxes.update"} {
		if containsPermission(memberScopes, forbidden) {
			t.Fatalf("tenant-token scopes %v contain project permission %q", memberScopes, forbidden)
		}
	}
	member.Scope = project
	projectScopes, err := EvaluateTokenScopes(member, subject, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"environment-profiles.list", "environment-quotas.get", "environments.get", "projects.get"} {
		if !containsPermission(projectScopes, required) {
			t.Fatalf("user project-token scopes %v missing %q", projectScopes, required)
		}
	}
	for _, forbidden := range []string{"environments.create", "remote-worker-bootstrap.act", "sandboxes.update", "sessions.get"} {
		if containsPermission(projectScopes, forbidden) {
			t.Fatalf("user project-token scopes %v contain %q", projectScopes, forbidden)
		}
	}
	member.Candidates[0].Binding.RoleName = "project.operator"
	operatorScopes, err := EvaluateTokenScopes(member, subject, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"environments.create", "environments.delete", "projects.act", "sandboxes.update"} {
		if !containsPermission(operatorScopes, required) {
			t.Fatalf("operator project-token scopes %v missing %q", operatorScopes, required)
		}
	}
	member.Scope = ScopePath{Level: ScopeTenant, TenantID: "tenant-alpha"}
	member.Application = ""
	if _, err := EvaluateTokenScopes(member, subject, now); !errors.Is(err, ErrSnapshotMalformed) {
		t.Fatalf("missing application error = %v, want ErrSnapshotMalformed", err)
	}

	member = allowedSnapshot(t, subject, "tenant-alpha", "project-b")
	member.Application = "user"
	member.Scope = ScopePath{Level: ScopeProject, TenantID: "tenant-alpha", OrganizationID: "organization-alpha", ProjectID: "project-a"}
	if _, err := EvaluateTokenScopes(member, subject, now); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("other-project scope error = %v, want ErrOperationDenied", err)
	}

	member.Application = "admin"
	member.Catalog = builtinCatalogV2Fixture(t)
	if _, err := EvaluateTokenScopes(member, subject, now); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("non-tenant admin scope error = %v, want ErrOperationDenied", err)
	}
	serviceAccount := SubjectRef{Kind: "serviceAccount", Issuer: subject.Issuer, Subject: "service-project-viewer"}
	serviceAccountProject := allowedSnapshot(t, serviceAccount, "tenant-alpha", "project-alpha")
	serviceAccountProject.Application = "admin"
	serviceAccountProject.Catalog = builtinCatalogV2Fixture(t)
	serviceAccountScopes, err := EvaluateTokenScopes(serviceAccountProject, serviceAccount, now)
	if err != nil {
		t.Fatal(err)
	}
	if !containsPermission(serviceAccountScopes, "projects.get") || containsPermission(serviceAccountScopes, "projects.act") || containsPermission(serviceAccountScopes, "tenants.get") {
		t.Fatalf("Admin project-viewer service-account scopes = %v", serviceAccountScopes)
	}
	serviceAccountProject.Scope.ProjectID = "project-other"
	if _, err := EvaluateTokenScopes(serviceAccountProject, serviceAccount, now); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("other-project service-account scope error = %v, want ErrOperationDenied", err)
	}
	member.Scope = ScopePath{Level: ScopeTenant, TenantID: "tenant-alpha"}
	member.Candidates[0].Membership.Scope = member.Scope
	member.Candidates[0].Binding.Scope = member.Scope
	member.Candidates[0].Binding.RoleName = "tenant.admin"
	adminScopes, err := EvaluateTokenScopes(member, subject, now)
	if err != nil || !containsPermission(adminScopes, "tenants.get") || !containsPermission(adminScopes, "projects.act") {
		t.Fatalf("tenant admin scopes = %v err=%v", adminScopes, err)
	}
	for _, forbidden := range []string{"audit.list", "profiles.list", "remote-worker-bootstrap.act", "sessions.get"} {
		if containsPermission(adminScopes, forbidden) {
			t.Fatalf("tenant admin tenant-token scopes %v contain %q", adminScopes, forbidden)
		}
	}
	member.Application = "user"
	member.Scope = project
	userTenantAdminProjectScopes, err := EvaluateTokenScopes(member, subject, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(userTenantAdminProjectScopes) != 31 {
		t.Fatalf("user tenant-admin project-token scope count = %d, want 31: %v", len(userTenantAdminProjectScopes), userTenantAdminProjectScopes)
	}
	for _, forbidden := range []string{"organizations.create", "remote-worker-bootstrap.act", "tenants.update"} {
		if containsPermission(userTenantAdminProjectScopes, forbidden) {
			t.Fatalf("user tenant-admin project-token scopes %v contain %q", userTenantAdminProjectScopes, forbidden)
		}
	}
}

func TestEvaluateDefaultDenyAndScopeContainment(t *testing.T) {
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	tenantID := "tenant-alpha"
	subject := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	request := authorizationRequest{
		Subject:    subject,
		Permission: "projects.get",
		Resource:   ScopeRef{Level: ScopeProject, ID: "project-alpha"},
	}
	projectPath := ScopePath{
		Level:          ScopeProject,
		TenantID:       tenantID,
		OrganizationID: "organization-alpha",
		ProjectID:      "project-alpha",
	}
	organizationPath := ScopePath{
		Level:          ScopeOrganization,
		TenantID:       tenantID,
		OrganizationID: "organization-alpha",
	}
	candidate := Candidate{
		Membership: MembershipFact{
			UID:         "membership-alpha",
			Subject:     subject,
			SubjectHash: digest,
			Scope:       organizationPath,
			State:       MembershipActive,
		},
		Binding: RoleBindingFact{
			UID:         "role-binding-alpha",
			Subject:     subject,
			SubjectHash: digest,
			RoleName:    "project.viewer",
			RoleVersion: 1,
			Scope:       projectPath,
			State:       BindingActive,
		},
	}
	base := Snapshot{
		TenantID:      tenantID,
		Scope:         projectPath,
		ScopeResolved: true,
		Catalog:       builtinCatalogFixture(t),
		Candidates:    []Candidate{candidate},
	}
	decision, err := evaluate(base, request, now)
	if err != nil || !decision.Allowed || decision.evidence == nil || decision.evidence.RoleBindingUID != "role-binding-alpha" {
		t.Fatalf("allow decision = %#v err=%v", decision, err)
	}

	tests := []struct {
		name   string
		mutate func(*Snapshot, *authorizationRequest)
		reason denyReason
	}{
		{name: "missing candidates", mutate: func(snapshot *Snapshot, _ *authorizationRequest) {
			snapshot.Candidates = nil
		}, reason: denyNoEligibleBinding},
		{name: "suspended membership", mutate: func(snapshot *Snapshot, _ *authorizationRequest) {
			snapshot.Candidates[0].Membership.State = MembershipSuspended
		}, reason: denyNoEligibleBinding},
		{name: "revoked binding", mutate: func(snapshot *Snapshot, _ *authorizationRequest) {
			snapshot.Candidates[0].Binding.State = BindingRevoked
		}, reason: denyNoEligibleBinding},
		{name: "expiry equality", mutate: func(snapshot *Snapshot, _ *authorizationRequest) {
			snapshot.Candidates[0].Binding.ExpiresAt = cloneTime(now)
		}, reason: denyNoEligibleBinding},
		{name: "membership narrower than binding", mutate: func(snapshot *Snapshot, _ *authorizationRequest) {
			snapshot.Candidates[0].Membership.Scope = ScopePath{Level: ScopeProject, TenantID: tenantID, OrganizationID: "organization-other", ProjectID: "project-alpha"}
		}, reason: denyNoEligibleBinding},
		{name: "binding outside request", mutate: func(snapshot *Snapshot, _ *authorizationRequest) {
			snapshot.Candidates[0].Binding.Scope = ScopePath{Level: ScopeProject, TenantID: tenantID, OrganizationID: "organization-other", ProjectID: "project-alpha"}
		}, reason: denyNoEligibleBinding},
		{name: "platform role is not tenant runtime authority", mutate: func(snapshot *Snapshot, _ *authorizationRequest) {
			snapshot.Candidates[0].Membership.Scope = ScopePath{Level: ScopePlatform}
			snapshot.Candidates[0].Binding.RoleName = "platform.admin"
			snapshot.Candidates[0].Binding.Scope = ScopePath{Level: ScopePlatform}
		}, reason: denyNoEligibleBinding},
		{name: "unregistered permission", mutate: func(_ *Snapshot, request *authorizationRequest) {
			request.Permission = "projects.future"
		}, reason: denyNoEligibleBinding},
		{name: "unresolved scope", mutate: func(snapshot *Snapshot, _ *authorizationRequest) {
			snapshot.ScopeResolved = false
		}, reason: denyUnknownScope},
		{name: "platform runtime", mutate: func(_ *Snapshot, request *authorizationRequest) {
			request.Resource = ScopeRef{Level: ScopePlatform}
		}, reason: denyPlatformRuntime},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := cloneSnapshot(base)
			requestCopy := request
			test.mutate(&snapshot, &requestCopy)
			got, gotErr := evaluate(snapshot, requestCopy, now)
			if gotErr != nil || got.Allowed || got.Reason != test.reason || got.evidence != nil {
				t.Fatalf("decision = %#v err=%v, want deny %s", got, gotErr, test.reason)
			}
		})
	}
}

func TestEvaluateActiveNarrowMembershipSurvivesInactiveBroadMembership(t *testing.T) {
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	tenantID := "tenant-alpha"
	subject := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	project := ScopePath{Level: ScopeProject, TenantID: tenantID, OrganizationID: "organization-alpha", ProjectID: "project-alpha"}
	binding := RoleBindingFact{UID: "role-binding-alpha", Subject: subject, SubjectHash: digest, RoleName: "project.viewer", RoleVersion: 1, Scope: project, State: BindingActive}
	snapshot := Snapshot{
		TenantID: tenantID, Scope: project, ScopeResolved: true, Catalog: builtinCatalogFixture(t),
		Candidates: []Candidate{
			{Membership: MembershipFact{UID: "membership-broad", Subject: subject, SubjectHash: digest, Scope: ScopePath{Level: ScopeTenant, TenantID: tenantID}, State: MembershipSuspended}, Binding: binding},
			{Membership: MembershipFact{UID: "membership-narrow", Subject: subject, SubjectHash: digest, Scope: project, State: MembershipActive}, Binding: binding},
		},
	}
	request := authorizationRequest{Subject: subject, Permission: "projects.get", Resource: ScopeRef{Level: ScopeProject, ID: "project-alpha"}}
	decision, err := evaluate(snapshot, request, now)
	if err != nil || !decision.Allowed || decision.evidence == nil || decision.evidence.MembershipUID != "membership-narrow" {
		t.Fatalf("narrow allow = %#v err=%v", decision, err)
	}
}

func TestEvaluateIntegrityFaultsReturnErrors(t *testing.T) {
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	tenantID := "tenant-alpha"
	subject := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	project := ScopePath{Level: ScopeProject, TenantID: tenantID, OrganizationID: "organization-alpha", ProjectID: "project-alpha"}
	candidate := Candidate{
		Membership: MembershipFact{UID: "membership-alpha", Subject: subject, SubjectHash: digest, Scope: project, State: MembershipActive},
		Binding:    RoleBindingFact{UID: "role-binding-alpha", Subject: subject, SubjectHash: digest, RoleName: "project.viewer", RoleVersion: 1, Scope: project, State: BindingActive},
	}
	base := Snapshot{TenantID: tenantID, Scope: project, ScopeResolved: true, Catalog: builtinCatalogFixture(t), Candidates: []Candidate{candidate}}
	request := authorizationRequest{Subject: subject, Permission: "projects.get", Resource: ScopeRef{Level: ScopeProject, ID: "project-alpha"}}

	tests := []struct {
		name   string
		mutate func(*Snapshot)
		target error
	}{
		{name: "catalog", mutate: func(snapshot *Snapshot) {
			snapshot.Catalog.Roles[5].Permissions = append(snapshot.Catalog.Roles[5].Permissions, "projects.update")
		}, target: ErrCatalogDrift},
		{name: "subject digest", mutate: func(snapshot *Snapshot) {
			snapshot.Candidates[0].Binding.SubjectHash = "sha256:" + stringsOf("0", 64)
		}, target: ErrSnapshotMalformed},
		{name: "cross tenant scope", mutate: func(snapshot *Snapshot) {
			snapshot.Candidates[0].Membership.Scope.TenantID = "tenant-other"
		}, target: ErrSnapshotMalformed},
		{name: "duplicate candidate", mutate: func(snapshot *Snapshot) {
			snapshot.Candidates = append(snapshot.Candidates, snapshot.Candidates[0])
		}, target: ErrSnapshotMalformed},
		{name: "resolved request scope mismatch", mutate: func(snapshot *Snapshot) {
			snapshot.Scope.ProjectID = "project-other"
		}, target: ErrSnapshotMalformed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := cloneSnapshot(base)
			test.mutate(&snapshot)
			decision, gotErr := evaluate(snapshot, request, now)
			if decision.Allowed || !errors.Is(gotErr, test.target) {
				t.Fatalf("decision=%#v err=%v, want %v", decision, gotErr, test.target)
			}
		})
	}
}

func TestVerifiedOperationBindExecuteOneShotAndDefaultDeny(t *testing.T) {
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	actor := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	resource := ScopeRef{Level: ScopeProject, ID: "project-alpha"}
	binder := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	operation, err := binder.Bind("tenant-alpha", resource, "projects.get")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := operation.Actor(); !ok || got != actor {
		t.Fatalf("operation actor = %#v ok=%v", got, ok)
	}
	snapshot := allowedSnapshot(t, actor, "tenant-alpha", "project-alpha")
	called := 0
	if err := operation.Execute(snapshot, now, func() error { called++; return nil }); err != nil || called != 1 {
		t.Fatalf("execute err=%v called=%d", err, called)
	}
	if _, ok := operation.Actor(); ok {
		t.Fatal("spent operation retained actor authority")
	}
	if err := operation.Execute(snapshot, now, func() error { called++; return nil }); !errors.Is(err, ErrOperationDenied) || called != 1 {
		t.Fatalf("second execute err=%v called=%d", err, called)
	}

	deniedBinder := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	deniedOperation, err := deniedBinder.Bind("tenant-alpha", resource, "projects.get")
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Candidates = nil
	if err := deniedOperation.Execute(snapshot, now, func() error { called++; return nil }); !errors.Is(err, ErrOperationDenied) || called != 1 {
		t.Fatalf("deny err=%v called=%d", err, called)
	}
}

func TestVerifiedOperationProtectedCallbackErrorPreservesExecutedProgress(t *testing.T) {
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	actor := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	resource := ScopeRef{Level: ScopeProject, ID: "project-alpha"}
	protectedErr := errors.New("protected operation rejected")
	progress := &atomic.Uint32{}
	binder := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	binder.progress = progress
	operation, err := binder.Bind("tenant-alpha", resource, "projects.get")
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.Execute(allowedSnapshot(t, actor, "tenant-alpha", "project-alpha"), now, func() error {
		return protectedErr
	}); !errors.Is(err, protectedErr) {
		t.Fatalf("protected callback error = %v", err)
	}
	if got := progress.Load(); got != operationProgressExecuted {
		t.Fatalf("protected callback progress = %d, want %d", got, operationProgressExecuted)
	}
}

func TestVerifiedOperationProjectSelectorUsesExistingEvaluator(t *testing.T) {
	now := time.Date(2026, time.October, 8, 1, 2, 3, 0, time.UTC)
	actor := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	tenant := ScopeRef{Level: ScopeTenant, ID: "tenant-alpha"}
	operation, err := testVerifiedOperationBinder(actor, "tenant-alpha", tenant, "projects.list").Bind("tenant-alpha", tenant, "projects.list")
	if err != nil {
		t.Fatal(err)
	}
	base := allowedSnapshot(t, actor, "tenant-alpha", "project-alpha")
	base.Application = "user"
	base.Scope = ScopePath{Level: ScopeTenant, TenantID: "tenant-alpha"}
	projectAlpha := ScopePath{Level: ScopeProject, TenantID: "tenant-alpha", OrganizationID: "organization-alpha", ProjectID: "project-alpha"}
	projectBeta := ScopePath{Level: ScopeProject, TenantID: "tenant-alpha", OrganizationID: "organization-beta", ProjectID: "project-beta"}
	batches := [][]ScopePath{{projectAlpha}, {projectBeta}}
	var bitmaps [][]bool
	if err := operation.ExecuteProjectSelector(base, now, func() ([]ScopePath, bool, error) {
		batch := batches[0]
		batches = batches[1:]
		return batch, len(batches) == 0, nil
	}, func(allowed []bool) (bool, error) {
		bitmaps = append(bitmaps, append([]bool(nil), allowed...))
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(bitmaps) != 2 || len(bitmaps[0]) != 1 || !bitmaps[0][0] || len(bitmaps[1]) != 1 || bitmaps[1][0] {
		t.Fatalf("selector bitmaps = %#v", bitmaps)
	}
	if err := operation.ExecuteProjectSelector(base, now, nil, nil); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("repeat selector error = %v", err)
	}
}

func TestVerifiedOperationProjectSelectorRejectsAuthorityMismatch(t *testing.T) {
	now := time.Date(2026, time.October, 8, 1, 2, 3, 0, time.UTC)
	actor := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	tenant := ScopeRef{Level: ScopeTenant, ID: "tenant-alpha"}
	project := ScopePath{Level: ScopeProject, TenantID: "tenant-alpha", OrganizationID: "organization-alpha", ProjectID: "project-alpha"}
	base := allowedSnapshot(t, actor, "tenant-alpha", "project-alpha")
	base.Application = "user"
	base.Scope = ScopePath{Level: ScopeTenant, TenantID: "tenant-alpha"}
	for _, test := range []struct {
		name       string
		permission string
		mutate     func(*Snapshot, *[]ScopePath)
	}{
		{name: "wrong application", permission: "projects.list", mutate: func(snapshot *Snapshot, _ *[]ScopePath) { snapshot.Application = "admin" }},
		{name: "wrong tenant", permission: "projects.list", mutate: func(_ *Snapshot, scopes *[]ScopePath) { (*scopes)[0].TenantID = "tenant-other" }},
		{name: "wrong permission", permission: "projects.get", mutate: func(*Snapshot, *[]ScopePath) {}},
		{name: "inactive parent scope", permission: "projects.list", mutate: func(_ *Snapshot, scopes *[]ScopePath) { (*scopes)[0].OrganizationID = "" }},
		{name: "mixed scope levels", permission: "projects.list", mutate: func(_ *Snapshot, scopes *[]ScopePath) {
			*scopes = append(*scopes, ScopePath{Level: ScopeOrganization, TenantID: "tenant-alpha", OrganizationID: "organization-alpha"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := cloneSnapshot(base)
			scopes := []ScopePath{project}
			test.mutate(&snapshot, &scopes)
			operation, err := testVerifiedOperationBinder(actor, "tenant-alpha", tenant, test.permission).Bind("tenant-alpha", tenant, test.permission)
			if err != nil {
				t.Fatal(err)
			}
			called := false
			err = operation.ExecuteProjectSelector(snapshot, now, func() ([]ScopePath, bool, error) { return scopes, true, nil }, func([]bool) (bool, error) { called = true; return false, nil })
			if !errors.Is(err, ErrOperationDenied) || called {
				t.Fatalf("error=%v callback=%v", err, called)
			}
		})
	}

	escaped, err := testVerifiedOperationBinder(actor, "tenant-alpha", tenant, "projects.list").Bind("tenant-alpha", tenant, "projects.list")
	if err != nil {
		t.Fatal(err)
	}
	escaped.lifetime.close()
	if err := escaped.ExecuteProjectSelector(base, now, func() ([]ScopePath, bool, error) { return []ScopePath{project}, true, nil }, func([]bool) (bool, error) { return false, nil }); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("escaped selector error = %v", err)
	}
}

func TestVerifiedOperationCopyTamperMismatchAndEscapeFailClosed(t *testing.T) {
	actor := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	resource := ScopeRef{Level: ScopeProject, ID: "project-alpha"}

	mismatch := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	if _, err := mismatch.Bind("tenant-other", resource, "projects.get"); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("mismatch bind err=%v", err)
	}
	if _, err := mismatch.Bind("tenant-alpha", resource, "projects.get"); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("mismatch retry err=%v", err)
	}

	copySource := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	copyValue := *copySource
	if _, err := copyValue.Bind("tenant-alpha", resource, "projects.get"); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("binder copy err=%v", err)
	}
	if _, err := copySource.Bind("tenant-alpha", resource, "projects.get"); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("binder source after copy err=%v", err)
	}

	tampered := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	tampered.permission = "projects.update"
	if _, err := tampered.Bind("tenant-alpha", resource, "projects.update"); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("tampered binder err=%v", err)
	}

	operationBinder := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	operation, err := operationBinder.Bind("tenant-alpha", resource, "projects.get")
	if err != nil {
		t.Fatal(err)
	}
	operationCopy := *operation
	if err := operationCopy.Execute(Snapshot{}, time.Now(), func() error { return nil }); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("operation copy err=%v", err)
	}
	if err := operation.Execute(Snapshot{}, time.Now(), func() error { return nil }); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("operation source after copy err=%v", err)
	}

	tamperedOperationBinder := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	tamperedOperation, err := tamperedOperationBinder.Bind("tenant-alpha", resource, "projects.get")
	if err != nil {
		t.Fatal(err)
	}
	tamperedOperation.actor.Subject = "attacker"
	if err := tamperedOperation.Execute(Snapshot{}, time.Now(), func() error { return nil }); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("tampered operation err=%v", err)
	}

	escapedBinder := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	escapedBinder.lifetime.close()
	if _, err := escapedBinder.Bind("tenant-alpha", resource, "projects.get"); !errors.Is(err, ErrOperationDenied) {
		t.Fatalf("escaped binder err=%v", err)
	}
}

func TestVerifiedOperationConcurrentBindAndExecuteHaveOneWinner(t *testing.T) {
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	actor := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	resource := ScopeRef{Level: ScopeProject, ID: "project-alpha"}
	binder := testVerifiedOperationBinder(actor, "tenant-alpha", resource, "projects.get")
	var bindSuccesses atomic.Int32
	operations := make(chan *VerifiedOperation, 16)
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			operation, err := binder.Bind("tenant-alpha", resource, "projects.get")
			if err == nil {
				bindSuccesses.Add(1)
				operations <- operation
			}
		}()
	}
	wait.Wait()
	close(operations)
	if bindSuccesses.Load() != 1 {
		t.Fatalf("concurrent bind successes=%d", bindSuccesses.Load())
	}
	operation := <-operations
	snapshot := allowedSnapshot(t, actor, "tenant-alpha", "project-alpha")
	var executeSuccesses atomic.Int32
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if operation.Execute(snapshot, now, func() error { return nil }) == nil {
				executeSuccesses.Add(1)
			}
		}()
	}
	wait.Wait()
	if executeSuccesses.Load() != 1 {
		t.Fatalf("concurrent execute successes=%d", executeSuccesses.Load())
	}
}

func testVerifiedOperationBinder(actor SubjectRef, tenantID string, resource ScopeRef, permission string) *VerifiedOperationBinder {
	binder := &VerifiedOperationBinder{
		lifetime: newOperationLifetime(), consumed: &atomic.Bool{}, progress: &atomic.Uint32{}, actor: actor, tenantID: tenantID, resource: resource, permission: permission,
	}
	binder.binding = operationBinding(actor, tenantID, resource, permission)
	binder.self = binder
	return binder
}

func allowedSnapshot(t *testing.T, actor SubjectRef, tenantID, projectID string) Snapshot {
	t.Helper()
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	project := ScopePath{Level: ScopeProject, TenantID: tenantID, OrganizationID: "organization-alpha", ProjectID: projectID}
	return Snapshot{
		TenantID: tenantID, Scope: project, ScopeResolved: true, Catalog: builtinCatalogFixture(t),
		Candidates: []Candidate{{
			Membership: MembershipFact{UID: "membership-alpha", Subject: actor, SubjectHash: digest, Scope: project, State: MembershipActive},
			Binding:    RoleBindingFact{UID: "role-binding-alpha", Subject: actor, SubjectHash: digest, RoleName: "project.viewer", RoleVersion: 1, Scope: project, State: BindingActive},
		}},
	}
}

func builtinCatalogFixture(t *testing.T) Catalog {
	return builtinCatalogFixtureAt(t, "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json")
}

func builtinCatalogV2Fixture(t *testing.T) Catalog {
	return builtinCatalogFixtureAt(t, "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v2.json")
}

func builtinCatalogFixtureAt(t *testing.T, path string) Catalog {
	t.Helper()
	var document struct {
		CatalogRevision string `json:"catalogRevision"`
		PublishedAt     string `json:"publishedAt"`
		Roles           []struct {
			Name        string   `json:"name"`
			Version     int64    `json:"version"`
			ScopeLevel  string   `json:"scopeLevel"`
			State       string   `json:"state"`
			Permissions []string `json:"permissions"`
		} `json:"roles"`
	}
	readFixture(t, path, &document)
	roles := make([]Role, len(document.Roles))
	for index, role := range document.Roles {
		catalogRevision := int64(1)
		publishedAt := builtinCatalogV1PublishedAt
		if document.CatalogRevision == "2" && role.Name == "platform.admin" {
			catalogRevision = 2
			publishedAt = document.PublishedAt
		}
		roles[index] = Role{
			Name: role.Name, Version: role.Version, CatalogRevision: catalogRevision,
			ScopeLevel: ScopeLevel(role.ScopeLevel), State: role.State,
			PublishedAt: publishedAt, Permissions: append([]string(nil), role.Permissions...),
		}
	}
	return Catalog{Roles: roles}
}

func readFixture(t *testing.T, relative string, target any) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", filepath.FromSlash(relative))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatal(err)
	}
}

func cloneCatalog(value Catalog) Catalog {
	cloned := Catalog{Roles: make([]Role, len(value.Roles))}
	for index, role := range value.Roles {
		cloned.Roles[index] = role
		cloned.Roles[index].Permissions = append([]string(nil), role.Permissions...)
	}
	return cloned
}

func cloneSnapshot(value Snapshot) Snapshot {
	cloned := value
	cloned.Catalog = cloneCatalog(value.Catalog)
	cloned.Candidates = append([]Candidate(nil), value.Candidates...)
	for index := range cloned.Candidates {
		cloned.Candidates[index].Membership.ExpiresAt = cloneTimeValue(value.Candidates[index].Membership.ExpiresAt)
		cloned.Candidates[index].Binding.ExpiresAt = cloneTimeValue(value.Candidates[index].Binding.ExpiresAt)
	}
	if value.GlobalBinding != nil {
		binding := *value.GlobalBinding
		cloned.GlobalBinding = &binding
	}
	return cloned
}

func cloneTime(value time.Time) *time.Time {
	return &value
}

func cloneTimeValue(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return cloneTime(*value)
}

func stringsOf(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
