-- Append-only successor for durable Project-create identifiers.
-- 000013 remains immutable; CREATE OR REPLACE preserves its signature, ACL,
-- stored rows, and replay behavior while hardening only new identifiers.
CREATE OR REPLACE FUNCTION cloud_agents.create_managed_agent_project_durable_v1(
    p_tenant_id text,
    p_subject_digest text,
    p_idempotency_key text,
    p_request_digest text,
    p_project_uid text,
    p_project_name text,
    p_organization_uid text,
    p_display_name text,
    p_audit_fact_id text
)
RETURNS TABLE (
    disposition text,
    replay_state text,
    operation_id text,
    operation_generation bigint,
    resource_kind text,
    resource_id text,
    resource_version bigint,
    stable_error_code text,
    outbox_event_id text,
    outbox_state text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    mutation_at timestamptz;
    stored cloud_agents.idempotency_records%ROWTYPE;
    inserted_count bigint;
    expected_revision bigint;
    next_revision bigint;
    operation_suffix text;
    event_suffix text;
    derived_operation_id text;
    derived_event_id text;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.transaction_timestamp();

    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.char_length(p_idempotency_key) NOT BETWEEN 16 AND 128
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]+$'
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_project_name)
        OR NOT cloud_agents.is_valid_identifier(p_organization_uid)
        OR pg_catalog.char_length(p_display_name) NOT BETWEEN 1 AND 160
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_id)
        OR NOT cloud_agents.coordination_registry_profile_is_registered(
            cloud_agents.coordination_project_create_registry_digest(),
            'managedAgentCreateProjectDurable/v1alpha1',
            'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f')
        OR NOT cloud_agents.coordination_profile_creates_operation(
            'managedAgentCreateProjectDurable/v1alpha1',
            'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f')
        OR cloud_agents.coordination_profile_outbox_class(
            'managedAgentCreateProjectDurable/v1alpha1',
            'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f') <> 'operation_effect'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'durable project create input is invalid';
    END IF;

    INSERT INTO cloud_agents.idempotency_records (
        tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id,
        profile_digest, idempotency_key, request_digest, state, created_at,
        updated_at, expires_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_subject_digest,
        cloud_agents.coordination_project_create_registry_digest(),
        'managedAgentCreateProjectDurable/v1alpha1',
        'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f',
        p_idempotency_key, p_request_digest, 'pending', mutation_at, mutation_at,
        mutation_at + pg_catalog.make_interval(secs => cloud_agents.coordination_profile_replay_ttl_seconds(
            'managedAgentCreateProjectDurable/v1alpha1',
            'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f')::integer)
    ) ON CONFLICT (
        tenant_id, subject_digest, profile_id, profile_digest, idempotency_key
    ) DO NOTHING;
    GET DIAGNOSTICS inserted_count = ROW_COUNT;

    SELECT record.* INTO STRICT stored
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = p_tenant_id
        AND record.subject_digest = p_subject_digest
        AND record.profile_id = 'managedAgentCreateProjectDurable/v1alpha1'
        AND record.profile_digest = 'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f'
        AND record.idempotency_key = p_idempotency_key
    FOR UPDATE;

    IF inserted_count = 0 AND stored.request_digest IS DISTINCT FROM p_request_digest THEN
        disposition := 'conflict';
        RETURN NEXT;
        RETURN;
    END IF;
    IF inserted_count = 0 THEN
        disposition := 'replay';
        replay_state := stored.state;
        operation_id := stored.operation_id;
        operation_generation := stored.operation_generation;
        resource_kind := stored.resource_kind;
        resource_id := stored.resource_id;
        resource_version := stored.resource_version;
        stable_error_code := stored.stable_error_code;
        RETURN NEXT;
        RETURN;
    END IF;

    PERFORM 1
    FROM cloud_agents.organizations AS organization
    WHERE organization.tenant_id = p_tenant_id
        AND organization.organization_uid = p_organization_uid
        AND organization.state = 'active'
    FOR KEY SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'organization is absent or inactive';
    END IF;

    SELECT revision.current_revision INTO expected_revision
    FROM cloud_agents.tenant_resource_versions AS revision
    WHERE revision.tenant_id = p_tenant_id AND revision.tenant_uid = p_tenant_id
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'tenant revision root is absent';
    END IF;
    next_revision := cloud_agents.allocate_tenant_revision(p_tenant_id, expected_revision, mutation_at);

    INSERT INTO cloud_agents.resource_changes (
        tenant_id, tenant_uid, resource_version, resource_kind, resource_uid,
        change_kind, actor_database_principal, occurred_at
    ) VALUES (
        p_tenant_id, p_tenant_id, next_revision, 'project', p_project_uid,
        'created', actor_principal, mutation_at
    );
    INSERT INTO cloud_agents.projects (
        tenant_id, tenant_ref_id, project_uid, project_name, organization_uid,
        display_name, state, resource_version, created_at, updated_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_project_name, p_organization_uid,
        p_display_name, 'active', next_revision, mutation_at, mutation_at
    );

    -- 000013 used a 32-character MD5 suffix.  This append-only successor keeps
    -- every existing row/replay untouched while using full-width SHA-256 ids for
    -- new writes.  Each domain-separated frame is length-prefixed before UTF-8
    -- encoding, so field boundaries cannot collide.
    operation_suffix := pg_catalog.encode(
        pg_catalog.sha256(
            pg_catalog.convert_to(
                pg_catalog.octet_length('cloud-agents/durable-project-create/operation-id/v1')::text || ':'
                    || 'cloud-agents/durable-project-create/operation-id/v1'
                    || pg_catalog.octet_length(p_subject_digest)::text || ':' || p_subject_digest
                    || pg_catalog.octet_length(p_idempotency_key)::text || ':' || p_idempotency_key
                    || pg_catalog.octet_length(p_request_digest)::text || ':' || p_request_digest,
                'UTF8')),
        'hex');
    event_suffix := pg_catalog.encode(
        pg_catalog.sha256(
            pg_catalog.convert_to(
                pg_catalog.octet_length('cloud-agents/durable-project-create/event-id/v1')::text || ':'
                    || 'cloud-agents/durable-project-create/event-id/v1'
                    || pg_catalog.octet_length(p_subject_digest)::text || ':' || p_subject_digest
                    || pg_catalog.octet_length(p_idempotency_key)::text || ':' || p_idempotency_key
                    || pg_catalog.octet_length(p_request_digest)::text || ':' || p_request_digest,
                'UTF8')),
        'hex');
    derived_operation_id := 'project-create-' || operation_suffix;
    derived_event_id := 'project-create-event-' || event_suffix;

    INSERT INTO cloud_agents.platform_operations (
        tenant_id, tenant_ref_id, operation_id, operation_generation,
        registry_digest, state_machine_digest, policy_digest, profile_id,
        profile_digest, subject_digest, request_digest, state, cleanup_phase,
        recovery_generation, current_attempt_number, terminal_resource_kind,
        terminal_resource_id, terminal_resource_version, created_at, updated_at,
        terminal_at
    ) VALUES (
        p_tenant_id, p_tenant_id, derived_operation_id, 1,
        cloud_agents.coordination_project_create_registry_digest(),
        cloud_agents.coordination_state_machine_digest(),
        cloud_agents.coordination_policy_digest(),
        'managedAgentCreateProjectDurable/v1alpha1',
        'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f',
        p_subject_digest, p_request_digest, 'succeeded', 'complete', 0, 1,
        'project', p_project_uid, next_revision, mutation_at, mutation_at, mutation_at
    );
    INSERT INTO cloud_agents.operation_attempts (
        tenant_id, tenant_ref_id, operation_id, operation_generation,
        attempt_number, state, created_at, updated_at, terminal_at
    ) VALUES (
        p_tenant_id, p_tenant_id, derived_operation_id, 1, 1,
        'succeeded', mutation_at, mutation_at, mutation_at
    );
    INSERT INTO cloud_agents.terminal_receipts (
        tenant_id, tenant_ref_id, operation_id, operation_generation,
        attempt_number, receipt_id, outcome, resource_kind, resource_id,
        resource_version, persisted_at
    ) VALUES (
        p_tenant_id, p_tenant_id, derived_operation_id, 1, 1, 'success',
        'succeeded', 'project', p_project_uid, next_revision, mutation_at
    );
    INSERT INTO cloud_agents.operation_finalizers (
        tenant_id, tenant_ref_id, operation_id, operation_generation,
        finalizer_name, required, state, delivery_attempts, created_at,
        updated_at, terminal_at
    ) VALUES (
        p_tenant_id, p_tenant_id, derived_operation_id, 1, 'project-create',
        true, 'succeeded', 0, mutation_at, mutation_at, mutation_at
    );
    INSERT INTO cloud_agents.outbox_events (
        tenant_id, tenant_ref_id, event_id, registry_digest, profile_id,
        profile_digest, event_class, aggregate_kind, aggregate_id,
        aggregate_sequence, resource_version, generation, operation_id,
        operation_generation, payload_digest, state, delivery_attempts,
        created_at, updated_at
    ) VALUES (
        p_tenant_id, p_tenant_id, derived_event_id,
        cloud_agents.coordination_project_create_registry_digest(),
        'managedAgentCreateProjectDurable/v1alpha1',
        'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f',
        'operation_effect', 'project', p_project_uid, 1, NULL, 1,
        derived_operation_id, 1, p_request_digest, 'pending', 0, mutation_at, mutation_at
    );
    INSERT INTO cloud_agents.coordination_audit_facts (
        tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id,
        profile_digest, subject_digest, operation_id, operation_generation,
        attempt_number, resource_kind, resource_id, resource_version,
        transition, outcome, database_timestamp
    ) VALUES (
        p_tenant_id, p_tenant_id, p_audit_fact_id,
        cloud_agents.coordination_project_create_registry_digest(),
        'managedAgentCreateProjectDurable/v1alpha1',
        'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f',
        p_subject_digest, derived_operation_id, 1, 1, 'project', p_project_uid,
        next_revision, 'project.create', 'succeeded', mutation_at
    );
    UPDATE cloud_agents.idempotency_records AS record
    SET state = 'succeeded', operation_id = derived_operation_id,
        operation_generation = 1, resource_kind = 'project',
        resource_id = p_project_uid, resource_version = next_revision,
        updated_at = mutation_at, terminal_at = mutation_at
    WHERE record.tenant_id = p_tenant_id
        AND record.subject_digest = p_subject_digest
        AND record.profile_id = 'managedAgentCreateProjectDurable/v1alpha1'
        AND record.profile_digest = 'sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f'
        AND record.idempotency_key = p_idempotency_key;

    disposition := 'created';
    replay_state := 'succeeded';
    operation_id := derived_operation_id;
    operation_generation := 1;
    resource_kind := 'project';
    resource_id := p_project_uid;
    resource_version := next_revision;
    outbox_event_id := derived_event_id;
    outbox_state := 'pending';
    RETURN NEXT;
END
$cloud_agents_function$;
