ALTER TABLE cloud_agents.runtime_profiles ADD COLUMN target_region_uid text NOT NULL DEFAULT '';
ALTER TABLE cloud_agents.runtime_profiles ADD COLUMN target_resource_pool_uid text NOT NULL DEFAULT '';
ALTER TABLE cloud_agents.runtime_profiles ADD COLUMN target_runtime text NOT NULL DEFAULT '';
ALTER TABLE cloud_agents.runtime_profiles ADD COLUMN target_architecture text NOT NULL DEFAULT '';
ALTER TABLE cloud_agents.runtime_profiles DROP CONSTRAINT runtime_profiles_target_uid_check;
ALTER TABLE cloud_agents.runtime_profiles ALTER COLUMN target_uid DROP NOT NULL;
ALTER TABLE cloud_agents.runtime_profiles ADD CONSTRAINT runtime_profiles_target_selection CHECK (
    target_uid IS NOT NULL
        AND cloud_agents.is_valid_identifier(target_uid)
        AND target_region_uid = '' AND target_resource_pool_uid = ''
        AND target_runtime = '' AND target_architecture = ''
    OR target_uid IS NULL
        AND cloud_agents.is_valid_identifier(target_region_uid)
        AND cloud_agents.is_valid_identifier(target_resource_pool_uid)
        AND target_runtime = 'docker' AND target_architecture IN ('amd64', 'arm64')
);

CREATE FUNCTION cloud_agents.foundation_select_remote_worker_target_v1(
    p_tenant text, p_project text, p_region text, p_pool text,
    p_runtime text, p_architecture text, p_cpu bigint, p_memory bigint, p_workspace text
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

CREATE FUNCTION cloud_agents.foundation_runtime_profile_available_v1(
    p_tenant text, p_project text, p_target text, p_region text, p_pool text,
    p_runtime text, p_architecture text, p_cpu bigint, p_memory bigint, p_workspace text
)
RETURNS boolean
LANGUAGE sql STABLE SET search_path = pg_catalog, cloud_agents
AS $$
    SELECT CASE WHEN p_target <> '' THEN
        cloud_agents.foundation_target_can_reserve_v1(
            p_tenant, p_project, p_target, p_cpu, p_memory, p_workspace
        )
    ELSE cloud_agents.foundation_select_remote_worker_target_v1(
        p_tenant, p_project, p_region, p_pool, p_runtime, p_architecture,
        p_cpu, p_memory, p_workspace
    ) IS NOT NULL END
$$;

CREATE FUNCTION cloud_agents.create_runtime_profile_draft_v3(
    p_tenant text, p_project text, p_profile text, p_name text, p_version bigint,
    p_description text, p_target text, p_region text, p_pool text,
    p_runtime text, p_architecture text, p_network_policy text, p_image text, p_release text,
    p_cpu bigint, p_memory bigint, p_key text, p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, target_region_uid text,
    target_resource_pool_uid text, target_runtime text, target_architecture text,
    network_policy_ref text, image_uri text, release_digest text, cpu_millis bigint,
    memory_bytes bigint, resource_version bigint, created_at timestamptz,
    updated_at timestamptz, published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    existing cloud_agents.runtime_profiles%ROWTYPE;
    representative_target text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT (
        cloud_agents.is_valid_identifier(p_target)
            AND p_region = '' AND p_pool = '' AND p_runtime = '' AND p_architecture = ''
        OR p_target = ''
            AND cloud_agents.is_valid_identifier(p_region)
            AND cloud_agents.is_valid_identifier(p_pool)
            AND p_runtime = 'docker' AND p_architecture IN ('amd64', 'arm64')
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'runtime profile target selection is invalid';
    END IF;

    SELECT profile.* INTO existing FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
      AND profile.create_idempotency_key = p_key FOR UPDATE;
    IF FOUND THEN
        IF existing.create_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSE
        representative_target := p_target;
        IF representative_target = '' THEN
            SELECT target.target_uid INTO representative_target
            FROM cloud_agents.deployment_targets AS target
            JOIN cloud_agents.remote_worker_enrollments AS enrollment
              ON enrollment.tenant_id = target.tenant_id
             AND enrollment.project_uid = target.project_uid
             AND enrollment.target_uid = target.target_uid
            WHERE p_region = 'region-local' AND p_pool = 'pool-remote-worker'
              AND p_runtime = 'docker'
              AND target.tenant_id = p_tenant AND target.project_uid = p_project
              AND target.target_kind = 'remote-worker'
              AND enrollment.node_resource_version IS NOT NULL
              AND enrollment.node_architecture = p_architecture
              AND enrollment.node_capabilities @> ARRAY[
                  'docker','network-dns-nft','workspace-volume'
              ]::text[]
            ORDER BY target.target_uid
            LIMIT 1 FOR KEY SHARE OF target, enrollment;
            IF representative_target IS NULL THEN
                RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
            END IF;
        END IF;
        PERFORM 1 FROM cloud_agents.create_runtime_profile_draft_v2(
            p_tenant, p_project, p_profile, p_name, p_version, p_description,
            representative_target, p_network_policy, p_image, p_release, p_cpu, p_memory,
            p_key, p_digest, p_request, p_subject
        );
        UPDATE cloud_agents.runtime_profiles AS profile SET
            target_uid = NULLIF(p_target, ''), target_region_uid = p_region,
            target_resource_pool_uid = p_pool, target_runtime = p_runtime,
            target_architecture = p_architecture
        WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
          AND profile.profile_uid = p_profile AND profile.profile_version = p_version
        RETURNING profile.* INTO existing;
        IF NOT FOUND THEN
            RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'runtime profile target selection drifted';
        END IF;
    END IF;

    RETURN QUERY SELECT existing.profile_version_uid, existing.profile_uid,
        existing.profile_name, existing.profile_version, existing.description,
        existing.status, COALESCE(existing.target_uid, ''), existing.target_region_uid,
        existing.target_resource_pool_uid, existing.target_runtime, existing.target_architecture,
        COALESCE(existing.network_policy_ref, ''), existing.image_uri, existing.release_digest,
        existing.cpu_millis, existing.memory_bytes, existing.resource_version,
        existing.created_at, existing.updated_at, existing.published_at, existing.disabled_at;
END;
$$;

CREATE OR REPLACE FUNCTION cloud_agents.transition_runtime_profile_v1(
    p_tenant text, p_project text, p_profile text, p_version bigint,
    p_expected_resource_version bigint, p_action text, p_key text,
    p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, image_uri text, release_digest text,
    cpu_millis bigint, memory_bytes bigint, resource_version bigint,
    created_at timestamptz, updated_at timestamptz, published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.runtime_profiles%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_profile)
        OR p_version NOT BETWEEN 1 AND 2147483647 OR p_expected_resource_version < 1
        OR p_action NOT IN ('publish', 'disable') OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_request)
        OR p_subject !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'runtime profile transition input is invalid'; END IF;
    SELECT profile.* INTO existing FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
        AND profile.profile_uid = p_profile AND profile.profile_version = p_version FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile was not found'; END IF;

    IF p_action = 'publish' AND existing.publish_idempotency_key IS NOT NULL THEN
        IF existing.publish_idempotency_key IS DISTINCT FROM p_key OR existing.publish_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSIF p_action = 'disable' AND existing.disable_idempotency_key IS NOT NULL THEN
        IF existing.disable_idempotency_key IS DISTINCT FROM p_key OR existing.disable_request_digest IS DISTINCT FROM p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile idempotency conflict';
        END IF;
    ELSE
        IF existing.resource_version <> p_expected_resource_version
            OR p_action = 'publish' AND existing.status <> 'draft'
            OR p_action = 'disable' AND existing.status <> 'published'
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'runtime profile transition conflict'; END IF;
        IF p_action = 'publish' THEN
            IF NOT cloud_agents.foundation_runtime_profile_available_v1(
                p_tenant, p_project, COALESCE(existing.target_uid, ''), existing.target_region_uid,
                existing.target_resource_pool_uid, existing.target_runtime, existing.target_architecture,
                existing.cpu_millis, existing.memory_bytes, ''
            ) THEN
                RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
            END IF;
            UPDATE cloud_agents.runtime_profiles AS profile SET status = 'published',
                resource_version = profile.resource_version + 1, updated_at = mutation_at, published_at = mutation_at,
                publish_idempotency_key = p_key, publish_request_digest = p_digest
            WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
                AND profile.profile_uid = p_profile AND profile.profile_version = p_version
            RETURNING profile.* INTO existing;
        ELSE
            UPDATE cloud_agents.runtime_profiles AS profile SET status = 'disabled',
                resource_version = profile.resource_version + 1, updated_at = mutation_at, disabled_at = mutation_at,
                disable_idempotency_key = p_key, disable_request_digest = p_digest
            WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
                AND profile.profile_uid = p_profile AND profile.profile_version = p_version
            RETURNING profile.* INTO existing;
        END IF;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|' || existing.profile_version_uid || '|' || p_action || '|' || p_key);
        INSERT INTO cloud_agents.runtime_profile_activity (
            tenant_id, project_uid, profile_version_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest, profile_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, existing.profile_version_uid, operation_value || '-done', operation_value,
            'runtime-profile.' || p_action, p_key, p_request, p_digest, p_subject, p_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.profile_version_uid, existing.profile_uid, existing.profile_name,
        existing.profile_version, existing.description, existing.status, existing.target_uid,
        existing.image_uri, existing.release_digest, existing.cpu_millis, existing.memory_bytes,
        existing.resource_version, existing.created_at, existing.updated_at,
        existing.published_at, existing.disabled_at;
END;
$$;

CREATE FUNCTION cloud_agents.transition_runtime_profile_v3(
    p_tenant text, p_project text, p_profile text, p_version bigint,
    p_expected_resource_version bigint, p_action text, p_key text,
    p_digest text, p_request text, p_subject text
)
RETURNS TABLE (
    profile_version_uid text, profile_uid text, profile_name text, profile_version bigint,
    description text, status text, target_uid text, target_region_uid text,
    target_resource_pool_uid text, target_runtime text, target_architecture text,
    network_policy_ref text, image_uri text, release_digest text, cpu_millis bigint,
    memory_bytes bigint, resource_version bigint, created_at timestamptz,
    updated_at timestamptz, published_at timestamptz, disabled_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    PERFORM 1 FROM cloud_agents.transition_runtime_profile_v2(
        p_tenant, p_project, p_profile, p_version, p_expected_resource_version,
        p_action, p_key, p_digest, p_request, p_subject
    );
    RETURN QUERY SELECT profile.profile_version_uid, profile.profile_uid,
        profile.profile_name, profile.profile_version, profile.description, profile.status,
        COALESCE(profile.target_uid, ''), profile.target_region_uid,
        profile.target_resource_pool_uid, profile.target_runtime, profile.target_architecture,
        COALESCE(profile.network_policy_ref, ''), profile.image_uri, profile.release_digest,
        profile.cpu_millis, profile.memory_bytes, profile.resource_version,
        profile.created_at, profile.updated_at, profile.published_at, profile.disabled_at
    FROM cloud_agents.runtime_profiles AS profile
    WHERE profile.tenant_id = p_tenant AND profile.project_uid = p_project
      AND profile.profile_uid = p_profile AND profile.profile_version = p_version;
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
        selected_target := cloud_agents.foundation_select_remote_worker_target_v1(
            selected.tenant_id, selected.project_uid, selected.target_region_uid,
            selected.target_resource_pool_uid, selected.target_runtime, selected.target_architecture,
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
        IF NOT cloud_agents.foundation_runtime_profile_available_v1(
            NEW.tenant_id, NEW.project_uid, COALESCE(NEW.target_uid, ''), NEW.target_region_uid,
            NEW.target_resource_pool_uid, NEW.target_runtime, NEW.target_architecture,
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
          cloud_agents.foundation_select_remote_worker_target_v1(
              profile.tenant_id, profile.project_uid, profile.target_region_uid,
              profile.target_resource_pool_uid, profile.target_runtime, profile.target_architecture,
              NEW.cpu_millis, NEW.memory_bytes, NEW.workspace_uid
          ) THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;

    PERFORM 1 FROM cloud_agents.deployment_targets AS target
    WHERE target.tenant_id = NEW.tenant_id AND target.project_uid = NEW.project_uid
      AND target.target_uid = target_id
    FOR UPDATE;
    IF NOT FOUND OR NOT cloud_agents.foundation_target_can_reserve_v1(
        NEW.tenant_id, NEW.project_uid, target_id,
        NEW.cpu_millis, NEW.memory_bytes, NEW.workspace_uid
    ) THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION cloud_agents.transition_foundation_sandbox_v4(
    p_project text, p_sandbox text, p_expected_generation bigint, p_expected_resource_version bigint,
    p_action text, p_confirmed_sandbox text, p_compute_disposition text, p_workspace_disposition text,
    p_subject text, p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    operation_uid text, idempotency_key text, action text, sandbox_uid text,
    sandbox_generation bigint, requested_by text, request_id text, requested_at timestamptz,
    updated_at timestamptz, operation_state text, cleanup_phase text, stable_error_code text,
    compute_disposition text, workspace_disposition text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    sandbox cloud_agents.sandbox_sessions%ROWTYPE;
    volume cloud_agents.workspace_volumes%ROWTYPE;
    prior cloud_agents.idempotency_records%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
    event_value text;
    next_generation bigint;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_sandbox)
        OR p_expected_generation < 1 OR p_expected_resource_version < 1
        OR p_action NOT IN ('stop', 'rebuild') OR p_confirmed_sandbox IS DISTINCT FROM p_sandbox
        OR p_compute_disposition IS DISTINCT FROM (CASE WHEN p_action = 'stop' THEN 'delete' ELSE 'create' END)
        OR p_workspace_disposition IS DISTINCT FROM 'retain'
        OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
        OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_request)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'foundation sandbox transition input is invalid'; END IF;

    INSERT INTO cloud_agents.idempotency_records (
        tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id, profile_digest,
        idempotency_key, request_digest, state, expires_at
    ) VALUES (
        cloud_agents.require_tenant_id(), cloud_agents.require_tenant_id(), p_subject,
        'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c',
        'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
        p_key, p_digest, 'pending', mutation_at + interval '1 day'
    ) ON CONFLICT DO NOTHING;
    SELECT record.* INTO STRICT prior FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = cloud_agents.require_tenant_id() AND record.subject_digest = p_subject
        AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
        AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
        AND record.idempotency_key = p_key FOR UPDATE;
    IF prior.request_digest IS DISTINCT FROM p_digest THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox idempotency conflict';
    END IF;

    operation_value := prior.operation_id;
    IF operation_value IS NULL THEN
        SELECT stored.* INTO sandbox FROM cloud_agents.sandbox_sessions AS stored
        WHERE stored.tenant_id = cloud_agents.require_tenant_id() AND stored.project_uid = p_project
            AND stored.sandbox_uid = p_sandbox FOR UPDATE;
        IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'foundation sandbox was not found'; END IF;
        SELECT stored.* INTO STRICT volume FROM cloud_agents.workspace_volumes AS stored
        WHERE stored.tenant_id = sandbox.tenant_id AND stored.project_uid = sandbox.project_uid
            AND stored.workspace_uid = sandbox.workspace_uid FOR KEY SHARE;
        IF sandbox.generation IS DISTINCT FROM p_expected_generation
            OR sandbox.resource_version IS DISTINCT FROM p_expected_resource_version
            OR volume.retention <> 'retain' OR volume.observed_state <> 'available'
            OR volume.physical_volume_uid IS NULL
        THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox transition conflict'; END IF;
        IF p_action = 'stop' THEN
            IF sandbox.desired_state <> 'running' OR sandbox.observed_state <> 'running'
                OR sandbox.observed_generation <> sandbox.generation OR sandbox.writer_released
                OR sandbox.runtime_uid IS NULL OR sandbox.runtime_operation_uid IS NULL
                OR sandbox.runtime_generation IS NULL OR sandbox.runtime_spec_digest IS NULL
            THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox transition conflict'; END IF;
        ELSE
            IF sandbox.desired_state <> 'stopped' OR sandbox.observed_state <> 'stopped'
                OR sandbox.observed_generation <> sandbox.generation OR NOT sandbox.writer_released
                OR sandbox.runtime_uid IS NOT NULL
            THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'foundation sandbox transition conflict'; END IF;
            PERFORM 1 FROM cloud_agents.runtime_profiles AS profile
            JOIN cloud_agents.network_policies AS policy
              ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
             AND policy.policy_uid = profile.network_policy_ref
            WHERE profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
                AND profile.profile_uid = sandbox.runtime_profile_uid
                AND profile.profile_version = sandbox.runtime_profile_version
                AND policy.default_egress IN ('restricted', 'deny')
                AND (policy.default_egress = 'restricted' AND pg_catalog.cardinality(policy.allowed_egress) > 0
                    OR policy.default_egress = 'deny' AND pg_catalog.cardinality(policy.allowed_egress) = 0)
                AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
                AND policy.proxy_policy_ref IS NULL AND NOT policy.ingress_enabled
            FOR KEY SHARE OF profile, policy;
            IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile network policy unavailable'; END IF;
            PERFORM 1 FROM cloud_agents.runtime_profiles AS profile
            JOIN cloud_agents.deployment_targets AS target
              ON target.tenant_id = profile.tenant_id AND target.project_uid = profile.project_uid
             AND target.target_uid = volume.target_uid
            WHERE profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
                AND profile.profile_uid = sandbox.runtime_profile_uid
                AND profile.profile_version = sandbox.runtime_profile_version
                AND profile.status IN ('published', 'disabled')
                AND cloud_agents.foundation_runtime_profile_available_v1(
                    profile.tenant_id, profile.project_uid, COALESCE(profile.target_uid, ''),
                    profile.target_region_uid, profile.target_resource_pool_uid,
                    profile.target_runtime, profile.target_architecture,
                    sandbox.cpu_millis, sandbox.memory_bytes, sandbox.workspace_uid
                )
                AND target.observed_phase = 'ready' AND target.scheduling_state = 'active'
            FOR KEY SHARE OF profile, target;
            IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'runtime profile target unavailable'; END IF;
        END IF;

        next_generation := sandbox.generation + 1;
        operation_value := 'op-' || pg_catalog.md5(sandbox.tenant_id || '|' || p_project || '|' || p_sandbox || '|' || p_key);
        event_value := 'evt-' || pg_catalog.md5(sandbox.tenant_id || '|' || p_project || '|' || p_sandbox || '|' || p_key);
        INSERT INTO cloud_agents.platform_operations (
            tenant_id, tenant_ref_id, operation_id, operation_generation, registry_digest,
            state_machine_digest, policy_digest, profile_id, profile_digest, subject_digest,
            request_digest, state, cleanup_phase, recovery_generation, current_attempt_number
        ) VALUES (
            sandbox.tenant_id, sandbox.tenant_id, operation_value, next_generation,
            'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c',
            cloud_agents.coordination_state_machine_digest(), cloud_agents.coordination_policy_digest(),
            'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
            p_subject, p_digest, 'pending', 'none', 0, 0
        );
        INSERT INTO cloud_agents.foundation_sandbox_activity (
            tenant_id, project_uid, sandbox_uid, operation_uid, action, idempotency_key,
            request_id, request_digest, subject_digest, sandbox_generation,
            compute_disposition, workspace_disposition, requested_at
        ) VALUES (
            sandbox.tenant_id, p_project, p_sandbox, operation_value, 'sandbox.' || p_action,
            p_key, p_request, p_digest, p_subject, next_generation,
            p_compute_disposition, p_workspace_disposition, mutation_at
        );
        INSERT INTO cloud_agents.operation_finalizers (
            tenant_id, tenant_ref_id, operation_id, operation_generation, finalizer_name,
            required, state, delivery_attempts
        ) VALUES (sandbox.tenant_id, sandbox.tenant_id, operation_value, next_generation,
            'sandbox-compute', true, 'pending', 0);
        INSERT INTO cloud_agents.outbox_events (
            tenant_id, tenant_ref_id, event_id, registry_digest, profile_id, profile_digest,
            event_class, aggregate_kind, aggregate_id, aggregate_sequence, generation,
            operation_id, operation_generation, payload_digest, state, delivery_attempts
        ) VALUES (
            sandbox.tenant_id, sandbox.tenant_id, event_value,
            'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c',
            'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
            'operation_effect', 'sandboxSession', p_sandbox, next_generation, next_generation,
            operation_value, next_generation, p_digest, 'pending', 0
        );
        INSERT INTO cloud_agents.coordination_audit_facts (
            tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
            subject_digest, operation_id, operation_generation, transition, outcome
        ) VALUES (
            sandbox.tenant_id, sandbox.tenant_id, event_value,
            'sha256:9ebf663f49a5d1f5209d5c94656419d21fc2022105355d51a5b2fc3ed318378c',
            'foundationSandboxLifecycle/v1alpha1', 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b',
            p_subject, operation_value, next_generation, 'sandbox.' || p_action || '.accept', 'pending'
        );
        UPDATE cloud_agents.sandbox_sessions AS stored SET
            generation = next_generation,
            desired_state = CASE WHEN p_action = 'stop' THEN 'stopped' ELSE 'running' END,
            writer_released = false,
            operation_id = operation_value,
            operation_generation = next_generation,
            spec_digest = p_digest,
            expires_at = CASE WHEN p_action = 'rebuild' AND stored.ttl_seconds IS NOT NULL
                THEN mutation_at + pg_catalog.make_interval(secs => stored.ttl_seconds)
                ELSE stored.expires_at END,
            stable_error_code = NULL
        WHERE stored.tenant_id = sandbox.tenant_id AND stored.project_uid = p_project
            AND stored.sandbox_uid = p_sandbox;
        UPDATE cloud_agents.idempotency_records AS record SET
            operation_id = operation_value, operation_generation = next_generation, updated_at = mutation_at
        WHERE record.tenant_id = sandbox.tenant_id AND record.subject_digest = p_subject
            AND record.profile_id = 'foundationSandboxLifecycle/v1alpha1'
            AND record.profile_digest = 'sha256:aabd6aa244c89a4850eba529dbe387132c3a0aff7ffba430f6115afb6e215a1b'
            AND record.idempotency_key = p_key;
    END IF;

    RETURN QUERY SELECT activity.operation_uid, activity.idempotency_key, activity.action,
        activity.sandbox_uid, activity.sandbox_generation, activity.subject_digest,
        activity.request_id, activity.requested_at, operation.updated_at, operation.state,
        operation.cleanup_phase, operation.terminal_error_code,
        activity.compute_disposition, activity.workspace_disposition
    FROM cloud_agents.foundation_sandbox_activity AS activity
    JOIN cloud_agents.platform_operations AS operation
      ON operation.tenant_id = activity.tenant_id AND operation.operation_id = activity.operation_uid
     AND operation.operation_generation = activity.sandbox_generation
    WHERE activity.tenant_id = cloud_agents.require_tenant_id() AND activity.operation_uid = operation_value;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'foundation sandbox activity drifted'; END IF;
END;
$$;

ALTER FUNCTION cloud_agents.foundation_select_remote_worker_target_v1(text,text,text,text,text,text,bigint,bigint,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.foundation_runtime_profile_available_v1(text,text,text,text,text,text,text,bigint,bigint,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.create_runtime_profile_draft_v3(text,text,text,text,bigint,text,text,text,text,text,text,text,text,text,bigint,bigint,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_runtime_profile_v1(text,text,text,bigint,bigint,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_runtime_profile_v3(text,text,text,bigint,bigint,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.transition_foundation_sandbox_v4(text,text,bigint,bigint,text,text,text,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.foundation_select_remote_worker_target_v1(text,text,text,text,text,text,bigint,bigint,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.foundation_runtime_profile_available_v1(text,text,text,text,text,text,text,bigint,bigint,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.create_runtime_profile_draft_v3(text,text,text,text,bigint,text,text,text,text,text,text,text,text,text,bigint,bigint,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_runtime_profile_v1(text,text,text,bigint,bigint,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_runtime_profile_v3(text,text,text,bigint,bigint,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_foundation_sandbox_v4(text,text,bigint,bigint,text,text,text,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_select_remote_worker_target_v1(text,text,text,text,text,text,bigint,bigint,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.foundation_runtime_profile_available_v1(text,text,text,text,text,text,text,bigint,bigint,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.create_runtime_profile_draft_v3(text,text,text,text,bigint,text,text,text,text,text,text,text,text,text,bigint,bigint,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_runtime_profile_v1(text,text,text,bigint,bigint,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_runtime_profile_v3(text,text,text,bigint,bigint,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_sandbox_v1(text,text,text,text,text,bigint,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.guard_remote_worker_foundation_admission_v1() TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.transition_foundation_sandbox_v4(text,text,bigint,bigint,text,text,text,text,text,text,text,text) TO cloud_agents_runtime;
