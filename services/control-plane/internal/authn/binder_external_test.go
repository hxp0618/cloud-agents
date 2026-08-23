package authn_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
)

func TestCrossPackageVerifiedOperationPositiveOneShotEscapeAndInvalidation(t *testing.T) {
	handle := authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		SubjectValue: "user-alpha", TenantID: "tenant-alpha", ResourceLevel: "project",
		ResourceID: "project-alpha", Permission: "projects.get",
	})
	resource := authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"}
	var escapedBinder *authz.VerifiedOperationBinder
	var escapedOperation *authz.VerifiedOperation
	invalidateStarted := make(chan struct{})
	invalidateDone := make(chan error, 1)
	callbackCount := 0
	err := authz.WithVerifiedOperation(handle.Principal, func(binder *authz.VerifiedOperationBinder) error {
		escapedBinder = binder
		operation, err := binder.Bind("tenant-alpha", resource, "projects.get")
		if err != nil {
			return err
		}
		escapedOperation = operation
		actor, ok := operation.Actor()
		if !ok || actor != (authz.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"}) {
			t.Fatalf("decoded actor = %#v ok=%v", actor, ok)
		}
		go func() {
			close(invalidateStarted)
			invalidateDone <- handle.Invalidate()
		}()
		<-invalidateStarted
		select {
		case err := <-invalidateDone:
			t.Fatalf("invalidation crossed an active generation lease: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
		return operation.Execute(allowedExternalSnapshot(t, actor), handle.Now, func() error {
			callbackCount++
			return nil
		})
	})
	if err != nil || callbackCount != 1 {
		t.Fatalf("verified operation err=%v callbackCount=%d", err, callbackCount)
	}
	if err := <-invalidateDone; err != nil {
		t.Fatal(err)
	}
	if _, err := escapedBinder.Bind("tenant-alpha", resource, "projects.get"); !errors.Is(err, authz.ErrOperationDenied) {
		t.Fatalf("escaped binder err=%v", err)
	}
	if _, ok := escapedOperation.Actor(); ok {
		t.Fatal("escaped operation retained actor")
	}
	if err := escapedOperation.Execute(authz.Snapshot{}, handle.Now, func() error { callbackCount++; return nil }); !errors.Is(err, authz.ErrOperationDenied) || callbackCount != 1 {
		t.Fatalf("escaped second execute err=%v callbackCount=%d", err, callbackCount)
	}
	if err := authz.WithVerifiedOperation(handle.Principal, func(*authz.VerifiedOperationBinder) error { return nil }); err == nil {
		t.Fatal("principal was reusable")
	}
}

func TestCrossPackageMismatchCopyDenyAndStaleGenerationFailClosed(t *testing.T) {
	resource := authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"}

	mismatch := authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		TenantID: "tenant-alpha", ResourceLevel: "project", ResourceID: "project-alpha", Permission: "projects.get",
	})
	if err := authz.WithVerifiedOperation(mismatch.Principal, func(binder *authz.VerifiedOperationBinder) error {
		if _, err := binder.Bind("tenant-alpha", resource, "projects.update"); !errors.Is(err, authz.ErrOperationDenied) {
			t.Fatalf("permission mismatch err=%v", err)
		}
		if _, err := binder.Bind("tenant-alpha", resource, "projects.get"); !errors.Is(err, authz.ErrOperationDenied) {
			t.Fatalf("mismatch retry err=%v", err)
		}
		return nil
	}); !errors.Is(err, authz.ErrOperationDenied) {
		t.Fatalf("mismatch callback without Execute succeeded: %v", err)
	}

	copyFixture := authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		TenantID: "tenant-alpha", ResourceLevel: "project", ResourceID: "project-alpha", Permission: "projects.get",
	})
	if err := authz.WithVerifiedOperation(copyFixture.Principal, func(binder *authz.VerifiedOperationBinder) error {
		copied := *binder
		if _, err := copied.Bind("tenant-alpha", resource, "projects.get"); !errors.Is(err, authz.ErrOperationDenied) {
			t.Fatalf("copied binder err=%v", err)
		}
		if _, err := binder.Bind("tenant-alpha", resource, "projects.get"); !errors.Is(err, authz.ErrOperationDenied) {
			t.Fatalf("source binder after copied attempt err=%v", err)
		}
		return nil
	}); !errors.Is(err, authz.ErrOperationDenied) {
		t.Fatalf("copied callback without Execute succeeded: %v", err)
	}

	stale := authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		TenantID: "tenant-alpha", ResourceLevel: "project", ResourceID: "project-alpha", Permission: "projects.get",
	})
	if err := stale.Invalidate(); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := authz.WithVerifiedOperation(stale.Principal, func(*authz.VerifiedOperationBinder) error { called = true; return nil }); err == nil || called {
		t.Fatalf("stale principal err=%v called=%v", err, called)
	}
}

func TestCrossPackageVerifiedOperationRequiresBindAndExecuteForSuccess(t *testing.T) {
	resource := authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"}

	unused := authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		TenantID: "tenant-alpha", ResourceLevel: "project", ResourceID: "project-alpha", Permission: "projects.get",
	})
	if err := authz.WithVerifiedOperation(unused.Principal, func(*authz.VerifiedOperationBinder) error { return nil }); !errors.Is(err, authz.ErrOperationDenied) {
		t.Fatalf("unused binder succeeded: %v", err)
	}

	boundOnly := authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		TenantID: "tenant-alpha", ResourceLevel: "project", ResourceID: "project-alpha", Permission: "projects.get",
	})
	if err := authz.WithVerifiedOperation(boundOnly.Principal, func(binder *authz.VerifiedOperationBinder) error {
		_, err := binder.Bind("tenant-alpha", resource, "projects.get")
		return err
	}); !errors.Is(err, authz.ErrOperationDenied) {
		t.Fatalf("bound but unexecuted operation succeeded: %v", err)
	}
}

func TestCrossPackageZeroValuesAndErrorsFailClosedWithoutSensitiveMaterial(t *testing.T) {
	resource := authz.ScopeRef{Level: authz.ScopeProject, ID: "project-secret"}
	for name, err := range map[string]error{
		"nil binder": func() error {
			var binder *authz.VerifiedOperationBinder
			_, err := binder.Bind("tenant-secret", resource, "projects.secret")
			return err
		}(),
		"zero binder": func() error {
			binder := &authz.VerifiedOperationBinder{}
			_, err := binder.Bind("tenant-secret", resource, "projects.secret")
			return err
		}(),
		"zero operation": (&authz.VerifiedOperation{}).Execute(authz.Snapshot{}, time.Now(), func() error { return nil }),
	} {
		if !errors.Is(err, authz.ErrOperationDenied) {
			t.Fatalf("%s err=%v", name, err)
		}
		assertRedactedAuthorizationError(t, err)
	}
	if actor, ok := (&authz.VerifiedOperation{}).Actor(); ok || actor != (authz.SubjectRef{}) {
		t.Fatalf("zero operation actor=%#v ok=%v", actor, ok)
	}

	handle := authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		SubjectIssuer: "https://identity.example.test/", SubjectValue: "actor-secret", TenantID: "tenant-secret", ResourceLevel: "project",
		ResourceID: "project-secret", Permission: "projects.get",
	})
	err := authz.WithVerifiedOperation(handle.Principal, func(binder *authz.VerifiedOperationBinder) error {
		_, bindErr := binder.Bind("tenant-secret", resource, "projects.update")
		return bindErr
	})
	if !errors.Is(err, authz.ErrOperationDenied) {
		t.Fatalf("mismatch err=%v", err)
	}
	assertRedactedAuthorizationError(t, err)
}

func assertRedactedAuthorizationError(t *testing.T, err error) {
	t.Helper()
	message := err.Error()
	for _, secret := range []string{
		"actor-secret", "tenant-secret", "project-secret", "projects.secret", "projects.get", "projects.update",
		"https://issuer.example", "https://identity.example.test/",
	} {
		if strings.Contains(message, secret) {
			t.Fatalf("authorization error leaked %q: %q", secret, message)
		}
	}
}

func TestCrossPackageAsyncExecuteCannotEscapePrincipalLease(t *testing.T) {
	handle := authn.NewTestVerifiedPrincipal(t, authn.TestPrincipalFixture{
		SubjectValue: "user-alpha", TenantID: "tenant-alpha", ResourceLevel: "project",
		ResourceID: "project-alpha", Permission: "projects.get",
	})
	actor := authz.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"}
	snapshot := allowedExternalSnapshot(t, actor)
	resource := authz.ScopeRef{Level: authz.ScopeProject, ID: "project-alpha"}
	executeEntered := make(chan struct{})
	releaseExecute := make(chan struct{})
	executeDone := make(chan error, 1)
	withDone := make(chan error, 1)
	go func() {
		withDone <- authz.WithVerifiedOperation(handle.Principal, func(binder *authz.VerifiedOperationBinder) error {
			operation, err := binder.Bind("tenant-alpha", resource, "projects.get")
			if err != nil {
				return err
			}
			go func() {
				executeDone <- operation.Execute(snapshot, handle.Now, func() error {
					close(executeEntered)
					<-releaseExecute
					return nil
				})
			}()
			<-executeEntered
			return nil
		})
	}()
	<-executeEntered
	select {
	case err := <-withDone:
		t.Fatalf("WithVerifiedOperation returned before async Execute settled: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseExecute)
	if err := <-executeDone; err != nil {
		t.Fatal(err)
	}
	if err := <-withDone; err != nil {
		t.Fatal(err)
	}
}

func allowedExternalSnapshot(t *testing.T, actor authz.SubjectRef) authz.Snapshot {
	t.Helper()
	digest, err := actor.Digest()
	if err != nil {
		t.Fatal(err)
	}
	project := authz.ScopePath{
		Level: authz.ScopeProject, TenantID: "tenant-alpha", OrganizationID: "organization-alpha", ProjectID: "project-alpha",
	}
	return authz.Snapshot{
		TenantID: "tenant-alpha", Scope: project, ScopeResolved: true, Catalog: externalCatalogFixture(t),
		Candidates: []authz.Candidate{{
			Membership: authz.MembershipFact{UID: "membership-alpha", Subject: actor, SubjectHash: digest, Scope: project, State: authz.MembershipActive},
			Binding: authz.RoleBindingFact{
				UID: "role-binding-alpha", Subject: actor, SubjectHash: digest, RoleName: "project.viewer",
				RoleVersion: 1, Scope: project, State: authz.BindingActive,
			},
		}},
	}
}

func externalCatalogFixture(t *testing.T) authz.Catalog {
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
	path := filepath.Join("..", "..", "..", "..", "contracts", "platform", "v1alpha1", "fixtures", "golden", "builtin-role-catalog-v1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	roles := make([]authz.Role, len(document.Roles))
	for index, role := range document.Roles {
		roles[index] = authz.Role{
			Name: role.Name, Version: role.Version, CatalogRevision: 1, ScopeLevel: authz.ScopeLevel(role.ScopeLevel),
			State: role.State, PublishedAt: "2026-08-17T00:00:00Z", Permissions: append([]string(nil), role.Permissions...),
		}
	}
	return authz.Catalog{Roles: roles}
}
