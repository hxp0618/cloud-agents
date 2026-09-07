ALTER TABLE cloud_agents.runtime_profiles
    ADD COLUMN workload_trust text NOT NULL DEFAULT 'trusted-single-tenant',
    ADD COLUMN isolation_runtime text NOT NULL DEFAULT 'runc',
    ADD CONSTRAINT runtime_profiles_isolation CHECK (
        workload_trust IN ('trusted-single-tenant', 'dedicated-node', 'shared-untrusted')
        AND isolation_runtime IN ('runc', 'gvisor')
        AND (workload_trust = 'shared-untrusted') = (isolation_runtime = 'gvisor')
    );

CREATE OR REPLACE FUNCTION cloud_agents.is_valid_remote_worker_capabilities(p_values text[])
RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path = pg_catalog
AS $$
    SELECT pg_catalog.cardinality(p_values) BETWEEN 1 AND 16
       AND p_values <@ ARRAY[
           'dedicated-node','docker','exec','files','isolation-gvisor','network-dns-nft',
           'network-internal-deny','preview','pty','ssh','workspace-volume'
       ]::text[]
       AND p_values = ARRAY(
           SELECT DISTINCT value
           FROM pg_catalog.unnest(p_values) AS value
           ORDER BY value
       )
$$;

CREATE FUNCTION cloud_agents.foundation_target_supports_isolation_v1(
    p_tenant text, p_project text, p_target text, p_workload_trust text, p_isolation_runtime text
)
RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    SELECT (p_workload_trust = 'trusted-single-tenant' AND p_isolation_runtime = 'runc'
            OR p_workload_trust = 'dedicated-node' AND p_isolation_runtime = 'runc'
            OR p_workload_trust = 'shared-untrusted' AND p_isolation_runtime = 'gvisor')
       AND EXISTS (
           SELECT 1
           FROM cloud_agents.deployment_targets AS target
           LEFT JOIN cloud_agents.remote_worker_enrollments AS enrollment
             ON enrollment.tenant_id = target.tenant_id
            AND enrollment.project_uid = target.project_uid
            AND enrollment.target_uid = target.target_uid
            AND target.target_kind = 'remote-worker'
           WHERE target.tenant_id = p_tenant AND target.project_uid = p_project
             AND target.target_uid = p_target
             AND (p_workload_trust = 'trusted-single-tenant'
                 OR p_workload_trust = 'dedicated-node'
                    AND target.target_kind = 'remote-worker'
                    AND enrollment.node_capabilities @> ARRAY['dedicated-node']::text[]
                 OR p_workload_trust = 'shared-untrusted'
                    AND target.target_kind = 'remote-worker'
                    AND enrollment.node_capabilities @> ARRAY[
                        'isolation-gvisor','network-internal-deny'
                    ]::text[])
       )
$$;

CREATE FUNCTION cloud_agents.foundation_select_remote_worker_target_v2(
    p_tenant text, p_project text, p_region text, p_pool text,
    p_runtime text, p_architecture text, p_workload_trust text, p_isolation_runtime text,
    p_cpu bigint, p_memory bigint, p_workspace text
)
RETURNS text
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    SELECT target.target_uid
    FROM cloud_agents.deployment_targets AS target
    JOIN cloud_agents.remote_worker_enrollments AS enrollment
      ON enrollment.tenant_id = target.tenant_id
     AND enrollment.project_uid = target.project_uid
     AND enrollment.target_uid = target.target_uid
    JOIN LATERAL cloud_agents.foundation_remote_worker_capacity_v1(
        target.tenant_id, target.project_uid, target.target_uid
    ) AS capacity ON true
    WHERE p_region = 'region-local' AND p_pool = 'pool-remote-worker'
      AND p_runtime = 'docker' AND p_architecture IN ('amd64', 'arm64')
      AND target.tenant_id = p_tenant AND target.project_uid = p_project
      AND target.target_kind = 'remote-worker'
      AND enrollment.node_architecture = p_architecture
      AND cloud_agents.foundation_target_supports_isolation_v1(
          p_tenant, p_project, target.target_uid, p_workload_trust, p_isolation_runtime
      )
      AND cloud_agents.foundation_target_can_reserve_v1(
          p_tenant, p_project, target.target_uid, p_cpu, p_memory, p_workspace
      )
      AND (p_workspace = '' OR NOT EXISTS (
              SELECT 1 FROM cloud_agents.workspace_volumes AS volume
              WHERE volume.tenant_id = p_tenant AND volume.project_uid = p_project
                AND volume.workspace_uid = p_workspace
          ) OR EXISTS (
              SELECT 1 FROM cloud_agents.workspace_volumes AS volume
              WHERE volume.tenant_id = p_tenant AND volume.project_uid = p_project
                AND volume.workspace_uid = p_workspace AND volume.target_uid = target.target_uid
          ))
    ORDER BY capacity.available_cpu_millis DESC,
        capacity.available_memory_bytes DESC,
        capacity.available_disk_bytes DESC,
        target.target_uid
    LIMIT 1
$$;

CREATE FUNCTION cloud_agents.foundation_runtime_profile_available_v2(
    p_tenant text, p_project text, p_target text, p_region text, p_pool text,
    p_runtime text, p_architecture text, p_workload_trust text, p_isolation_runtime text,
    p_network_policy text, p_cpu bigint, p_memory bigint, p_workspace text
)
RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.network_policies AS policy
        WHERE policy.tenant_id = p_tenant AND policy.project_uid = p_project
          AND policy.policy_uid = p_network_policy
          AND policy.default_egress IN ('restricted', 'deny')
          AND (policy.default_egress = 'restricted' AND pg_catalog.cardinality(policy.allowed_egress) > 0
              OR policy.default_egress = 'deny' AND pg_catalog.cardinality(policy.allowed_egress) = 0)
          AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
          AND policy.proxy_policy_ref IS NULL AND NOT policy.ingress_enabled
          AND (p_isolation_runtime <> 'gvisor'
              OR policy.default_egress = 'deny' AND NOT policy.preview_enabled)
    ) AND CASE WHEN p_target <> '' THEN
        cloud_agents.foundation_target_supports_isolation_v1(
            p_tenant, p_project, p_target, p_workload_trust, p_isolation_runtime
        ) AND cloud_agents.foundation_target_can_reserve_v1(
            p_tenant, p_project, p_target, p_cpu, p_memory, p_workspace
        )
    ELSE cloud_agents.foundation_select_remote_worker_target_v2(
        p_tenant, p_project, p_region, p_pool, p_runtime, p_architecture,
        p_workload_trust, p_isolation_runtime, p_cpu, p_memory, p_workspace
    ) IS NOT NULL END
$$;

CREATE FUNCTION cloud_agents.create_runtime_profile_draft_v4(
    p_tenant text, p_project text, p_profile text, p_name text, p_version bigint,
    p_description text, p_workload_trust text, p_isolation_runtime text,
    p_target text, p_region text, p_pool text, p_runtime text, p_architecture text,
    p_network_policy text, p_image text, p_release text, p_cpu bigint, p_memory bigint,
    p_key text, p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, workload_trust text, isolation_runtime text,
    target_uid text, target_region_uid text, target_resource_pool_uid text,
    target_runtime text, target_architecture text, network_policy_ref text,
    image_uri text, release_digest text, cpu_millis bigint, memory_bytes bigint,
    resource_version bigint, created_at timestamptz, updated_at timestamptz,
    published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    existing cloud_agents.runtime_profiles%ROWTYPE;
BEGIN
    IF NOT (
        p_workload_trust IN ('trusted-single-tenant', 'dedicated-node', 'shared-untrusted')
        AND p_isolation_runtime IN ('runc', 'gvisor')
        AND (p_workload_trust = 'shared-untrusted') = (p_isolation_runtime = 'gvisor')
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'runtime profile isolation is invalid';
    END IF;
    PERFORM 1 FROM cloud_agents.create_runtime_profile_draft_v3(
        p_tenant, p_project, p_profile, p_name, p_version, p_description,
        p_target, p_region, p_pool, p_runtime, p_architecture, p_network_policy,
        p_image, p_release, p_cpu, p_memory, p_key, p_digest, p_request, p_subject
    );
    UPDATE cloud_agents.runtime_profiles AS profile
    SET workload_trust = p_workload_trust, isolation_runtime = p_isolation_runtime
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
      AND profile.profile_uid = p_profile AND profile.profile_version = p_version
      AND (profile.workload_trust = 'trusted-single-tenant' AND profile.isolation_runtime = 'runc'
          OR profile.workload_trust = p_workload_trust AND profile.isolation_runtime = p_isolation_runtime)
    RETURNING profile.* INTO existing;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
    END IF;
    RETURN QUERY SELECT existing.profile_version_uid, existing.profile_uid,
        existing.profile_name, existing.profile_version, existing.description, existing.status,
        existing.workload_trust, existing.isolation_runtime, COALESCE(existing.target_uid, ''),
        existing.target_region_uid, existing.target_resource_pool_uid, existing.target_runtime,
        existing.target_architecture, COALESCE(existing.network_policy_ref, ''), existing.image_uri,
        existing.release_digest, existing.cpu_millis, existing.memory_bytes,
        existing.resource_version, existing.created_at, existing.updated_at,
        existing.published_at, existing.disabled_at;
END;
$$;

CREATE FUNCTION cloud_agents.transition_runtime_profile_v4(
    p_tenant text, p_project text, p_profile text, p_version bigint,
    p_expected_resource_version bigint, p_action text, p_key text,
    p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, workload_trust text, isolation_runtime text,
    target_uid text, target_region_uid text, target_resource_pool_uid text,
    target_runtime text, target_architecture text, network_policy_ref text,
    image_uri text, release_digest text, cpu_millis bigint, memory_bytes bigint,
    resource_version bigint, created_at timestamptz, updated_at timestamptz,
    published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    existing cloud_agents.runtime_profiles%ROWTYPE;
BEGIN
    SELECT profile.* INTO existing
    FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
      AND profile.profile_uid = p_profile AND profile.profile_version = p_version;
    IF p_action = 'publish' AND FOUND AND NOT cloud_agents.foundation_runtime_profile_available_v2(
        existing.tenant_id, existing.project_uid, COALESCE(existing.target_uid, ''),
        existing.target_region_uid, existing.target_resource_pool_uid,
        existing.target_runtime, existing.target_architecture,
        existing.workload_trust, existing.isolation_runtime, existing.network_policy_ref,
        existing.cpu_millis, existing.memory_bytes, ''
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;
    PERFORM 1 FROM cloud_agents.transition_runtime_profile_v3(
        p_tenant, p_project, p_profile, p_version, p_expected_resource_version,
        p_action, p_key, p_digest, p_request, p_subject
    );
    SELECT profile.* INTO STRICT existing
    FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
      AND profile.profile_uid = p_profile AND profile.profile_version = p_version;
    RETURN QUERY SELECT existing.profile_version_uid, existing.profile_uid,
        existing.profile_name, existing.profile_version, existing.description, existing.status,
        existing.workload_trust, existing.isolation_runtime, COALESCE(existing.target_uid, ''),
        existing.target_region_uid, existing.target_resource_pool_uid, existing.target_runtime,
        existing.target_architecture, COALESCE(existing.network_policy_ref, ''), existing.image_uri,
        existing.release_digest, existing.cpu_millis, existing.memory_bytes,
        existing.resource_version, existing.created_at, existing.updated_at,
        existing.published_at, existing.disabled_at;
END;
$$;

CREATE OR REPLACE FUNCTION cloud_agents.accept_foundation_sandbox_v1(
    p_project text, p_workspace text, p_workspace_name text, p_sandbox text,
    p_profile text, p_profile_version bigint, p_subject text, p_key text, p_digest text
)
RETURNS TABLE (
    operation_uid text, workspace_uid text, sandbox_uid text, profile_uid text,
    profile_version bigint, generation bigint, desired_state text, observed_state text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    selected cloud_agents.runtime_profiles%ROWTYPE;
    selected_target text;
    operation_value text;
    event_value text;
    prior_digest text;
    prior_operation text;
    replay boolean := false;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_workspace)
        OR NOT cloud_agents.is_valid_identifier(p_workspace_name) OR NOT cloud_agents.is_valid_identifier(p_sandbox)
        OR NOT cloud_agents.is_valid_identifier(p_profile) OR p_profile_version NOT BETWEEN 1 AND 2147483647
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation sandbox input is invalid'; END IF;
    SELECT record.request_digest, record.operation_id INTO prior_digest, prior_operation
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = cloud_agents.require_tenant_id() AND record.subject_digest = p_subject
        AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        AND record.idempotency_key = p_key;
    IF FOUND THEN
        replay := true;
        IF prior_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox idempotency conflict';
        END IF;
    END IF;
    SELECT profile.* INTO selected FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = cloud_agents.require_tenant_id() AND profile.project_uid = p_project
        AND profile.profile_uid = p_profile AND profile.profile_version = p_profile_version FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile was not found'; END IF;
    IF NOT replay AND selected.status <> 'published' THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile is not available';
    END IF;
    IF replay THEN
        SELECT volume.target_uid INTO selected_target
        FROM cloud_agents.sandbox_sessions AS sandbox
        JOIN cloud_agents.workspace_volumes AS volume
          ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
         AND volume.workspace_uid = sandbox.workspace_uid
        WHERE sandbox.tenant_id = cloud_agents.require_tenant_id()
          AND sandbox.project_uid = p_project AND sandbox.operation_id = prior_operation;
    ELSIF selected.target_uid IS NOT NULL THEN
        selected_target := selected.target_uid;
    ELSE
        selected_target := cloud_agents.foundation_select_remote_worker_target_v2(
            selected.tenant_id, selected.project_uid, selected.target_region_uid,
            selected.target_resource_pool_uid, selected.target_runtime, selected.target_architecture,
            selected.workload_trust, selected.isolation_runtime,
            selected.cpu_millis, selected.memory_bytes, p_workspace
        );
    END IF;
    IF selected_target IS NULL THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;
    operation_value := 'op-' || pg_catalog.md5(cloud_agents.require_tenant_id() || '|' || p_project || '|' || p_sandbox || '|' || p_key);
    event_value := 'evt-' || pg_catalog.md5(cloud_agents.require_tenant_id() || '|' || p_project || '|' || p_sandbox || '|' || p_key);
    SELECT cloud_agents.accept_foundation_intent_v1(
        p_project, p_workspace, p_workspace_name, p_workspace, selected_target, p_sandbox,
        selected.image_uri, selected.cpu_millis, selected.memory_bytes, operation_value,
        event_value, p_subject, p_key, p_digest
    ) INTO operation_value;
    UPDATE cloud_agents.sandbox_sessions AS sandbox
    SET runtime_profile_uid = p_profile, runtime_profile_version = p_profile_version
    WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = p_project
        AND sandbox.sandbox_uid = p_sandbox
        AND (sandbox.runtime_profile_uid IS NULL AND sandbox.runtime_profile_version IS NULL
            OR sandbox.runtime_profile_uid = p_profile AND sandbox.runtime_profile_version = p_profile_version);
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox profile conflict'; END IF;
    RETURN QUERY SELECT operation_value, p_workspace, p_sandbox, p_profile, p_profile_version,
        sandbox.generation, sandbox.desired_state, sandbox.observed_state
    FROM cloud_agents.sandbox_sessions AS sandbox
    WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = p_project
        AND sandbox.sandbox_uid = p_sandbox;
END;
$$;

CREATE OR REPLACE FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1()
RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    profile cloud_agents.runtime_profiles%ROWTYPE;
    target_id text;
BEGIN
    IF TG_TABLE_NAME = 'runtime_profiles' THEN
        IF NEW.status <> 'published' OR NEW.status IS NOT DISTINCT FROM OLD.status THEN
            RETURN NEW;
        END IF;
        IF NOT cloud_agents.foundation_runtime_profile_available_v2(
            NEW.tenant_id, NEW.project_uid, COALESCE(NEW.target_uid, ''), NEW.target_region_uid,
            NEW.target_resource_pool_uid, NEW.target_runtime, NEW.target_architecture,
            NEW.workload_trust, NEW.isolation_runtime, NEW.network_policy_ref,
            NEW.cpu_millis, NEW.memory_bytes, ''
        ) THEN
            RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.runtime_profile_uid IS NULL OR NEW.desired_state <> 'running' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE'
       AND ROW(NEW.generation, NEW.desired_state, NEW.runtime_profile_uid,
               NEW.runtime_profile_version, NEW.cpu_millis, NEW.memory_bytes)
           IS NOT DISTINCT FROM
           ROW(OLD.generation, OLD.desired_state, OLD.runtime_profile_uid,
               OLD.runtime_profile_version, OLD.cpu_millis, OLD.memory_bytes) THEN
        RETURN NEW;
    END IF;
    SELECT stored.* INTO profile
    FROM cloud_agents.runtime_profiles AS stored
    WHERE stored.tenant_id = NEW.tenant_id AND stored.project_uid = NEW.project_uid
      AND stored.profile_uid = NEW.runtime_profile_uid
      AND stored.profile_version = NEW.runtime_profile_version
      AND stored.status IN ('published', 'disabled');
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;
    SELECT volume.target_uid INTO target_id
    FROM cloud_agents.workspace_volumes AS volume
    WHERE volume.tenant_id = NEW.tenant_id AND volume.project_uid = NEW.project_uid
      AND volume.workspace_uid = NEW.workspace_uid;
    IF target_id IS NULL OR profile.target_uid IS NOT NULL AND profile.target_uid <> target_id
       OR profile.target_uid IS NULL AND target_id IS DISTINCT FROM
          cloud_agents.foundation_select_remote_worker_target_v2(
              profile.tenant_id, profile.project_uid, profile.target_region_uid,
              profile.target_resource_pool_uid, profile.target_runtime, profile.target_architecture,
              profile.workload_trust, profile.isolation_runtime,
              NEW.cpu_millis, NEW.memory_bytes, NEW.workspace_uid
          ) THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;

    PERFORM 1 FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = NEW.tenant_id AND target.project_uid = NEW.project_uid
      AND target.target_uid = target_id
    FOR UPDATE;
    IF NOT FOUND OR NOT cloud_agents.foundation_runtime_profile_available_v2(
        profile.tenant_id, profile.project_uid, target_id, profile.target_region_uid,
        profile.target_resource_pool_uid, profile.target_runtime, profile.target_architecture,
        profile.workload_trust, profile.isolation_runtime, profile.network_policy_ref,
        NEW.cpu_millis, NEW.memory_bytes, NEW.workspace_uid
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION cloud_agents.claim_foundation_sandbox_v9(
    p_target_kind text, p_target_uid text, p_holder text, p_incarnation text, p_token text,
    p_lease_seconds integer, p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, operation_generation bigint, action text,
    project_uid text, workspace_uid text, workspace_name text, volume_uid text,
    physical_volume_uid text, target_uid text, target_generation bigint, target_endpoint text,
    credential_ref text, sandbox_uid text, sandbox_generation bigint, image_uri text,
    runtime_profile_uid text, runtime_profile_version bigint,
    workload_trust text, isolation_runtime text,
    cpu_millis bigint, memory_bytes bigint, spec_digest text, runtime_uid text,
    runtime_state text, runtime_operation_uid text, runtime_generation bigint,
    runtime_spec_digest text, ttl_seconds integer, expires_at timestamptz,
    network_policy_uid text, network_default_egress text, network_allowed_egress text[],
    network_preview_enabled boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    claimed_at timestamptz := transaction_timestamp();
    candidate cloud_agents.outbox_events%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_target_kind NOT IN ('docker', 'kubernetes', 'remote-worker')
        OR p_target_kind IN ('docker', 'kubernetes') AND p_target_uid <> ''
        OR p_target_kind = 'remote-worker' AND NOT cloud_agents.is_valid_identifier(p_target_uid)
        OR NOT cloud_agents.is_valid_identifier(p_holder) OR NOT cloud_agents.is_valid_identifier(p_incarnation)
        OR NOT cloud_agents.is_valid_identifier(p_token) OR p_lease_seconds NOT BETWEEN 1 AND 60
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation claim input is invalid'; END IF;

    SELECT event.* INTO candidate
    FROM cloud_agents.outbox_events AS event
    JOIN cloud_agents.sandbox_sessions AS sandbox
      ON sandbox.tenant_id = event.tenant_id AND sandbox.operation_id = event.operation_id
     AND sandbox.operation_generation = event.operation_generation
     AND sandbox.sandbox_uid = event.aggregate_id AND sandbox.generation = event.generation
    JOIN cloud_agents.workspace_volumes AS volume
      ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
     AND volume.workspace_uid = sandbox.workspace_uid
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    JOIN cloud_agents.runtime_profiles AS profile
      ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
     AND profile.profile_uid = sandbox.runtime_profile_uid
     AND profile.profile_version = sandbox.runtime_profile_version
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = event.tenant_id AND activity.operation_uid = event.operation_id
     AND activity.sandbox_generation = event.operation_generation
    WHERE event.profile_id = 'foundationSandboxLifecycle/v1alpha1'
      AND event.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
      AND event.event_class = 'operation_effect' AND event.aggregate_kind = 'sandboxSession'
      AND (event.state = 'pending' OR event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at)
      AND event.delivery_attempts < 8 AND NOT sandbox.writer_released
      AND target.target_kind = p_target_kind AND (p_target_uid = '' OR target.target_uid = p_target_uid)
      AND (p_target_kind IN ('docker', 'kubernetes')
           OR COALESCE(activity.action, 'sandbox.create') = 'sandbox.create' AND sandbox.generation = 1
              AND cloud_agents.foundation_runtime_profile_available_v2(
                  profile.tenant_id, profile.project_uid, target.target_uid,
                  profile.target_region_uid, profile.target_resource_pool_uid,
                  profile.target_runtime, profile.target_architecture,
                  profile.workload_trust, profile.isolation_runtime, profile.network_policy_ref,
                  sandbox.cpu_millis, sandbox.memory_bytes, sandbox.workspace_uid)
           OR activity.action = 'sandbox.rebuild'
              AND cloud_agents.foundation_runtime_profile_available_v2(
                  profile.tenant_id, profile.project_uid, target.target_uid,
                  profile.target_region_uid, profile.target_resource_pool_uid,
                  profile.target_runtime, profile.target_architecture,
                  profile.workload_trust, profile.isolation_runtime, profile.network_policy_ref,
                  sandbox.cpu_millis, sandbox.memory_bytes, sandbox.workspace_uid)
           OR activity.action = 'sandbox.stop')
      AND (activity.action = 'sandbox.stop' AND sandbox.desired_state = 'stopped'
              AND sandbox.runtime_uid IS NOT NULL AND volume.observed_state = 'available'
           OR COALESCE(activity.action, 'sandbox.create') IN ('sandbox.create', 'sandbox.rebuild')
              AND sandbox.desired_state = 'running'
              AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
              AND (activity.action IS NULL OR volume.observed_state = 'available'))
      AND (p_target_kind IN ('docker', 'kubernetes') OR NOT EXISTS (
          SELECT 1 FROM cloud_agents.outbox_events AS active_event
          JOIN cloud_agents.sandbox_sessions AS active_sandbox
            ON active_sandbox.tenant_id = active_event.tenant_id
           AND active_sandbox.operation_id = active_event.operation_id
           AND active_sandbox.operation_generation = active_event.operation_generation
          JOIN cloud_agents.workspace_volumes AS active_volume
            ON active_volume.tenant_id = active_sandbox.tenant_id
           AND active_volume.project_uid = active_sandbox.project_uid
           AND active_volume.workspace_uid = active_sandbox.workspace_uid
          WHERE active_event.state = 'claimed' AND active_event.claim_expires_at > claimed_at
            AND active_volume.target_uid = p_target_uid
      ))
    ORDER BY event.created_at, event.tenant_id, event.event_id
    FOR UPDATE OF event SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;

    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);
    UPDATE cloud_agents.outbox_events AS event SET state = 'claimed',
        delivery_attempts = event.delivery_attempts + 1, next_attempt_at = NULL,
        claim_holder_id = p_holder, claim_incarnation = p_incarnation, claim_token = p_token,
        claim_started_at = claimed_at,
        claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds), updated_at = claimed_at
    WHERE event.tenant_id = candidate.tenant_id AND event.event_id = candidate.event_id
    RETURNING event.* INTO candidate;
    UPDATE cloud_agents.platform_operations AS operation SET
        state = CASE WHEN candidate.delivery_attempts = 1 THEN 'running' ELSE 'reconciling' END,
        recovery_generation = candidate.delivery_attempts - 1,
        current_attempt_number = candidate.delivery_attempts, updated_at = claimed_at
    WHERE operation.tenant_id = candidate.tenant_id AND operation.operation_id = candidate.operation_id
      AND operation.operation_generation = candidate.operation_generation
      AND operation.state IN ('pending', 'running', 'reconciling');
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation operation is not claimable'; END IF;
    INSERT INTO cloud_agents.operation_attempts (
        tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number, state,
        claim_holder_id, claim_incarnation, claim_token, claim_started_at, claim_expires_at,
        created_at, updated_at
    ) VALUES (
        candidate.tenant_id, candidate.tenant_id, candidate.operation_id, candidate.operation_generation,
        candidate.delivery_attempts, 'claimed', p_holder, p_incarnation, p_token,
        claimed_at, candidate.claim_expires_at, claimed_at, claimed_at
    );
    PERFORM cloud_agents.append_coordination_audit(
        candidate.tenant_id, p_audit, candidate.profile_id, candidate.profile_digest,
        p_subject, candidate.operation_id, candidate.operation_generation, candidate.delivery_attempts,
        NULL, NULL, NULL, 'sandbox.claim', 'pending', NULL, NULL, claimed_at
    );

    RETURN QUERY SELECT candidate.tenant_id, candidate.event_id, candidate.delivery_attempts,
        candidate.claim_expires_at, candidate.operation_id, candidate.operation_generation,
        COALESCE(activity.action, 'sandbox.create'), sandbox.project_uid, sandbox.workspace_uid,
        workspace.workspace_name, volume.volume_uid, volume.physical_volume_uid, volume.target_uid,
        target.generation, target.endpoint, target.credential_ref, sandbox.sandbox_uid,
        sandbox.generation, sandbox.image_uri, sandbox.runtime_profile_uid, sandbox.runtime_profile_version,
        profile.workload_trust, profile.isolation_runtime,
        sandbox.cpu_millis, sandbox.memory_bytes, sandbox.spec_digest, sandbox.runtime_uid,
        sandbox.runtime_state, sandbox.runtime_operation_uid, sandbox.runtime_generation,
        sandbox.runtime_spec_digest, sandbox.ttl_seconds, sandbox.expires_at,
        COALESCE(profile.network_policy_ref, ''), COALESCE(policy.default_egress, ''),
        COALESCE(policy.allowed_egress, ARRAY[]::text[]), COALESCE(policy.preview_enabled, false)
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspaces AS workspace USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
     AND target.target_uid = volume.target_uid
    JOIN cloud_agents.runtime_profiles AS profile
      ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
     AND profile.profile_uid = sandbox.runtime_profile_uid
     AND profile.profile_version = sandbox.runtime_profile_version
    LEFT JOIN cloud_agents.network_policies AS policy
      ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
     AND policy.policy_uid = profile.network_policy_ref
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = candidate.tenant_id AND activity.operation_uid = candidate.operation_id
     AND activity.sandbox_generation = candidate.operation_generation
    WHERE sandbox.tenant_id = candidate.tenant_id AND sandbox.operation_id = candidate.operation_id
      AND sandbox.operation_generation = candidate.operation_generation
      AND sandbox.sandbox_uid = candidate.aggregate_id AND sandbox.generation = candidate.generation;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation effect authority drifted'; END IF;
END;
$$;

ALTER FUNCTION cloud_agents.is_valid_remote_worker_capabilities(text[]) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.foundation_target_supports_isolation_v1(text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.foundation_select_remote_worker_target_v2(text,text,text,text,text,text,text,text,bigint,bigint,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.foundation_runtime_profile_available_v2(text,text,text,text,text,text,text,text,text,text,bigint,bigint,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_runtime_profile_draft_v4(text,text,text,text,bigint,text,text,text,text,text,text,text,text,text,text,text,bigint,bigint,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_runtime_profile_v4(text,text,text,bigint,bigint,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_foundation_sandbox_v9(text,text,text,text,text,integer,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.is_valid_remote_worker_capabilities(text[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.foundation_target_supports_isolation_v1(text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.foundation_select_remote_worker_target_v2(text,text,text,text,text,text,text,text,bigint,bigint,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.foundation_runtime_profile_available_v2(text,text,text,text,text,text,text,text,text,text,bigint,bigint,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_runtime_profile_draft_v4(text,text,text,text,bigint,text,text,text,text,text,text,text,text,text,text,text,bigint,bigint,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_runtime_profile_v4(text,text,text,bigint,bigint,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_sandbox_v9(text,text,text,text,text,integer,text,text) FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v8(text,text,text,text,text,integer,text,text) FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_target_supports_isolation_v1(text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_select_remote_worker_target_v2(text,text,text,text,text,text,text,text,bigint,bigint,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_runtime_profile_available_v2(text,text,text,text,text,text,text,text,text,text,bigint,bigint,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_runtime_profile_draft_v4(text,text,text,text,bigint,text,text,text,text,text,text,text,text,text,text,text,bigint,bigint,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_runtime_profile_v4(text,text,text,bigint,bigint,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_sandbox_v9(text,text,text,text,text,integer,text,text) TO cloud_agents_runtime;
