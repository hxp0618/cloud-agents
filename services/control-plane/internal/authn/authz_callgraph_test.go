package authn

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const authzImportPath = "github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"

const controlPlaneImportPath = "github.com/hxp0618/cloud-agents/services/control-plane"

// TestProductionAuthzAuthorityCallGraphClosure is a deliberate, generated-
// candidate review barrier. The binder, operation, and snapshot remain public
// Go types only because store/postgres cannot otherwise consume them without
// an import cycle. Every production reference is therefore frozen here. A new
// caller, helper, alias, method use, or snapshot construction fails this test
// until that exact path receives independent review.
func TestProductionAuthzAuthorityCallGraphClosure(t *testing.T) {
	events, err := scanProductionAuthzAuthorityEvents(filepath.Clean("../.."))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"internal/authz/rbac.go:<package>:package.VerifiedOperation",
		"internal/authz/rbac.go:<package>:package.VerifiedOperationBinder",
		"internal/authz/rbac.go:VerifiedOperation.Actor:package.VerifiedOperation",
		"internal/authz/rbac.go:VerifiedOperation.Execute:package.Snapshot",
		"internal/authz/rbac.go:VerifiedOperation.Execute:package.VerifiedOperation",
		"internal/authz/rbac.go:VerifiedOperation.selfBound:package.VerifiedOperation",
		"internal/authz/rbac.go:VerifiedOperationBinder.Bind:package.VerifiedOperation",
		"internal/authz/rbac.go:VerifiedOperationBinder.Bind:package.VerifiedOperation",
		"internal/authz/rbac.go:VerifiedOperationBinder.Bind:package.VerifiedOperationBinder",
		"internal/authz/rbac.go:WithVerifiedOperation:package.VerifiedOperationBinder",
		"internal/authz/rbac.go:WithVerifiedOperation:package.VerifiedOperationBinder",
		"internal/authz/rbac.go:evaluate:package.Snapshot",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetCleanup:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetCleanup:method.Actor",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetCleanup:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetCleanup:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetCleanup:package.WithVerifiedOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetProbe:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetProbe:method.Actor",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetProbe:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetProbe:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.BeginDeploymentTargetProbe:package.WithVerifiedOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.CompleteDeploymentTargetCleanup:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.CompleteDeploymentTargetCleanup:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.CompleteDeploymentTargetCleanup:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.CompleteDeploymentTargetCleanup:package.WithVerifiedOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.CompleteDeploymentTargetProbe:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.CompleteDeploymentTargetProbe:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.CompleteDeploymentTargetProbe:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.CompleteDeploymentTargetProbe:package.WithVerifiedOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.GetDeploymentTarget:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.GetDeploymentTarget:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.GetDeploymentTarget:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.GetDeploymentTarget:package.WithVerifiedOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargetAuditEvents:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargetAuditEvents:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargetAuditEvents:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargetAuditEvents:package.WithVerifiedOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargetOperations:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargetOperations:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargetOperations:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargetOperations:package.WithVerifiedOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargets:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargets:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargets:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.ListDeploymentTargets:package.WithVerifiedOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.RegisterDeploymentTarget:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.RegisterDeploymentTarget:method.Actor",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.RegisterDeploymentTarget:method.Bind",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.RegisterDeploymentTarget:package.VerifiedOperationBinder",
		"internal/store/postgres/deployment_target.go:DurableCoordinationService.RegisterDeploymentTarget:package.WithVerifiedOperation",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.ClaimIdempotency:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.ClaimIdempotency:method.Actor",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.ClaimIdempotency:method.Bind",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.ClaimIdempotency:package.VerifiedOperationBinder",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.ClaimIdempotency:package.WithVerifiedOperation",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencyFailure:method.Actor",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencyFailure:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencyFailure:method.Bind",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencyFailure:package.VerifiedOperationBinder",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencyFailure:package.WithVerifiedOperation",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencySuccess:method.Actor",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencySuccess:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencySuccess:method.Bind",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencySuccess:package.VerifiedOperationBinder",
		"internal/store/postgres/durable_coordination.go:DurableCoordinationService.CompleteIdempotencySuccess:package.WithVerifiedOperation",
		"internal/store/postgres/durable_project_create.go:DurableCoordinationService.CreateProjectDurable:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/durable_project_create.go:DurableCoordinationService.CreateProjectDurable:method.Actor",
		"internal/store/postgres/durable_project_create.go:DurableCoordinationService.CreateProjectDurable:method.Bind",
		"internal/store/postgres/durable_project_create.go:DurableCoordinationService.CreateProjectDurable:package.VerifiedOperationBinder",
		"internal/store/postgres/durable_project_create.go:DurableCoordinationService.CreateProjectDurable:package.WithVerifiedOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.CreateEnvironmentProfile:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.CreateEnvironmentProfile:method.Actor",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.CreateEnvironmentProfile:method.Bind",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.CreateEnvironmentProfile:package.VerifiedOperationBinder",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.CreateEnvironmentProfile:package.WithVerifiedOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.GetEnvironmentProfile:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.GetEnvironmentProfile:method.Bind",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.GetEnvironmentProfile:package.VerifiedOperationBinder",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.GetEnvironmentProfile:package.WithVerifiedOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.ListEnvironmentProfileAuditEvents:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.ListEnvironmentProfileAuditEvents:method.Bind",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.ListEnvironmentProfileAuditEvents:package.VerifiedOperationBinder",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.ListEnvironmentProfileAuditEvents:package.WithVerifiedOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.ListEnvironmentProfiles:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.ListEnvironmentProfiles:method.Bind",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.ListEnvironmentProfiles:package.VerifiedOperationBinder",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.ListEnvironmentProfiles:package.WithVerifiedOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.TransitionEnvironmentProfile:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.TransitionEnvironmentProfile:method.Actor",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.TransitionEnvironmentProfile:method.Bind",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.TransitionEnvironmentProfile:package.VerifiedOperationBinder",
		"internal/store/postgres/environment_profile.go:DurableCoordinationService.TransitionEnvironmentProfile:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_execution.go:DurableCoordinationService.GetManagedAgentExecution:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_execution.go:DurableCoordinationService.GetManagedAgentExecution:method.Bind",
		"internal/store/postgres/managed_agent_execution.go:DurableCoordinationService.GetManagedAgentExecution:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_execution.go:DurableCoordinationService.GetManagedAgentExecution:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_execution.go:DurableCoordinationService.ListManagedAgentExecutions:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_execution.go:DurableCoordinationService.ListManagedAgentExecutions:method.Bind",
		"internal/store/postgres/managed_agent_execution.go:DurableCoordinationService.ListManagedAgentExecutions:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_execution.go:DurableCoordinationService.ListManagedAgentExecutions:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_execution.go:withManagedAgentProjectMutation:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_execution.go:withManagedAgentProjectMutation:method.Bind",
		"internal/store/postgres/managed_agent_execution.go:withManagedAgentProjectMutation:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_execution.go:withManagedAgentProjectMutation:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_events.go:DurableCoordinationService.GetManagedAgentEvents:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_events.go:DurableCoordinationService.GetManagedAgentEvents:method.Bind",
		"internal/store/postgres/managed_agent_events.go:DurableCoordinationService.GetManagedAgentEvents:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_events.go:DurableCoordinationService.GetManagedAgentEvents:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.CloseManagedAgentSession:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.CloseManagedAgentSession:method.Bind",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.CloseManagedAgentSession:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.CloseManagedAgentSession:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.CreateManagedAgentSession:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.CreateManagedAgentSession:method.Bind",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.CreateManagedAgentSession:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.CreateManagedAgentSession:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.GetManagedAgentSession:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.GetManagedAgentSession:method.Bind",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.GetManagedAgentSession:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.GetManagedAgentSession:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.getManagedAgentSessionForRuntime:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.getManagedAgentSessionForRuntime:method.Bind",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.getManagedAgentSessionForRuntime:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.getManagedAgentSessionForRuntime:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.ListManagedAgentSessions:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.ListManagedAgentSessions:method.Bind",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.ListManagedAgentSessions:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_session.go:DurableCoordinationService.ListManagedAgentSessions:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.CreateManagedAgentTurn:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.CreateManagedAgentTurn:method.Bind",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.CreateManagedAgentTurn:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.CreateManagedAgentTurn:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.readManagedAgentTurn:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.readManagedAgentTurn:method.Bind",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.readManagedAgentTurn:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.readManagedAgentTurn:package.WithVerifiedOperation",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.ListManagedAgentTurns:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.ListManagedAgentTurns:method.Bind",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.ListManagedAgentTurns:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_agent_turn.go:DurableCoordinationService.ListManagedAgentTurns:package.WithVerifiedOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.BeginManagedHostEnvironmentLeaseUpgrade:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.BeginManagedHostEnvironmentLeaseUpgrade:method.Bind",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.BeginManagedHostEnvironmentLeaseUpgrade:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.BeginManagedHostEnvironmentLeaseUpgrade:package.WithVerifiedOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CompleteManagedHostEnvironmentLeaseDeployment:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CompleteManagedHostEnvironmentLeaseDeployment:method.Bind",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CompleteManagedHostEnvironmentLeaseDeployment:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CompleteManagedHostEnvironmentLeaseDeployment:package.WithVerifiedOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CompleteManagedHostEnvironmentLeaseTermination:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CompleteManagedHostEnvironmentLeaseTermination:method.Bind",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CompleteManagedHostEnvironmentLeaseTermination:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CompleteManagedHostEnvironmentLeaseTermination:package.WithVerifiedOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CreateManagedHostEnvironmentLease:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CreateManagedHostEnvironmentLease:method.Bind",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CreateManagedHostEnvironmentLease:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.CreateManagedHostEnvironmentLease:package.WithVerifiedOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.GetManagedHostEnvironmentLease:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.GetManagedHostEnvironmentLease:method.Bind",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.GetManagedHostEnvironmentLease:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.GetManagedHostEnvironmentLease:package.WithVerifiedOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.ListManagedHostEnvironmentLeases:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.ListManagedHostEnvironmentLeases:method.Bind",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.ListManagedHostEnvironmentLeases:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.ListManagedHostEnvironmentLeases:package.WithVerifiedOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.TerminateManagedHostEnvironmentLease:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.TerminateManagedHostEnvironmentLease:method.Bind",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.TerminateManagedHostEnvironmentLease:package.VerifiedOperationBinder",
		"internal/store/postgres/managed_host_environment_lease.go:DurableCoordinationService.TerminateManagedHostEnvironmentLease:package.WithVerifiedOperation",
		"internal/store/postgres/organization_create.go:DurableCoordinationService.CreateOrganization:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/organization_create.go:DurableCoordinationService.CreateOrganization:method.Bind",
		"internal/store/postgres/organization_create.go:DurableCoordinationService.CreateOrganization:package.VerifiedOperationBinder",
		"internal/store/postgres/organization_create.go:DurableCoordinationService.CreateOrganization:package.WithVerifiedOperation",
		"internal/store/postgres/organization_read.go:DurableCoordinationService.GetOrganization:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/organization_read.go:DurableCoordinationService.GetOrganization:method.Bind",
		"internal/store/postgres/organization_read.go:DurableCoordinationService.GetOrganization:package.VerifiedOperationBinder",
		"internal/store/postgres/organization_read.go:DurableCoordinationService.GetOrganization:package.WithVerifiedOperation",
		"internal/store/postgres/organization_read.go:DurableCoordinationService.ListOrganizations:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/organization_read.go:DurableCoordinationService.ListOrganizations:method.Bind",
		"internal/store/postgres/organization_read.go:DurableCoordinationService.ListOrganizations:package.VerifiedOperationBinder",
		"internal/store/postgres/organization_read.go:DurableCoordinationService.ListOrganizations:package.WithVerifiedOperation",
		"internal/store/postgres/project_read.go:DurableCoordinationService.GetProject:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/project_read.go:DurableCoordinationService.GetProject:method.Bind",
		"internal/store/postgres/project_read.go:DurableCoordinationService.GetProject:package.VerifiedOperationBinder",
		"internal/store/postgres/project_read.go:DurableCoordinationService.GetProject:package.WithVerifiedOperation",
		"internal/store/postgres/project_read.go:DurableCoordinationService.ListProjects:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/project_read.go:DurableCoordinationService.ListProjects:method.Bind",
		"internal/store/postgres/project_read.go:DurableCoordinationService.ListProjects:package.VerifiedOperationBinder",
		"internal/store/postgres/project_read.go:DurableCoordinationService.ListProjects:package.WithVerifiedOperation",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.GetMembership:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.GetMembership:method.Bind",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.GetMembership:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.GetMembership:package.WithVerifiedOperation",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.ListMemberships:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.ListMemberships:method.Bind",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.ListMemberships:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.ListMemberships:package.WithVerifiedOperation",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.ListRoleBindings:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.ListRoleBindings:method.Bind",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.ListRoleBindings:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.ListRoleBindings:package.WithVerifiedOperation",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.GetRoleBinding:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.GetRoleBinding:method.Bind",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.GetRoleBinding:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_read.go:DurableCoordinationService.GetRoleBinding:package.WithVerifiedOperation",
		"internal/store/postgres/role_read.go:DurableCoordinationService.GetRole:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/role_read.go:DurableCoordinationService.GetRole:method.Bind",
		"internal/store/postgres/role_read.go:DurableCoordinationService.GetRole:package.VerifiedOperationBinder",
		"internal/store/postgres/role_read.go:DurableCoordinationService.GetRole:package.WithVerifiedOperation",
		"internal/store/postgres/role_read.go:DurableCoordinationService.ListRoles:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/role_read.go:DurableCoordinationService.ListRoles:method.Bind",
		"internal/store/postgres/role_read.go:DurableCoordinationService.ListRoles:package.VerifiedOperationBinder",
		"internal/store/postgres/role_read.go:DurableCoordinationService.ListRoles:package.WithVerifiedOperation",
		"internal/store/postgres/tenant_read.go:DurableCoordinationService.GetPlatformTenant:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/tenant_read.go:DurableCoordinationService.GetPlatformTenant:method.Bind",
		"internal/store/postgres/tenant_read.go:DurableCoordinationService.GetPlatformTenant:package.VerifiedOperationBinder",
		"internal/store/postgres/tenant_read.go:DurableCoordinationService.GetPlatformTenant:package.WithVerifiedOperation",
		"internal/store/postgres/rbac.go:executeVerifiedRBACOperation:method.Actor",
		"internal/store/postgres/rbac.go:executeVerifiedRBACOperation:method.Execute",
		"internal/store/postgres/rbac.go:executeVerifiedRBACOperation:package.Snapshot",
		"internal/store/postgres/rbac.go:executeVerifiedRBACOperation:package.VerifiedOperation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.BindRole:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.BindRole:package.WithVerifiedOperation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.BindRole:bridge.withKnownScopeMutation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.CreateMembership:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.CreateMembership:package.WithVerifiedOperation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.CreateMembership:bridge.withKnownScopeMutation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.ResumeMembership:bridge.transitionMembership",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.RevokeMembership:bridge.transitionMembership",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.RevokeRoleBinding:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.RevokeRoleBinding:package.WithVerifiedOperation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.RevokeRoleBinding:bridge.withStoredScopeMutation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.SuspendMembership:bridge.transitionMembership",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.transitionMembership:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.transitionMembership:package.WithVerifiedOperation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.transitionMembership:bridge.withStoredScopeMutation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.withKnownScopeMutation:method.Bind",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.withKnownScopeMutation:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.withKnownScopeMutation:bridge.executeVerifiedRBACOperation",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.withStoredScopeMutation:method.Bind",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.withStoredScopeMutation:package.VerifiedOperationBinder",
		"internal/store/postgres/rbac_mutation.go:RBACMutationService.withStoredScopeMutation:bridge.executeVerifiedRBACOperation",
	}
	assertExactAuthorityEvents(t, events, want)
}

func TestProductionAuthzAuthorityCallGraphRejectsUnreviewedBypass(t *testing.T) {
	t.Run("same-package bypass", func(t *testing.T) {
		root, baseline := newAuthorityScannerFixture(t)
		writeAuthorityScannerFixture(t, root, "internal/authz/bypass.go", `package authz
func UnreviewedSamePackage(principal any, binder *VerifiedOperationBinder) {
	_ = WithVerifiedOperation
	_, _ = binder.Bind("tenant", ScopeRef{}, "permission")
}`)
		assertAuthorityFixtureAdds(t, root, baseline, "internal/authz/bypass.go:UnreviewedSamePackage")
	})

	t.Run("bridge function value alias", func(t *testing.T) {
		root, baseline := newAuthorityScannerFixture(t)
		writeAuthorityScannerFixture(t, root, "internal/store/postgres/bypass.go", `package postgres
var unreviewedDispatch = executeVerifiedRBACOperation
`)
		assertAuthorityFixtureAdds(t, root, baseline, "internal/store/postgres/bypass.go:<package>:bridge.executeVerifiedRBACOperation")
	})

	t.Run("indirect method helper", func(t *testing.T) {
		root, baseline := newAuthorityScannerFixture(t)
		writeAuthorityScannerFixture(t, root, "internal/store/postgres/indirect.go", `package postgres
import az "github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
func unreviewedIndirect(operation *az.VerifiedOperation) {
	execute := operation.Execute
	_ = execute
}`)
		assertAuthorityFixtureAdds(t, root, baseline, "internal/store/postgres/indirect.go:unreviewedIndirect:method.Execute")
	})

	t.Run("cross-file dynamic dispatch", func(t *testing.T) {
		root, baseline := newAuthorityScannerFixture(t)
		writeAuthorityScannerFixture(t, root, "internal/store/postgres/dynamic.go", `package postgres
import "reflect"
var unreviewedDynamicDispatch = reflect.ValueOf
`)
		assertAuthorityFixtureAdds(t, root, baseline, "internal/store/postgres/dynamic.go:<package>:forbidden.dynamic-dispatch-import")
	})

	t.Run("unrelated selectors are ignored", func(t *testing.T) {
		root, baseline := newAuthorityScannerFixture(t)
		writeAuthorityScannerFixture(t, root, "internal/ordinary/ordinary.go", `package ordinary
type localOperation struct{}
func (localOperation) Bind() {}
func (localOperation) Actor() {}
func (localOperation) Execute() {}
func ordinary(value localOperation) {
	value.Bind()
	value.Actor()
	value.Execute()
}`)
		events, err := scanProductionAuthzAuthorityEvents(root)
		if err != nil {
			t.Fatal(err)
		}
		assertExactAuthorityEvents(t, events, baseline)
	})

	t.Run("exported same-package surface", func(t *testing.T) {
		root, _ := newAuthorityScannerFixture(t)
		baseline, err := scanProductionAuthzExportedDeclarations(root)
		if err != nil {
			t.Fatal(err)
		}
		writeAuthorityScannerFixture(t, root, "internal/authz/exported_bypass.go", `package authz
func UnreviewedExportedBypass() {}
`)
		events, err := scanProductionAuthzExportedDeclarations(root)
		if err != nil {
			t.Fatal(err)
		}
		added := authorityEventDifference(events, baseline)
		want := "internal/authz/exported_bypass.go:<package>:surface.func.UnreviewedExportedBypass"
		if len(added) != 1 || added[0] != want {
			t.Fatalf("exported same-package surface was not frozen: got=%v want=%q", added, want)
		}
	})
}

func TestProductionAuthzExportedDeclarationSurfaceClosure(t *testing.T) {
	got, err := scanProductionAuthzExportedDeclarations(filepath.Clean("../.."))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(`
internal/authz/rbac.go:<package>:surface.const.BindingActive
internal/authz/rbac.go:<package>:surface.const.BindingRevoked
internal/authz/rbac.go:<package>:surface.const.MembershipActive
internal/authz/rbac.go:<package>:surface.const.MembershipRevoked
internal/authz/rbac.go:<package>:surface.const.MembershipSuspended
internal/authz/rbac.go:<package>:surface.const.ScopeOrganization
internal/authz/rbac.go:<package>:surface.const.ScopePlatform
internal/authz/rbac.go:<package>:surface.const.ScopeProject
internal/authz/rbac.go:<package>:surface.const.ScopeTenant
internal/authz/rbac.go:<package>:surface.field.Candidate.Binding
internal/authz/rbac.go:<package>:surface.field.Candidate.Membership
internal/authz/rbac.go:<package>:surface.field.Catalog.Roles
internal/authz/rbac.go:<package>:surface.field.MembershipFact.ExpiresAt
internal/authz/rbac.go:<package>:surface.field.MembershipFact.Scope
internal/authz/rbac.go:<package>:surface.field.MembershipFact.State
internal/authz/rbac.go:<package>:surface.field.MembershipFact.Subject
internal/authz/rbac.go:<package>:surface.field.MembershipFact.SubjectHash
internal/authz/rbac.go:<package>:surface.field.MembershipFact.UID
internal/authz/rbac.go:<package>:surface.field.Role.CatalogRevision
internal/authz/rbac.go:<package>:surface.field.Role.Name
internal/authz/rbac.go:<package>:surface.field.Role.Permissions
internal/authz/rbac.go:<package>:surface.field.Role.PublishedAt
internal/authz/rbac.go:<package>:surface.field.Role.ScopeLevel
internal/authz/rbac.go:<package>:surface.field.Role.State
internal/authz/rbac.go:<package>:surface.field.Role.Version
internal/authz/rbac.go:<package>:surface.field.RoleBindingFact.ExpiresAt
internal/authz/rbac.go:<package>:surface.field.RoleBindingFact.RoleName
internal/authz/rbac.go:<package>:surface.field.RoleBindingFact.RoleVersion
internal/authz/rbac.go:<package>:surface.field.RoleBindingFact.Scope
internal/authz/rbac.go:<package>:surface.field.RoleBindingFact.State
internal/authz/rbac.go:<package>:surface.field.RoleBindingFact.Subject
internal/authz/rbac.go:<package>:surface.field.RoleBindingFact.SubjectHash
internal/authz/rbac.go:<package>:surface.field.RoleBindingFact.UID
internal/authz/rbac.go:<package>:surface.field.ScopePath.Level
internal/authz/rbac.go:<package>:surface.field.ScopePath.OrganizationID
internal/authz/rbac.go:<package>:surface.field.ScopePath.ProjectID
internal/authz/rbac.go:<package>:surface.field.ScopePath.TenantID
internal/authz/rbac.go:<package>:surface.field.ScopeRef.ID
internal/authz/rbac.go:<package>:surface.field.ScopeRef.Level
internal/authz/rbac.go:<package>:surface.field.Snapshot.Candidates
internal/authz/rbac.go:<package>:surface.field.Snapshot.Catalog
internal/authz/rbac.go:<package>:surface.field.Snapshot.Scope
internal/authz/rbac.go:<package>:surface.field.Snapshot.ScopeResolved
internal/authz/rbac.go:<package>:surface.field.Snapshot.TenantID
internal/authz/rbac.go:<package>:surface.field.SubjectRef.Issuer
internal/authz/rbac.go:<package>:surface.field.SubjectRef.Kind
internal/authz/rbac.go:<package>:surface.field.SubjectRef.Subject
internal/authz/rbac.go:<package>:surface.func.WithVerifiedOperation
internal/authz/rbac.go:<package>:surface.method.Catalog.Role
internal/authz/rbac.go:<package>:surface.method.Catalog.Validate
internal/authz/rbac.go:<package>:surface.method.ScopePath.Contains
internal/authz/rbac.go:<package>:surface.method.ScopePath.Validate
internal/authz/rbac.go:<package>:surface.method.ScopeRef.Validate
internal/authz/rbac.go:<package>:surface.method.SubjectRef.CanonicalBytes
internal/authz/rbac.go:<package>:surface.method.SubjectRef.Digest
internal/authz/rbac.go:<package>:surface.method.SubjectRef.Validate
internal/authz/rbac.go:<package>:surface.method.VerifiedOperation.Actor
internal/authz/rbac.go:<package>:surface.method.VerifiedOperation.Execute
internal/authz/rbac.go:<package>:surface.method.VerifiedOperationBinder.Bind
internal/authz/rbac.go:<package>:surface.type.Candidate
internal/authz/rbac.go:<package>:surface.type.Catalog
internal/authz/rbac.go:<package>:surface.type.MembershipFact
internal/authz/rbac.go:<package>:surface.type.Role
internal/authz/rbac.go:<package>:surface.type.RoleBindingFact
internal/authz/rbac.go:<package>:surface.type.ScopeLevel
internal/authz/rbac.go:<package>:surface.type.ScopePath
internal/authz/rbac.go:<package>:surface.type.ScopeRef
internal/authz/rbac.go:<package>:surface.type.Snapshot
internal/authz/rbac.go:<package>:surface.type.SubjectRef
internal/authz/rbac.go:<package>:surface.type.VerifiedOperation
internal/authz/rbac.go:<package>:surface.type.VerifiedOperationBinder
internal/authz/rbac.go:<package>:surface.var.ErrCatalogDrift
internal/authz/rbac.go:<package>:surface.var.ErrInvalidRequest
internal/authz/rbac.go:<package>:surface.var.ErrOperationDenied
internal/authz/rbac.go:<package>:surface.var.ErrScopeUnresolved
internal/authz/rbac.go:<package>:surface.var.ErrSnapshotMalformed
`)
	assertExactAuthorityEvents(t, got, want)
}

func scanProductionAuthzAuthorityEvents(root string) ([]string, error) {
	program, err := loadAuthorityScannerProgram(root)
	if err != nil {
		return nil, err
	}
	events := make([]string, 0)
	for _, sourcePackage := range program.packages {
		if !sourcePackage.referencesAuthority {
			continue
		}
		checked := program.check(sourcePackage.importPath)
		for _, sourceFile := range sourcePackage.files {
			relative, err := filepath.Rel(root, sourceFile.path)
			if err != nil {
				return nil, err
			}
			relative = filepath.ToSlash(relative)
			if sourceFile.dotOrBlankAuthzImport != "" {
				events = append(events, authorityEvent(relative, "<package>", "forbidden."+sourceFile.dotOrBlankAuthzImport+"-import"))
			}
			// Authority-bearing packages are closed as a unit: an otherwise
			// unrelated file in the same package can still use reflection or
			// unsafe to reach types imported by a sibling file.
			if sourceFile.importsDynamicDispatch {
				events = append(events, authorityEvent(relative, "<package>", "forbidden.dynamic-dispatch-import"))
			}
			inspect := func(node ast.Node, function string) {
				ast.Inspect(node, func(candidate ast.Node) bool {
					identifier, ok := candidate.(*ast.Ident)
					if !ok {
						return true
					}
					if symbol := authoritySymbol(checked.info.Uses[identifier]); symbol != "" {
						events = append(events, authorityEvent(relative, function, symbol))
					}
					return true
				})
			}
			for _, declaration := range sourceFile.syntax.Decls {
				switch item := declaration.(type) {
				case *ast.FuncDecl:
					function := item.Name.Name
					if receiver := receiverTypeName(item); receiver != "" {
						function = receiver + "." + function
					}
					inspect(item, function)
				case *ast.GenDecl:
					inspect(item, "<package>")
				}
			}
		}
	}
	sort.Strings(events)
	return events, nil
}

type authorityScannerProgram struct {
	packages         map[string]*authorityScannerPackage
	checked          map[string]*authorityCheckedPackage
	checking         map[string]bool
	externalImporter types.Importer
	fset             *token.FileSet
}

type authorityScannerPackage struct {
	importPath          string
	name                string
	files               []authorityScannerFile
	referencesAuthority bool
}

type authorityScannerFile struct {
	path                   string
	syntax                 *ast.File
	importsAuthz           bool
	importsDynamicDispatch bool
	dotOrBlankAuthzImport  string
}

type authorityCheckedPackage struct {
	pkg  *types.Package
	info *types.Info
}

func loadAuthorityScannerProgram(root string) (*authorityScannerProgram, error) {
	program := &authorityScannerProgram{
		packages:         make(map[string]*authorityScannerPackage),
		checked:          make(map[string]*authorityCheckedPackage),
		checking:         make(map[string]bool),
		externalImporter: importer.Default(),
		fset:             token.NewFileSet(),
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(program.fset, path, source, 0)
		if err != nil {
			return err
		}
		relativeDirectory, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		importPath := controlPlaneImportPath
		if relativeDirectory != "." {
			importPath += "/" + filepath.ToSlash(relativeDirectory)
		}
		sourceFile := authorityScannerFile{path: path, syntax: file}
		for _, imported := range file.Imports {
			pathValue := strings.Trim(imported.Path.Value, `"`)
			if pathValue == "reflect" || pathValue == "unsafe" {
				sourceFile.importsDynamicDispatch = true
			}
			if pathValue != authzImportPath {
				continue
			}
			sourceFile.importsAuthz = true
			if imported.Name != nil && (imported.Name.Name == "." || imported.Name.Name == "_") {
				sourceFile.dotOrBlankAuthzImport = imported.Name.Name
			}
		}
		pkg := program.packages[importPath]
		if pkg == nil {
			pkg = &authorityScannerPackage{importPath: importPath, name: file.Name.Name}
			program.packages[importPath] = pkg
		}
		pkg.files = append(pkg.files, sourceFile)
		pkg.referencesAuthority = pkg.referencesAuthority || importPath == authzImportPath || sourceFile.importsAuthz
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, pkg := range program.packages {
		sort.Slice(pkg.files, func(left, right int) bool { return pkg.files[left].path < pkg.files[right].path })
	}
	return program, nil
}

func (program *authorityScannerProgram) check(importPath string) *authorityCheckedPackage {
	if checked := program.checked[importPath]; checked != nil {
		return checked
	}
	sourcePackage := program.packages[importPath]
	if sourcePackage == nil {
		if imported, err := program.externalImporter.Import(importPath); err == nil {
			return &authorityCheckedPackage{pkg: imported, info: &types.Info{}}
		}
		stub := types.NewPackage(importPath, filepath.Base(importPath))
		stub.MarkComplete()
		return &authorityCheckedPackage{pkg: stub, info: &types.Info{}}
	}
	if program.checking[importPath] {
		stub := types.NewPackage(importPath, sourcePackage.name)
		return &authorityCheckedPackage{pkg: stub, info: &types.Info{}}
	}
	program.checking[importPath] = true
	defer delete(program.checking, importPath)
	info := &types.Info{
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Types:      make(map[ast.Expr]types.TypeAndValue),
	}
	files := make([]*ast.File, 0, len(sourcePackage.files))
	for _, file := range sourcePackage.files {
		files = append(files, file.syntax)
	}
	configuration := types.Config{
		Importer: program,
		Error:    func(error) {},
	}
	pkg, _ := configuration.Check(importPath, program.fset, files, info)
	if pkg == nil {
		pkg = types.NewPackage(importPath, sourcePackage.name)
	}
	checked := &authorityCheckedPackage{pkg: pkg, info: info}
	program.checked[importPath] = checked
	return checked
}

func (program *authorityScannerProgram) Import(importPath string) (*types.Package, error) {
	return program.check(importPath).pkg, nil
}

func authoritySymbol(object types.Object) string {
	if object == nil || object.Pkg() == nil {
		return ""
	}
	packagePath := object.Pkg().Path()
	if packagePath == authzImportPath {
		switch object := object.(type) {
		case *types.TypeName:
			switch object.Name() {
			case "VerifiedOperationBinder", "VerifiedOperation", "Snapshot":
				return "package." + object.Name()
			}
		case *types.Func:
			if object.Name() == "WithVerifiedOperation" {
				return "package." + object.Name()
			}
			if signature, ok := object.Type().(*types.Signature); ok && signature.Recv() != nil {
				switch object.Name() {
				case "Bind", "Actor", "Execute":
					if receiverNamedTypeName(signature.Recv().Type()) == map[string]string{
						"Bind": "VerifiedOperationBinder", "Actor": "VerifiedOperation", "Execute": "VerifiedOperation",
					}[object.Name()] {
						return "method." + object.Name()
					}
				}
			}
		}
	}
	if packagePath == controlPlaneImportPath+"/internal/store/postgres" {
		if function, ok := object.(*types.Func); ok {
			switch function.Name() {
			case "executeVerifiedRBACOperation", "transitionMembership", "withKnownScopeMutation", "withStoredScopeMutation":
				return "bridge." + function.Name()
			}
		}
	}
	return ""
}

func receiverNamedTypeName(value types.Type) string {
	if pointer, ok := value.(*types.Pointer); ok {
		value = pointer.Elem()
	}
	if named, ok := value.(*types.Named); ok && named.Obj() != nil {
		return named.Obj().Name()
	}
	return ""
}

func scanProductionAuthzExportedDeclarations(root string) ([]string, error) {
	authzRoot := filepath.Join(root, "internal", "authz")
	events := make([]string, 0)
	err := filepath.WalkDir(authzRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			switch item := declaration.(type) {
			case *ast.FuncDecl:
				if !item.Name.IsExported() {
					continue
				}
				if receiver := receiverTypeName(item); receiver != "" {
					events = append(events, authorityEvent(filepath.ToSlash(relative), "<package>", "surface.method."+receiver+"."+item.Name.Name))
				} else {
					events = append(events, authorityEvent(filepath.ToSlash(relative), "<package>", "surface.func."+item.Name.Name))
				}
			case *ast.GenDecl:
				for _, specification := range item.Specs {
					switch specification := specification.(type) {
					case *ast.ValueSpec:
						kind := strings.ToLower(item.Tok.String())
						for _, name := range specification.Names {
							if name.IsExported() {
								events = append(events, authorityEvent(filepath.ToSlash(relative), "<package>", "surface."+kind+"."+name.Name))
							}
						}
					case *ast.TypeSpec:
						if !specification.Name.IsExported() {
							continue
						}
						typeName := specification.Name.Name
						events = append(events, authorityEvent(filepath.ToSlash(relative), "<package>", "surface.type."+typeName))
						switch declared := specification.Type.(type) {
						case *ast.StructType:
							for _, field := range declared.Fields.List {
								if len(field.Names) == 0 {
									if name := embeddedExportedName(field.Type); name != "" {
										events = append(events, authorityEvent(filepath.ToSlash(relative), "<package>", "surface.field."+typeName+"."+name))
									}
									continue
								}
								for _, name := range field.Names {
									if name.IsExported() {
										events = append(events, authorityEvent(filepath.ToSlash(relative), "<package>", "surface.field."+typeName+"."+name.Name))
									}
								}
							}
						case *ast.InterfaceType:
							for _, method := range declared.Methods.List {
								for _, name := range method.Names {
									if name.IsExported() {
										events = append(events, authorityEvent(filepath.ToSlash(relative), "<package>", "surface.interface."+typeName+"."+name.Name))
									}
								}
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(events)
	return events, nil
}

func embeddedExportedName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		if expression.IsExported() {
			return expression.Name
		}
	case *ast.SelectorExpr:
		if expression.Sel.IsExported() {
			return expression.Sel.Name
		}
	case *ast.StarExpr:
		return embeddedExportedName(expression.X)
	case *ast.IndexExpr:
		return embeddedExportedName(expression.X)
	case *ast.IndexListExpr:
		return embeddedExportedName(expression.X)
	}
	return ""
}

func newAuthorityScannerFixture(t *testing.T) (string, []string) {
	t.Helper()
	root := t.TempDir()
	writeAuthorityScannerFixture(t, root, "internal/authz/rbac.go", `package authz
type ScopeRef struct{}
type Snapshot struct{}
type VerifiedOperationBinder struct{}
type VerifiedOperation struct{}
func WithVerifiedOperation(principal any, callback func(*VerifiedOperationBinder) error) error { return nil }
func (*VerifiedOperationBinder) Bind(string, ScopeRef, string) (*VerifiedOperation, error) { return nil, nil }
func (*VerifiedOperation) Actor() (any, bool) { return nil, false }
func (*VerifiedOperation) Execute(Snapshot, any, func() error) error { return nil }
`)
	writeAuthorityScannerFixture(t, root, "internal/store/postgres/rbac.go", `package postgres
import az "github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
func executeVerifiedRBACOperation(operation *az.VerifiedOperation) error { return nil }
`)
	baseline, err := scanProductionAuthzAuthorityEvents(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, baseline
}

func writeAuthorityScannerFixture(t *testing.T, root string, relative string, source string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertAuthorityFixtureAdds(t *testing.T, root string, baseline []string, fragment string) {
	t.Helper()
	events, err := scanProductionAuthzAuthorityEvents(root)
	if err != nil {
		t.Fatal(err)
	}
	added := authorityEventDifference(events, baseline)
	for _, event := range added {
		if strings.Contains(event, fragment) {
			return
		}
	}
	t.Fatalf("unreviewed authority path was not detected: fragment=%q added=%v all=%v", fragment, added, events)
}

func authorityEventDifference(events []string, baseline []string) []string {
	counts := make(map[string]int, len(baseline))
	for _, event := range baseline {
		counts[event]++
	}
	added := make([]string, 0)
	for _, event := range events {
		if counts[event] != 0 {
			counts[event]--
			continue
		}
		added = append(added, event)
	}
	return added
}

func authorityEvent(path string, function string, symbol string) string {
	return fmt.Sprintf("%s:%s:%s", path, function, symbol)
}

func assertExactAuthorityEvents(t *testing.T, got []string, want []string) {
	t.Helper()
	if err := compareExactAuthorityEvents(got, want); err != nil {
		t.Fatal(err)
	}
}

func compareExactAuthorityEvents(got []string, want []string) error {
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		return fmt.Errorf("production authz authority call graph changed:\n got=%v\nwant=%v", got, want)
	}
	return nil
}
