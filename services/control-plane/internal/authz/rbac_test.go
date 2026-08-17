package authz

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestEvaluateDefaultDenyAndScopeContainment(t *testing.T) {
	now := time.Date(2026, time.August, 17, 1, 2, 3, 0, time.UTC)
	tenantID := "tenant-alpha"
	subject := SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-alpha"}
	digest, err := subject.Digest()
	if err != nil {
		t.Fatal(err)
	}
	request := Request{
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
	decision, err := Evaluate(base, request, now)
	if err != nil || !decision.Allowed || decision.Evidence == nil || decision.Evidence.RoleBindingUID != "role-binding-alpha" {
		t.Fatalf("allow decision = %#v err=%v", decision, err)
	}

	tests := []struct {
		name   string
		mutate func(*Snapshot, *Request)
		reason DenyReason
	}{
		{name: "missing candidates", mutate: func(snapshot *Snapshot, _ *Request) {
			snapshot.Candidates = nil
		}, reason: DenyNoEligibleBinding},
		{name: "suspended membership", mutate: func(snapshot *Snapshot, _ *Request) {
			snapshot.Candidates[0].Membership.State = MembershipSuspended
		}, reason: DenyNoEligibleBinding},
		{name: "revoked binding", mutate: func(snapshot *Snapshot, _ *Request) {
			snapshot.Candidates[0].Binding.State = BindingRevoked
		}, reason: DenyNoEligibleBinding},
		{name: "expiry equality", mutate: func(snapshot *Snapshot, _ *Request) {
			snapshot.Candidates[0].Binding.ExpiresAt = cloneTime(now)
		}, reason: DenyNoEligibleBinding},
		{name: "membership narrower than binding", mutate: func(snapshot *Snapshot, _ *Request) {
			snapshot.Candidates[0].Membership.Scope = ScopePath{Level: ScopeProject, TenantID: tenantID, OrganizationID: "organization-other", ProjectID: "project-alpha"}
		}, reason: DenyNoEligibleBinding},
		{name: "binding outside request", mutate: func(snapshot *Snapshot, _ *Request) {
			snapshot.Candidates[0].Binding.Scope = ScopePath{Level: ScopeProject, TenantID: tenantID, OrganizationID: "organization-other", ProjectID: "project-alpha"}
		}, reason: DenyNoEligibleBinding},
		{name: "platform role is not tenant runtime authority", mutate: func(snapshot *Snapshot, _ *Request) {
			snapshot.Candidates[0].Membership.Scope = ScopePath{Level: ScopePlatform}
			snapshot.Candidates[0].Binding.RoleName = "platform.admin"
			snapshot.Candidates[0].Binding.Scope = ScopePath{Level: ScopePlatform}
		}, reason: DenyNoEligibleBinding},
		{name: "unregistered permission", mutate: func(_ *Snapshot, request *Request) {
			request.Permission = "projects.future"
		}, reason: DenyNoEligibleBinding},
		{name: "unresolved scope", mutate: func(snapshot *Snapshot, _ *Request) {
			snapshot.ScopeResolved = false
		}, reason: DenyUnknownScope},
		{name: "platform runtime", mutate: func(_ *Snapshot, request *Request) {
			request.Resource = ScopeRef{Level: ScopePlatform}
		}, reason: DenyPlatformRuntime},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := cloneSnapshot(base)
			requestCopy := request
			test.mutate(&snapshot, &requestCopy)
			got, gotErr := Evaluate(snapshot, requestCopy, now)
			if gotErr != nil || got.Allowed || got.Reason != test.reason || got.Evidence != nil {
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
	request := Request{Subject: subject, Permission: "projects.get", Resource: ScopeRef{Level: ScopeProject, ID: "project-alpha"}}
	decision, err := Evaluate(snapshot, request, now)
	if err != nil || !decision.Allowed || decision.Evidence == nil || decision.Evidence.MembershipUID != "membership-narrow" {
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
	request := Request{Subject: subject, Permission: "projects.get", Resource: ScopeRef{Level: ScopeProject, ID: "project-alpha"}}

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
			decision, gotErr := Evaluate(snapshot, request, now)
			if decision.Allowed || !errors.Is(gotErr, test.target) {
				t.Fatalf("decision=%#v err=%v, want %v", decision, gotErr, test.target)
			}
		})
	}
}

func builtinCatalogFixture(t *testing.T) Catalog {
	t.Helper()
	var document struct {
		Roles []struct {
			Name        string   `json:"name"`
			Version     int64    `json:"version"`
			ScopeLevel  string   `json:"scopeLevel"`
			State       string   `json:"state"`
			Permissions []string `json:"permissions"`
		} `json:"roles"`
	}
	readFixture(t, "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json", &document)
	roles := make([]Role, len(document.Roles))
	for index, role := range document.Roles {
		roles[index] = Role{
			Name: role.Name, Version: role.Version, CatalogRevision: 1,
			ScopeLevel: ScopeLevel(role.ScopeLevel), State: role.State,
			PublishedAt: "2026-08-17T00:00:00Z", Permissions: append([]string(nil), role.Permissions...),
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
