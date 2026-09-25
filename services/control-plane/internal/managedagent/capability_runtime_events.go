package managedagent

import (
	"context"
	"errors"
	"strings"

	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

func (coordinator *DurableRuntimeExecutionCoordinator) recordCapabilityAdmissionEvents(
	ctx context.Context,
	principalSource VerifiedPrincipalSource,
	scope Scope,
	sessionID, turnID, executionID string,
	generation uint64,
	bindings []*workerruntimev1alpha1.RuntimeCapabilityBinding,
) error {
	for _, binding := range bindings {
		if binding == nil {
			return ErrInvalidInput
		}
		resource := ResourceKind("")
		resourceID := binding.GetResourceId()
		operation := ""
		switch binding.GetResourceKind() {
		case "mcp-server":
			resource, operation = ResourceMcpServer, "mcp.call"
		case "skill-bundle":
			resource, operation = ResourceSkillBundle, "skill.load"
		default:
			return ErrInvalidInput
		}
		event := CapabilityEventInput{
			Scope: scope, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: generation,
			Operation: operation, Resource: resource, ResourceID: resourceID, Version: binding.GetVersion(),
			Digest: binding.GetDigest(), Result: "accepted",
		}
		event.MutationDigest = CapabilityEventMutationDigest(event)
		principal, err := nextVerifiedPrincipal(principalSource)
		if err != nil {
			return err
		}
		if err := coordinator.store.RecordManagedAgentCapabilityEvent(ctx, scope.TenantID, principal, event); err != nil {
			return err
		}
	}
	return nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) recordCapabilityResolutionFailureEvent(
	ctx context.Context,
	principalSource VerifiedPrincipalSource,
	scope Scope,
	sessionID, turnID, executionID string,
	generation uint64,
	cause error,
) error {
	var failure *CapabilityResolutionFailure
	if !errors.As(cause, &failure) {
		return nil
	}
	operation, result := "", "failed"
	switch failure.Resource {
	case ResourceMcpServer:
		operation = "mcp.fail"
	case ResourceSkillBundle:
		operation = "skill.fail"
	default:
		return ErrInvalidInput
	}
	if failure.Reason == "revoked" {
		operation = strings.TrimSuffix(operation, ".fail") + ".revoke"
		result = "revoked"
	}
	event := CapabilityEventInput{
		Scope: scope, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: generation,
		Operation: operation, Resource: failure.Resource, ResourceID: failure.ResourceID,
		Version: failure.Version, Digest: failure.Digest, Result: result,
		ErrorCode: "capability_" + failure.Reason,
	}
	event.MutationDigest = CapabilityEventMutationDigest(event)
	principal, err := nextVerifiedPrincipal(principalSource)
	if err != nil {
		return err
	}
	return coordinator.store.RecordManagedAgentCapabilityEvent(ctx, scope.TenantID, principal, event)
}

func (coordinator *DurableRuntimeExecutionCoordinator) recordCapabilityExecutionFailureEvents(
	ctx context.Context,
	principalSource VerifiedPrincipalSource,
	scope Scope,
	sessionID, turnID, executionID string,
	generation uint64,
	bindings []*workerruntimev1alpha1.RuntimeCapabilityBinding,
	errorCode string,
) error {
	if errorCode != "capability_unsupported" {
		return nil
	}
	for _, binding := range bindings {
		if binding == nil {
			return ErrInvalidInput
		}
		resource, operation := ResourceKind(""), ""
		switch binding.GetResourceKind() {
		case "mcp-server":
			resource, operation = ResourceMcpServer, "mcp.fail"
		case "skill-bundle":
			resource, operation = ResourceSkillBundle, "skill.fail"
		default:
			return ErrInvalidInput
		}
		event := CapabilityEventInput{
			Scope: scope, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: generation,
			Operation: operation, Resource: resource, ResourceID: binding.GetResourceId(), Version: binding.GetVersion(),
			Digest: binding.GetDigest(), Result: "failed", ErrorCode: errorCode,
		}
		event.MutationDigest = CapabilityEventMutationDigest(event)
		principal, err := nextVerifiedPrincipal(principalSource)
		if err != nil {
			return err
		}
		if err := coordinator.store.RecordManagedAgentCapabilityEvent(ctx, scope.TenantID, principal, event); err != nil {
			return err
		}
	}
	return nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) recordCapabilityRuntimeOutcomeEvents(
	ctx context.Context,
	principalSource VerifiedPrincipalSource,
	scope Scope,
	sessionID, turnID, executionID string,
	generation uint64,
	bindings []*workerruntimev1alpha1.RuntimeCapabilityBinding,
	messages []runtimeprotocol.Message,
) error {
	byID := indexCapabilityBindings(bindings)
	written := make(map[string]struct{})
	for _, message := range messages {
		if message.MessageType != "Event" || message.Payload["eventType"] != "item.completed" {
			continue
		}
		payload, _ := message.Payload["payload"].(map[string]any)
		data, _ := payload["data"].(map[string]any)
		resourceID, _ := data["capabilityResourceId"].(string)
		resource := capabilityRuntimeEventResource(payload)
		if resource == "" {
			continue
		}
		binding := byID[capabilityBindingKey{resource: resource, id: resourceID}]
		operation := "mcp.call"
		if resource == ResourceSkillBundle {
			operation = "skill.load"
		}
		if binding == nil {
			continue
		}
		status, _ := payload["status"].(string)
		result, errorCode := "succeeded", ""
		switch status {
		case "completed":
		case "failed":
			operation, result, errorCode = capabilityFailureOperation(resource), "failed", "capability_call_failed"
		case "declined":
			operation, result, errorCode = capabilityFailureOperation(resource), "failed", "capability_call_declined"
		default:
			continue
		}
		resultDigest, err := RuntimeMessageDigest(publicRuntimeMessage(message))
		if err != nil {
			return err
		}
		event := CapabilityEventInput{
			Scope: scope, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: generation,
			Operation: operation, Resource: resource, ResourceID: resourceID, Version: binding.GetVersion(),
			Digest: binding.GetDigest(), Result: result, ResultDigest: resultDigest, ErrorCode: errorCode,
		}
		event.MutationDigest = CapabilityEventMutationDigest(event)
		if _, exists := written[event.MutationDigest]; exists {
			continue
		}
		written[event.MutationDigest] = struct{}{}
		principal, err := nextVerifiedPrincipal(principalSource)
		if err != nil {
			return err
		}
		if err := coordinator.store.RecordManagedAgentCapabilityEvent(ctx, scope.TenantID, principal, event); err != nil {
			return err
		}
	}
	return nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) recordPendingCapabilityFailureEvents(
	ctx context.Context,
	principalSource VerifiedPrincipalSource,
	scope Scope,
	sessionID, turnID, executionID string,
	generation uint64,
	bindings []*workerruntimev1alpha1.RuntimeCapabilityBinding,
	messages []runtimeprotocol.Message,
) error {
	byID := indexCapabilityBindings(bindings)
	type pendingCapability struct {
		resource   ResourceKind
		resourceID string
		message    runtimeprotocol.Message
	}
	type pendingKey struct {
		resource   ResourceKind
		resourceID string
		itemID     string
	}
	pending := make(map[pendingKey]pendingCapability)
	order := make([]pendingKey, 0)
	for _, message := range messages {
		if message.MessageType != "Event" {
			continue
		}
		eventType, _ := message.Payload["eventType"].(string)
		payload, _ := message.Payload["payload"].(map[string]any)
		data, _ := payload["data"].(map[string]any)
		itemID, _ := data["providerItemId"].(string)
		if itemID == "" {
			continue
		}
		resourceID, _ := data["capabilityResourceId"].(string)
		resource := capabilityRuntimeEventResource(payload)
		if resource == "" {
			continue
		}
		key := pendingKey{resource: resource, resourceID: resourceID, itemID: itemID}
		if eventType == "item.completed" {
			delete(pending, key)
			continue
		}
		if eventType != "item.started" && eventType != "item.updated" {
			continue
		}
		binding := byID[capabilityBindingKey{resource: resource, id: resourceID}]
		if binding == nil {
			continue
		}
		if _, exists := pending[key]; !exists {
			order = append(order, key)
			pending[key] = pendingCapability{resource: resource, resourceID: resourceID, message: message}
		}
	}
	for _, key := range order {
		activity, exists := pending[key]
		if !exists {
			continue
		}
		binding := byID[capabilityBindingKey{resource: activity.resource, id: activity.resourceID}]
		inputDigest, err := RuntimeMessageDigest(publicRuntimeMessage(activity.message))
		if err != nil {
			return err
		}
		event := CapabilityEventInput{
			Scope: scope, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: generation,
			Operation: capabilityFailureOperation(activity.resource), Resource: activity.resource, ResourceID: activity.resourceID,
			Version: binding.GetVersion(), Digest: binding.GetDigest(), Result: "failed", InputDigest: inputDigest,
			ErrorCode: "capability_call_unknown",
		}
		event.MutationDigest = CapabilityEventMutationDigest(event)
		principal, err := nextVerifiedPrincipal(principalSource)
		if err != nil {
			return err
		}
		if err := coordinator.store.RecordManagedAgentCapabilityEvent(ctx, scope.TenantID, principal, event); err != nil {
			return err
		}
	}
	return nil
}

func capabilityResourceKind(value string) ResourceKind {
	switch value {
	case "mcp-server":
		return ResourceMcpServer
	case "skill-bundle":
		return ResourceSkillBundle
	default:
		return ""
	}
}

type capabilityBindingKey struct {
	resource ResourceKind
	id       string
}

func indexCapabilityBindings(bindings []*workerruntimev1alpha1.RuntimeCapabilityBinding) map[capabilityBindingKey]*workerruntimev1alpha1.RuntimeCapabilityBinding {
	result := make(map[capabilityBindingKey]*workerruntimev1alpha1.RuntimeCapabilityBinding, len(bindings))
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		resource := capabilityResourceKind(binding.GetResourceKind())
		if resource != "" {
			result[capabilityBindingKey{resource: resource, id: binding.GetResourceId()}] = binding
		}
	}
	return result
}

func capabilityRuntimeEventResource(payload map[string]any) ResourceKind {
	itemType, _ := payload["itemType"].(string)
	data, _ := payload["data"].(map[string]any)
	sourceItemType, _ := data["sourceItemType"].(string)
	switch {
	case itemType == "mcp_tool_call":
		return ResourceMcpServer
	case itemType == "dynamic_tool_call" && sourceItemType == "skill":
		return ResourceSkillBundle
	default:
		return ""
	}
}

func capabilityFailureOperation(resource ResourceKind) string {
	if resource == ResourceSkillBundle {
		return "skill.fail"
	}
	return "mcp.fail"
}
