ALTER TABLE cloud_agents.managed_agent_sessions
    ADD COLUMN workspace_uid text,
    ADD COLUMN sandbox_uid text,
    ADD COLUMN sandbox_generation bigint,
    ADD COLUMN environment_profile_uid text,
    ADD COLUMN environment_profile_version bigint;

ALTER TABLE cloud_agents.managed_agent_sessions
    ADD CONSTRAINT managed_agent_sessions_foundation_binding CHECK (
        (workspace_uid IS NULL AND sandbox_uid IS NULL AND sandbox_generation IS NULL)
        OR (cloud_agents.is_valid_identifier(workspace_uid)
            AND cloud_agents.is_valid_identifier(sandbox_uid)
            AND sandbox_generation > 0)
    ),
    ADD CONSTRAINT managed_agent_sessions_profile_binding CHECK (
        (environment_profile_uid IS NULL AND environment_profile_version IS NULL)
        OR (cloud_agents.is_valid_identifier(environment_profile_uid)
            AND environment_profile_version BETWEEN 1 AND 2147483647)
    ),
    ADD CONSTRAINT managed_agent_sessions_single_environment CHECK (
        environment_lease_uid IS NULL OR sandbox_uid IS NULL
    ),
    ADD CONSTRAINT managed_agent_sessions_foundation_profile CHECK (
        sandbox_uid IS NULL OR environment_profile_uid IS NOT NULL
    ),
    ADD CONSTRAINT managed_agent_sessions_workspace_fk
        FOREIGN KEY (tenant_id, project_uid, workspace_uid)
        REFERENCES cloud_agents.workspaces (tenant_id, project_uid, workspace_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    ADD CONSTRAINT managed_agent_sessions_sandbox_fk
        FOREIGN KEY (tenant_id, project_uid, sandbox_uid)
        REFERENCES cloud_agents.sandbox_sessions (tenant_id, project_uid, sandbox_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    ADD CONSTRAINT managed_agent_sessions_profile_fk
        FOREIGN KEY (tenant_id, project_uid, environment_profile_uid, environment_profile_version)
        REFERENCES cloud_agents.environment_profiles (tenant_id, project_uid, profile_uid, profile_version)
        ON UPDATE RESTRICT ON DELETE RESTRICT;

CREATE FUNCTION cloud_agents.create_managed_agent_session_v4(
    p_tenant_id text, p_project_uid text, p_session_uid text, p_provider_kind text,
    p_environment_lease_uid text, p_workspace_uid text, p_sandbox_uid text,
    p_sandbox_generation bigint, p_environment_profile_uid text,
    p_environment_profile_version bigint, p_idempotency_key text, p_request_digest text
)
RETURNS TABLE (
    session_uid text, provider_kind text, environment_lease_uid text,
    environment_generation bigint, workspace_uid text, sandbox_uid text,
    sandbox_generation bigint, environment_profile_uid text,
    environment_profile_version bigint, state text, resource_version bigint,
    created_at timestamptz, updated_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    ignored_principal text;
    mutation_at timestamptz := pg_catalog.transaction_timestamp();
    existing cloud_agents.managed_agent_sessions%ROWTYPE;
    selected_sandbox cloud_agents.sandbox_sessions%ROWTYPE;
    selected_profile cloud_agents.environment_profiles%ROWTYPE;
    selected_target text;
    target_kind text;
    target_phase text;
    target_scheduling text;
    volume_state text;
    operation_state text;
    operation_cleanup text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project_uid)
        OR NOT cloud_agents.is_valid_identifier(p_session_uid)
        OR NOT cloud_agents.is_valid_identifier(p_provider_kind)
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR (
            cloud_agents.is_valid_identifier(p_environment_lease_uid)
            AND p_workspace_uid IS NULL AND p_sandbox_uid IS NULL
            AND p_sandbox_generation IS NULL AND p_environment_profile_uid IS NULL
            AND p_environment_profile_version IS NULL
            OR p_environment_lease_uid IS NULL
            AND cloud_agents.is_valid_identifier(p_workspace_uid)
            AND cloud_agents.is_valid_identifier(p_sandbox_uid)
            AND p_sandbox_generation > 0
            AND cloud_agents.is_valid_identifier(p_environment_profile_uid)
            AND p_environment_profile_version BETWEEN 1 AND 2147483647
        ) IS NOT TRUE
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'managed agent session input is invalid';
    END IF;

    SELECT session.* INTO existing
    FROM cloud_agents.managed_agent_sessions AS session
    WHERE session.tenant_id = p_tenant_id AND session.project_uid = p_project_uid
        AND session.create_idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_request_digest
            OR existing.environment_lease_uid IS DISTINCT FROM p_environment_lease_uid
            OR existing.workspace_uid IS DISTINCT FROM p_workspace_uid
            OR existing.sandbox_uid IS DISTINCT FROM p_sandbox_uid
            OR existing.sandbox_generation IS DISTINCT FROM p_sandbox_generation
            OR existing.environment_profile_uid IS DISTINCT FROM p_environment_profile_uid
            OR existing.environment_profile_version IS DISTINCT FROM p_environment_profile_version
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent session idempotency conflict';
        END IF;
        RETURN QUERY SELECT existing.session_uid, existing.provider_kind,
            existing.environment_lease_uid, existing.environment_generation,
            existing.workspace_uid, existing.sandbox_uid, existing.sandbox_generation,
            COALESCE(existing.environment_profile_uid, environment.environment_profile_uid),
            COALESCE(existing.environment_profile_version, environment.environment_profile_version),
            existing.state, existing.resource_version, existing.created_at, existing.updated_at
        FROM (SELECT 1) AS replay
        LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
          ON environment.tenant_id = existing.tenant_id AND environment.project_uid = existing.project_uid
         AND environment.lease_uid = existing.environment_lease_uid;
        RETURN;
    END IF;

    IF p_environment_lease_uid IS NOT NULL THEN
        RETURN QUERY
        SELECT created.session_uid, created.provider_kind, created.environment_lease_uid,
            created.environment_generation, NULL::text, NULL::text, NULL::bigint,
            created.environment_profile_uid, created.environment_profile_version,
            created.state, created.resource_version, created.created_at, created.updated_at
        FROM cloud_agents.create_managed_agent_session_v3(
            p_tenant_id, p_project_uid, p_session_uid, p_provider_kind,
            p_environment_lease_uid, p_idempotency_key, p_request_digest
        ) AS created;
        RETURN;
    END IF;

    SELECT sandbox.* INTO selected_sandbox
    FROM cloud_agents.sandbox_sessions AS sandbox
    WHERE sandbox.tenant_id = p_tenant_id AND sandbox.project_uid = p_project_uid
        AND sandbox.sandbox_uid = p_sandbox_uid
    FOR SHARE;
    IF NOT FOUND OR selected_sandbox.workspace_uid IS DISTINCT FROM p_workspace_uid
        OR selected_sandbox.generation IS DISTINCT FROM p_sandbox_generation
        OR selected_sandbox.desired_state <> 'running' OR selected_sandbox.observed_state <> 'running'
        OR selected_sandbox.observed_generation IS DISTINCT FROM selected_sandbox.generation
        OR selected_sandbox.writer_released OR selected_sandbox.runtime_uid IS NULL
        OR selected_sandbox.runtime_state <> 'Running'
        OR selected_sandbox.runtime_generation IS DISTINCT FROM selected_sandbox.generation
        OR selected_sandbox.runtime_spec_digest IS DISTINCT FROM selected_sandbox.spec_digest
        OR selected_sandbox.expires_at IS NOT NULL AND selected_sandbox.expires_at <= mutation_at
    THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent Session sandbox is not ready';
    END IF;

    SELECT volume.target_uid, volume.observed_state, target.target_kind,
        target.observed_phase, target.scheduling_state
    INTO selected_target, volume_state, target_kind, target_phase, target_scheduling
    FROM cloud_agents.workspace_volumes AS volume
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    WHERE volume.tenant_id = p_tenant_id AND volume.project_uid = p_project_uid
        AND volume.workspace_uid = p_workspace_uid
    FOR SHARE OF volume, target;
    IF NOT FOUND OR volume_state <> 'available' OR target_phase <> 'ready'
        OR target_scheduling <> 'active'
        OR target_kind NOT IN ('docker', 'kubernetes', 'remote-worker')
    THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent Session target is not available';
    END IF;

    SELECT operation.state, operation.cleanup_phase INTO operation_state, operation_cleanup
    FROM cloud_agents.platform_operations AS operation
    WHERE operation.tenant_id = p_tenant_id
        AND operation.operation_id = selected_sandbox.operation_id
        AND operation.operation_generation = selected_sandbox.operation_generation
    FOR SHARE;
    IF NOT FOUND OR operation_state <> 'succeeded' OR operation_cleanup <> 'complete' THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent Session sandbox operation is incomplete';
    END IF;

    SELECT profile.* INTO selected_profile
    FROM cloud_agents.environment_profiles AS profile
    JOIN cloud_agents.worker_releases AS release
      ON release.tenant_id = profile.tenant_id AND release.project_uid = profile.project_uid
     AND release.release_digest = profile.release_digest
     AND release.status = 'approved' AND release.verification_state = 'attested'
    WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
        AND profile.profile_uid = p_environment_profile_uid
        AND profile.profile_version = p_environment_profile_version
        AND profile.status = 'published' AND p_provider_kind = ANY(profile.provider_kinds)
        AND selected_target = ANY(profile.target_refs)
        AND selected_sandbox.image_uri LIKE '%@' || profile.release_digest
        AND selected_sandbox.cpu_millis >= profile.cpu_limit_millis
        AND selected_sandbox.memory_bytes >= profile.memory_limit_bytes
        AND EXISTS (
            SELECT 1 FROM cloud_agents.runtime_profiles AS runtime_profile
            WHERE runtime_profile.tenant_id = selected_sandbox.tenant_id
                AND runtime_profile.project_uid = selected_sandbox.project_uid
                AND runtime_profile.profile_uid = selected_sandbox.runtime_profile_uid
                AND runtime_profile.profile_version = selected_sandbox.runtime_profile_version
                AND runtime_profile.status = 'published'
                AND runtime_profile.network_policy_ref = profile.network_policy_ref
        )
    FOR SHARE OF profile, release;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent Session profile is not compatible with sandbox';
    END IF;

    INSERT INTO cloud_agents.managed_agent_sessions (
        tenant_id, tenant_ref_id, project_uid, session_uid, provider_kind,
        workspace_uid, sandbox_uid, sandbox_generation,
        environment_profile_uid, environment_profile_version,
        state, resource_version, create_idempotency_key, create_request_digest,
        created_at, updated_at
    ) VALUES (
        p_tenant_id, p_tenant_id, p_project_uid, p_session_uid, p_provider_kind,
        p_workspace_uid, p_sandbox_uid, p_sandbox_generation,
        p_environment_profile_uid, p_environment_profile_version,
        'active', 1, p_idempotency_key, p_request_digest, mutation_at, mutation_at
    ) RETURNING managed_agent_sessions.* INTO existing;

    RETURN QUERY SELECT existing.session_uid, existing.provider_kind,
        existing.environment_lease_uid, existing.environment_generation,
        existing.workspace_uid, existing.sandbox_uid, existing.sandbox_generation,
        existing.environment_profile_uid, existing.environment_profile_version,
        existing.state, existing.resource_version, existing.created_at, existing.updated_at;
END;
$cloud_agents_function$;

CREATE OR REPLACE FUNCTION cloud_agents.guard_managed_agent_target_admission_v1()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    lease_id text;
    lease_generation bigint;
    sandbox_id text;
    sandbox_generation bigint;
    workspace_id text;
    target_id text;
    scheduling_state text;
    environment cloud_agents.managed_host_environment_leases%ROWTYPE;
    sandbox cloud_agents.sandbox_sessions%ROWTYPE;
BEGIN
    IF TG_OP = 'UPDATE' AND (NEW.state NOT IN ('queued', 'running') OR NEW.state = OLD.state) THEN
        RETURN NEW;
    END IF;
    IF TG_TABLE_NAME = 'managed_agent_sessions' THEN
        lease_id := NEW.environment_lease_uid;
        lease_generation := NEW.environment_generation;
        sandbox_id := NEW.sandbox_uid;
        sandbox_generation := NEW.sandbox_generation;
        workspace_id := NEW.workspace_uid;
    ELSE
        SELECT session.environment_lease_uid, session.environment_generation,
            session.sandbox_uid, session.sandbox_generation, session.workspace_uid
        INTO lease_id, lease_generation, sandbox_id, sandbox_generation, workspace_id
        FROM cloud_agents.managed_agent_sessions AS session
        WHERE session.tenant_id = NEW.tenant_id AND session.project_uid = NEW.project_uid
            AND session.session_uid = NEW.session_uid;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'managed agent session is absent';
        END IF;
    END IF;
    IF lease_id IS NULL AND sandbox_id IS NULL THEN RETURN NEW; END IF;

    IF lease_id IS NOT NULL THEN
        SELECT lease.deployment_target_uid INTO target_id
        FROM cloud_agents.managed_host_environment_leases AS lease
        WHERE lease.tenant_id = NEW.tenant_id AND lease.project_uid = NEW.project_uid
            AND lease.lease_uid = lease_id;
        IF target_id IS NULL THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent environment is unavailable';
        END IF;
        SELECT target.scheduling_state INTO scheduling_state
        FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = NEW.tenant_id AND target.project_uid = NEW.project_uid
            AND target.target_uid = target_id
        FOR SHARE;
        IF NOT FOUND OR scheduling_state <> 'active' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent target is drained';
        END IF;
        SELECT lease.* INTO environment
        FROM cloud_agents.managed_host_environment_leases AS lease
        WHERE lease.tenant_id = NEW.tenant_id AND lease.project_uid = NEW.project_uid
            AND lease.lease_uid = lease_id
        FOR SHARE;
        IF NOT FOUND OR environment.deployment_target_uid IS DISTINCT FROM target_id
            OR environment.generation IS DISTINCT FROM lease_generation
            OR environment.desired_phase <> 'active' OR environment.observed_phase <> 'ready'
            OR environment.cleanup_phase <> 'none'
            OR environment.expires_at <= pg_catalog.clock_timestamp()
        THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent environment is unavailable';
        END IF;
        RETURN NEW;
    END IF;

    SELECT current_sandbox.* INTO sandbox
    FROM cloud_agents.sandbox_sessions AS current_sandbox
    WHERE current_sandbox.tenant_id = NEW.tenant_id
        AND current_sandbox.project_uid = NEW.project_uid
        AND current_sandbox.sandbox_uid = sandbox_id
    FOR SHARE;
    IF NOT FOUND OR sandbox.workspace_uid IS DISTINCT FROM workspace_id
        OR sandbox.generation IS DISTINCT FROM sandbox_generation
        OR sandbox.desired_state <> 'running' OR sandbox.observed_state <> 'running'
        OR sandbox.observed_generation IS DISTINCT FROM sandbox.generation
        OR sandbox.writer_released OR sandbox.runtime_uid IS NULL
        OR sandbox.runtime_state <> 'Running'
        OR sandbox.runtime_generation IS DISTINCT FROM sandbox.generation
        OR sandbox.runtime_spec_digest IS DISTINCT FROM sandbox.spec_digest
        OR sandbox.expires_at IS NOT NULL AND sandbox.expires_at <= pg_catalog.clock_timestamp()
    THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent sandbox is unavailable';
    END IF;
    SELECT target.target_uid, target.scheduling_state INTO target_id, scheduling_state
    FROM cloud_agents.workspace_volumes AS volume
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    WHERE volume.tenant_id = NEW.tenant_id AND volume.project_uid = NEW.project_uid
        AND volume.workspace_uid = workspace_id AND volume.observed_state = 'available'
    FOR SHARE OF volume, target;
    IF NOT FOUND OR scheduling_state <> 'active' THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'managed agent target is drained';
    END IF;
    RETURN NEW;
END;
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.create_managed_agent_session_v4(
    text, text, text, text, text, text, text, bigint, text, bigint, text, text
) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.guard_managed_agent_target_admission_v1()
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.create_managed_agent_session_v4(
    text, text, text, text, text, text, text, bigint, text, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.guard_managed_agent_target_admission_v1() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_managed_agent_session_v4(
    text, text, text, text, text, text, text, bigint, text, bigint, text, text
) TO cloud_agents_runtime;

-- Keep database validation aligned with the bytewise canonical order produced
-- by the Control Plane when wildcard and exact DNS targets are mixed.
CREATE OR REPLACE FUNCTION cloud_agents.is_valid_network_egress_list_v1(
    p_values text[], default_egress text, allowlist_ref text
)
RETURNS boolean
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE target text; previous text;
BEGIN
    IF p_values IS NULL OR pg_catalog.cardinality(p_values) > 64
        OR default_egress NOT IN ('public', 'restricted', 'deny') THEN
        RETURN false;
    END IF;
    FOREACH target IN ARRAY p_values LOOP
        IF NOT cloud_agents.is_valid_network_egress_target_v1(target)
            OR previous IS NOT NULL AND target COLLATE "C" <= previous COLLATE "C" THEN
            RETURN false;
        END IF;
        previous := target;
    END LOOP;
    RETURN (default_egress = 'restricted' OR pg_catalog.cardinality(p_values) = 0)
        AND (default_egress <> 'restricted' OR pg_catalog.cardinality(p_values) > 0 OR allowlist_ref IS NOT NULL);
END;
$cloud_agents_function$;

-- Foundation Agent EnvironmentProfiles use the same durable catalog as legacy
-- leases, but their Sandbox adapter enforces restricted egress itself.
CREATE OR REPLACE FUNCTION cloud_agents.transition_environment_profile_v4(
    p_tenant_id text, p_project_uid text, p_profile_uid text, p_profile_version bigint,
    p_expected_resource_version bigint, p_action text, p_idempotency_key text,
    p_request_digest text, p_request_id text, p_subject_digest text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, provider_kinds text[], cpu_limit_millis bigint,
    memory_limit_bytes bigint, storage_policy_ref text, network_policy_ref text,
    release_digest text, target_refs text[], provider_credential_ref text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz,
    published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE ignored_principal text; referenced_policy text;
BEGIN
    ignored_principal := cloud_agents.require_runtime_mutation_principal();
    IF p_action = 'publish' THEN
        SELECT profile.network_policy_ref INTO referenced_policy
        FROM cloud_agents.environment_profiles AS profile
        WHERE profile.tenant_id = p_tenant_id AND profile.project_uid = p_project_uid
            AND profile.profile_uid = p_profile_uid AND profile.profile_version = p_profile_version
        FOR SHARE;
        IF FOUND THEN
            PERFORM 1 FROM cloud_agents.network_policies AS policy
            WHERE policy.tenant_id = p_tenant_id AND policy.project_uid = p_project_uid
                AND policy.policy_uid = referenced_policy
                AND (policy.default_egress = 'public'
                    OR policy.default_egress = 'restricted' AND pg_catalog.cardinality(policy.allowed_egress) > 0)
                AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
                AND policy.proxy_policy_ref IS NULL
                AND NOT policy.ingress_enabled AND NOT policy.preview_enabled
            FOR SHARE;
            IF NOT FOUND THEN
                RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'network policy is not available';
            END IF;
        END IF;
    END IF;
    RETURN QUERY SELECT * FROM cloud_agents.transition_environment_profile_v3(
        p_tenant_id, p_project_uid, p_profile_uid, p_profile_version,
        p_expected_resource_version, p_action, p_idempotency_key,
        p_request_digest, p_request_id, p_subject_digest
    );
END;
$cloud_agents_function$;
