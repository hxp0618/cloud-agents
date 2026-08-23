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
