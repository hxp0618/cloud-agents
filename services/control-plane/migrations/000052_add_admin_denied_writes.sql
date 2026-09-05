-- Rejections are evidence, not executed resource operations. Requested resources may not exist.
CREATE TABLE cloud_agents.admin_denied_writes (
    tenant_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(tenant_id)),
    project_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(project_uid)),
    event_uid text NOT NULL CHECK (cloud_agents.is_valid_identifier(event_uid)),
    subject_digest text NOT NULL CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    action text NOT NULL CHECK (action IN ('adminUpgradeEnvironmentLease', 'adminRollbackEnvironmentLease',
        'adminRegisterWorkerRelease', 'adminSetStoragePolicy', 'adminSetNetworkPolicy', 'adminSetProjectLeaseQuota',
        'adminCreateEnvironmentProfile', 'adminPublishEnvironmentProfile', 'adminDisableEnvironmentProfile',
        'adminRegisterDeploymentTarget', 'adminProbeDeploymentTarget', 'adminTransitionDeploymentTargetScheduling',
        'adminCleanupDeploymentTarget')),
    resource_uid text NOT NULL CHECK (resource_uid = '' OR cloud_agents.is_valid_identifier(resource_uid)),
    profile_version bigint NOT NULL CHECK (profile_version BETWEEN 0 AND 2147483647),
    request_id text NOT NULL CHECK (cloud_agents.is_valid_identifier(request_id)),
    occurred_at timestamptz NOT NULL DEFAULT pg_catalog.clock_timestamp(),
    PRIMARY KEY (tenant_id, project_uid, event_uid)
);
CREATE INDEX admin_denied_writes_page_idx ON cloud_agents.admin_denied_writes
    (tenant_id, project_uid, occurred_at DESC, event_uid DESC);
ALTER TABLE cloud_agents.admin_denied_writes OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.admin_denied_writes ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.admin_denied_writes FORCE ROW LEVEL SECURITY;
CREATE POLICY admin_denied_writes_runtime_tenant ON cloud_agents.admin_denied_writes
    TO cloud_agents_runtime USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY admin_denied_writes_migration_owner ON cloud_agents.admin_denied_writes
    TO cloud_agents_migration_owner USING (true) WITH CHECK (true);
REVOKE ALL ON TABLE cloud_agents.admin_denied_writes FROM PUBLIC;
GRANT SELECT ON TABLE cloud_agents.admin_denied_writes TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.record_admin_denied_write_v1(p_project text, p_event text,
    p_actor text, p_action text, p_resource text, p_profile_version bigint, p_request text)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    INSERT INTO cloud_agents.admin_denied_writes
        (tenant_id, project_uid, event_uid, subject_digest, action, resource_uid, profile_version, request_id)
    VALUES (cloud_agents.require_tenant_id(), p_project, p_event, p_actor, p_action, p_resource, p_profile_version, p_request);
    RETURN p_event;
END;
$$;
ALTER FUNCTION cloud_agents.record_admin_denied_write_v1(text, text, text, text, text, bigint, text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.record_admin_denied_write_v1(text, text, text, text, text, bigint, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.record_admin_denied_write_v1(text, text, text, text, text, bigint, text) TO cloud_agents_runtime;
