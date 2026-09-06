CREATE FUNCTION cloud_agents.is_valid_remote_worker_capabilities(p_values text[])
RETURNS boolean
LANGUAGE sql IMMUTABLE SET search_path = pg_catalog
AS $$
    SELECT pg_catalog.cardinality(p_values) BETWEEN 1 AND 16
       AND p_values <@ ARRAY['docker','exec','files','preview','pty','ssh']::text[]
       AND p_values = ARRAY(
           SELECT DISTINCT value
           FROM pg_catalog.unnest(p_values) AS value
           ORDER BY value
       )
$$;

ALTER TABLE cloud_agents.remote_worker_enrollments
    ADD COLUMN node_resource_version bigint CHECK (node_resource_version IS NULL OR node_resource_version > 0),
    ADD COLUMN node_generation bigint CHECK (node_generation IS NULL OR node_generation > 0),
    ADD COLUMN node_observed_generation bigint CHECK (node_observed_generation IS NULL OR node_observed_generation > 0),
    ADD COLUMN node_desired_state text CHECK (node_desired_state IS NULL OR node_desired_state IN ('active', 'drained')),
    ADD COLUMN node_observed_state text CHECK (node_observed_state IS NULL OR node_observed_state IN ('active', 'drained')),
    ADD COLUMN node_worker_version text CHECK (node_worker_version IS NULL OR node_worker_version ~ '^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$'),
    ADD COLUMN node_os text CHECK (node_os IS NULL OR cloud_agents.is_valid_identifier(node_os)),
    ADD COLUMN node_architecture text CHECK (node_architecture IS NULL OR cloud_agents.is_valid_identifier(node_architecture)),
    ADD COLUMN node_kernel_version text CHECK (node_kernel_version IS NULL OR
        (pg_catalog.char_length(node_kernel_version) BETWEEN 1 AND 128 AND node_kernel_version ~ '^[ -~]+$')),
    ADD COLUMN node_capabilities text[],
    ADD COLUMN node_capacity_cpu_millis bigint CHECK (node_capacity_cpu_millis IS NULL OR node_capacity_cpu_millis BETWEEN 100 AND 512000000),
    ADD COLUMN node_capacity_memory_bytes bigint CHECK (node_capacity_memory_bytes IS NULL OR node_capacity_memory_bytes BETWEEN 134217728 AND 8796093022208000),
    ADD COLUMN node_capacity_disk_bytes bigint CHECK (node_capacity_disk_bytes IS NULL OR node_capacity_disk_bytes BETWEEN 134217728 AND 8796093022208000),
    ADD COLUMN node_first_connected_at timestamptz,
    ADD COLUMN node_last_heartbeat_at timestamptz,
    ADD COLUMN node_heartbeat_expires_at timestamptz,
    ADD CONSTRAINT remote_worker_enrollments_node_status_check CHECK (
        (node_resource_version IS NULL AND node_generation IS NULL AND node_observed_generation IS NULL
            AND node_desired_state IS NULL AND node_observed_state IS NULL AND node_worker_version IS NULL
            AND node_os IS NULL AND node_architecture IS NULL AND node_kernel_version IS NULL
            AND node_capabilities IS NULL AND node_capacity_cpu_millis IS NULL
            AND node_capacity_memory_bytes IS NULL AND node_capacity_disk_bytes IS NULL
            AND node_first_connected_at IS NULL AND node_last_heartbeat_at IS NULL
            AND node_heartbeat_expires_at IS NULL)
        OR (state = 'enrolled' AND node_resource_version IS NOT NULL AND node_generation IS NOT NULL
            AND node_observed_generation IS NOT NULL AND node_observed_generation <= node_generation
            AND node_desired_state IS NOT NULL AND node_observed_state IS NOT NULL
            AND node_worker_version IS NOT NULL AND node_os IS NOT NULL AND node_architecture IS NOT NULL
            AND node_kernel_version IS NOT NULL AND node_capabilities IS NOT NULL
            AND cloud_agents.is_valid_remote_worker_capabilities(node_capabilities)
            AND node_capacity_cpu_millis IS NOT NULL AND node_capacity_memory_bytes IS NOT NULL
            AND node_capacity_disk_bytes IS NOT NULL AND node_first_connected_at IS NOT NULL
            AND node_last_heartbeat_at >= node_first_connected_at
            AND node_heartbeat_expires_at = node_last_heartbeat_at + interval '30 seconds')
    );

CREATE FUNCTION cloud_agents.reset_remote_worker_node_status_on_incarnation_change_v1()
RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    IF OLD.incarnation_uid IS DISTINCT FROM NEW.incarnation_uid THEN
        NEW.node_resource_version := NULL;
        NEW.node_generation := NULL;
        NEW.node_observed_generation := NULL;
        NEW.node_desired_state := NULL;
        NEW.node_observed_state := NULL;
        NEW.node_worker_version := NULL;
        NEW.node_os := NULL;
        NEW.node_architecture := NULL;
        NEW.node_kernel_version := NULL;
        NEW.node_capabilities := NULL;
        NEW.node_capacity_cpu_millis := NULL;
        NEW.node_capacity_memory_bytes := NULL;
        NEW.node_capacity_disk_bytes := NULL;
        NEW.node_first_connected_at := NULL;
        NEW.node_last_heartbeat_at := NULL;
        NEW.node_heartbeat_expires_at := NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reset_remote_worker_node_status_on_incarnation_change_v1
BEFORE UPDATE OF incarnation_uid ON cloud_agents.remote_worker_enrollments
FOR EACH ROW EXECUTE FUNCTION cloud_agents.reset_remote_worker_node_status_on_incarnation_change_v1();

CREATE FUNCTION cloud_agents.heartbeat_remote_worker_v1(
    p_tenant text, p_project text, p_enrollment text, p_peer_certificate_digest text,
    p_incarnation text, p_observed_generation bigint, p_observed_state text, p_worker_version text,
    p_os text, p_architecture text, p_kernel_version text, p_capabilities text[],
    p_capacity_cpu_millis bigint, p_capacity_memory_bytes bigint, p_capacity_disk_bytes bigint
)
RETURNS TABLE (
    enrollment_uid text, worker_uid text, worker_name text, incarnation_uid text,
    node_resource_version bigint, node_generation bigint, node_observed_generation bigint,
    node_desired_state text, node_observed_state text, node_health_state text,
    node_worker_version text, node_os text, node_architecture text, node_kernel_version text,
    node_capabilities text[], node_capacity_cpu_millis bigint, node_capacity_memory_bytes bigint,
    node_capacity_disk_bytes bigint, node_first_connected_at timestamptz,
    node_last_heartbeat_at timestamptz, node_heartbeat_expires_at timestamptz,
    reconcile_required boolean
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_incarnation) OR p_observed_generation < 1
       OR p_observed_state NOT IN ('active', 'drained')
       OR p_worker_version !~ '^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$'
       OR NOT cloud_agents.is_valid_identifier(p_os) OR NOT cloud_agents.is_valid_identifier(p_architecture)
       OR pg_catalog.char_length(p_kernel_version) NOT BETWEEN 1 AND 128 OR p_kernel_version !~ '^[ -~]+$'
       OR p_capabilities IS NULL OR NOT cloud_agents.is_valid_remote_worker_capabilities(p_capabilities)
       OR p_capacity_cpu_millis NOT BETWEEN 100 AND 512000000
       OR p_capacity_memory_bytes NOT BETWEEN 134217728 AND 8796093022208000
       OR p_capacity_disk_bytes NOT BETWEEN 134217728 AND 8796093022208000 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker heartbeat input is invalid';
    END IF;

    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    FOR UPDATE;
    IF NOT FOUND OR existing.state <> 'enrolled' OR existing.certificate_state <> 'active'
       OR existing.certificate_not_after <= clock_timestamp()
       OR existing.certificate_sha256 <> p_peer_certificate_digest
       OR existing.incarnation_uid <> p_incarnation THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;
    IF existing.node_generation IS NULL THEN
        IF p_observed_generation <> 1 OR p_observed_state <> 'active' THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker generation conflict';
        END IF;
        existing.node_resource_version := 1;
        existing.node_generation := 1;
        existing.node_observed_generation := 1;
        existing.node_desired_state := 'active';
        existing.node_first_connected_at := mutation_at;
    ELSIF p_observed_generation < existing.node_observed_generation
       OR p_observed_generation > existing.node_generation THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker generation conflict';
    END IF;
    existing.node_observed_generation := p_observed_generation;
    existing.node_observed_state := p_observed_state;

    UPDATE cloud_agents.remote_worker_enrollments AS enrollment
    SET node_resource_version = existing.node_resource_version,
        node_generation = existing.node_generation,
        node_observed_generation = existing.node_observed_generation,
        node_desired_state = existing.node_desired_state,
        node_observed_state = existing.node_observed_state,
        node_worker_version = p_worker_version, node_os = p_os,
        node_architecture = p_architecture, node_kernel_version = p_kernel_version,
        node_capabilities = p_capabilities, node_capacity_cpu_millis = p_capacity_cpu_millis,
        node_capacity_memory_bytes = p_capacity_memory_bytes,
        node_capacity_disk_bytes = p_capacity_disk_bytes,
        node_first_connected_at = existing.node_first_connected_at,
        node_last_heartbeat_at = mutation_at,
        node_heartbeat_expires_at = mutation_at + interval '30 seconds'
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    RETURNING enrollment.* INTO existing;

    RETURN QUERY SELECT existing.enrollment_uid, existing.worker_uid, existing.worker_name,
        existing.incarnation_uid, existing.node_resource_version, existing.node_generation,
        existing.node_observed_generation, existing.node_desired_state, existing.node_observed_state,
        'online'::text, existing.node_worker_version, existing.node_os, existing.node_architecture,
        existing.node_kernel_version, existing.node_capabilities, existing.node_capacity_cpu_millis,
        existing.node_capacity_memory_bytes, existing.node_capacity_disk_bytes,
        existing.node_first_connected_at, existing.node_last_heartbeat_at,
        existing.node_heartbeat_expires_at,
        existing.node_observed_generation <> existing.node_generation
            OR existing.node_observed_state <> existing.node_desired_state;
END;
$$;

ALTER FUNCTION cloud_agents.is_valid_remote_worker_capabilities(text[]) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.reset_remote_worker_node_status_on_incarnation_change_v1() OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.heartbeat_remote_worker_v1(text,text,text,text,text,bigint,text,text,text,text,text,text[],bigint,bigint,bigint) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.is_valid_remote_worker_capabilities(text[]) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.reset_remote_worker_node_status_on_incarnation_change_v1() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.heartbeat_remote_worker_v1(text,text,text,text,text,bigint,text,text,text,text,text,text[],bigint,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.heartbeat_remote_worker_v1(text,text,text,text,text,bigint,text,text,text,text,text,text[],bigint,bigint,bigint) TO cloud_agents_runtime;
