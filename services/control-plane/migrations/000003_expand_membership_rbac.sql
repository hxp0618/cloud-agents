ALTER TABLE cloud_agents.resource_changes
    DROP CONSTRAINT resource_changes_resource_kind;

ALTER TABLE cloud_agents.resource_changes
    ADD CONSTRAINT resource_changes_resource_kind
    CHECK (
        resource_kind IN (
            'platform_tenant',
            'organization',
            'project',
            'membership',
            'role_binding'
        )
    );

CREATE TABLE cloud_agents.builtin_roles (
    role_name text NOT NULL,
    role_version bigint NOT NULL,
    catalog_revision bigint NOT NULL,
    scope_level text NOT NULL,
    state text NOT NULL,
    published_at timestamptz NOT NULL,
    PRIMARY KEY (role_name, role_version),
    CONSTRAINT builtin_roles_name
        CHECK (
            role_name IN (
                'organization.admin',
                'platform.admin',
                'project.admin',
                'project.developer',
                'project.operator',
                'project.viewer',
                'tenant.admin'
            )
        ),
    CONSTRAINT builtin_roles_version CHECK (role_version > 0),
    CONSTRAINT builtin_roles_catalog_revision CHECK (catalog_revision > 0),
    CONSTRAINT builtin_roles_scope
        CHECK (scope_level IN ('platform', 'tenant', 'organization', 'project')),
    CONSTRAINT builtin_roles_state CHECK (state IN ('active', 'deprecated', 'revoked')),
    UNIQUE (role_name, role_version, scope_level)
);

CREATE TABLE cloud_agents.builtin_role_permissions (
    role_name text NOT NULL,
    role_version bigint NOT NULL,
    permission text NOT NULL,
    PRIMARY KEY (role_name, role_version, permission),
    CONSTRAINT builtin_role_permissions_permission
        CHECK (
            pg_catalog.char_length(permission) BETWEEN 3 AND 128
            AND permission ~ '^[a-z][a-z0-9-]*\.(create|get|list|watch|update|delete|act|bind)$'
        ),
    CONSTRAINT builtin_role_permissions_role_fk
        FOREIGN KEY (role_name, role_version)
        REFERENCES cloud_agents.builtin_roles (role_name, role_version)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX builtin_role_permissions_role_fk_idx
    ON cloud_agents.builtin_role_permissions (role_name, role_version);

CREATE TABLE cloud_agents.memberships (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    membership_uid text NOT NULL,
    membership_name text NOT NULL,
    subject_kind text NOT NULL,
    subject_issuer text NOT NULL,
    subject_value text NOT NULL,
    subject_digest text NOT NULL,
    scope_level text NOT NULL,
    scope_tenant_uid text,
    scope_organization_uid text,
    scope_project_uid text,
    scope_key text GENERATED ALWAYS AS (
        CASE scope_level
            WHEN 'platform' THEN 'platform'
            WHEN 'tenant' THEN 'tenant:' || scope_tenant_uid
            WHEN 'organization' THEN 'organization:' || scope_organization_uid
            WHEN 'project' THEN 'project:' || scope_project_uid
        END
    ) STORED,
    state text NOT NULL,
    live_marker boolean GENERATED ALWAYS AS (
        CASE WHEN state <> 'revoked' THEN true ELSE NULL END
    ) STORED,
    expires_at timestamptz,
    resource_version bigint NOT NULL,
    resource_kind text GENERATED ALWAYS AS ('membership') STORED,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, membership_uid),
    CONSTRAINT memberships_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT memberships_uid CHECK (cloud_agents.is_valid_identifier(membership_uid)),
    CONSTRAINT memberships_name CHECK (cloud_agents.is_valid_identifier(membership_name)),
    CONSTRAINT memberships_subject_kind
        CHECK (subject_kind IN ('user', 'serviceAccount', 'workload')),
    CONSTRAINT memberships_subject_issuer
        CHECK (pg_catalog.char_length(subject_issuer) BETWEEN 1 AND 512),
    CONSTRAINT memberships_subject_value
        CHECK (pg_catalog.char_length(subject_value) BETWEEN 1 AND 256),
    CONSTRAINT memberships_subject_digest
        CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT memberships_scope
        CHECK (
            (
                scope_level = 'platform'
                AND scope_tenant_uid IS NULL
                AND scope_organization_uid IS NULL
                AND scope_project_uid IS NULL
            )
            OR (
                scope_level = 'tenant'
                AND scope_tenant_uid IS NOT NULL
                AND scope_tenant_uid = tenant_id
                AND scope_organization_uid IS NULL
                AND scope_project_uid IS NULL
            )
            OR (
                scope_level = 'organization'
                AND scope_tenant_uid IS NULL
                AND scope_organization_uid IS NOT NULL
                AND cloud_agents.is_valid_identifier(scope_organization_uid)
                AND scope_project_uid IS NULL
            )
            OR (
                scope_level = 'project'
                AND scope_tenant_uid IS NULL
                AND scope_organization_uid IS NULL
                AND scope_project_uid IS NOT NULL
                AND cloud_agents.is_valid_identifier(scope_project_uid)
            )
        ),
    CONSTRAINT memberships_scope_key_present CHECK (scope_key IS NOT NULL),
    CONSTRAINT memberships_state CHECK (state IN ('active', 'suspended', 'revoked')),
    CONSTRAINT memberships_resource_version CHECK (resource_version > 0),
    CONSTRAINT memberships_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT memberships_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT memberships_scope_tenant_fk
        FOREIGN KEY (tenant_id, scope_tenant_uid)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT memberships_scope_organization_fk
        FOREIGN KEY (tenant_id, scope_organization_uid)
        REFERENCES cloud_agents.organizations (tenant_id, organization_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT memberships_scope_project_fk
        FOREIGN KEY (tenant_id, scope_project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT memberships_change_fk
        FOREIGN KEY (tenant_id, resource_version, resource_kind, membership_uid)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (tenant_id, membership_name),
    UNIQUE (
        tenant_id,
        subject_kind,
        subject_issuer,
        subject_value,
        scope_key,
        live_marker
    )
);

CREATE INDEX memberships_tenant_fk_idx
    ON cloud_agents.memberships (tenant_id, tenant_ref_id);

CREATE INDEX memberships_scope_tenant_fk_idx
    ON cloud_agents.memberships (tenant_id, scope_tenant_uid);

CREATE INDEX memberships_scope_organization_fk_idx
    ON cloud_agents.memberships (tenant_id, scope_organization_uid);

CREATE INDEX memberships_scope_project_fk_idx
    ON cloud_agents.memberships (tenant_id, scope_project_uid);

CREATE INDEX memberships_change_fk_idx
    ON cloud_agents.memberships
    (tenant_id, resource_version, resource_kind, membership_uid);

CREATE INDEX memberships_subject_lookup_idx
    ON cloud_agents.memberships
    (tenant_id, subject_kind, subject_value);

CREATE TABLE cloud_agents.role_bindings (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    role_binding_uid text NOT NULL,
    role_binding_name text NOT NULL,
    subject_kind text NOT NULL,
    subject_issuer text NOT NULL,
    subject_value text NOT NULL,
    subject_digest text NOT NULL,
    role_name text NOT NULL,
    role_version bigint NOT NULL,
    scope_level text NOT NULL,
    scope_tenant_uid text,
    scope_organization_uid text,
    scope_project_uid text,
    scope_key text GENERATED ALWAYS AS (
        CASE scope_level
            WHEN 'platform' THEN 'platform'
            WHEN 'tenant' THEN 'tenant:' || scope_tenant_uid
            WHEN 'organization' THEN 'organization:' || scope_organization_uid
            WHEN 'project' THEN 'project:' || scope_project_uid
        END
    ) STORED,
    state text NOT NULL,
    live_marker boolean GENERATED ALWAYS AS (
        CASE WHEN state <> 'revoked' THEN true ELSE NULL END
    ) STORED,
    expires_at timestamptz,
    resource_version bigint NOT NULL,
    resource_kind text GENERATED ALWAYS AS ('role_binding') STORED,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, role_binding_uid),
    CONSTRAINT role_bindings_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT role_bindings_uid CHECK (cloud_agents.is_valid_identifier(role_binding_uid)),
    CONSTRAINT role_bindings_name CHECK (cloud_agents.is_valid_identifier(role_binding_name)),
    CONSTRAINT role_bindings_subject_kind
        CHECK (subject_kind IN ('user', 'serviceAccount', 'workload')),
    CONSTRAINT role_bindings_subject_issuer
        CHECK (pg_catalog.char_length(subject_issuer) BETWEEN 1 AND 512),
    CONSTRAINT role_bindings_subject_value
        CHECK (pg_catalog.char_length(subject_value) BETWEEN 1 AND 256),
    CONSTRAINT role_bindings_subject_digest
        CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT role_bindings_scope
        CHECK (
            (
                scope_level = 'platform'
                AND scope_tenant_uid IS NULL
                AND scope_organization_uid IS NULL
                AND scope_project_uid IS NULL
            )
            OR (
                scope_level = 'tenant'
                AND scope_tenant_uid IS NOT NULL
                AND scope_tenant_uid = tenant_id
                AND scope_organization_uid IS NULL
                AND scope_project_uid IS NULL
            )
            OR (
                scope_level = 'organization'
                AND scope_tenant_uid IS NULL
                AND scope_organization_uid IS NOT NULL
                AND cloud_agents.is_valid_identifier(scope_organization_uid)
                AND scope_project_uid IS NULL
            )
            OR (
                scope_level = 'project'
                AND scope_tenant_uid IS NULL
                AND scope_organization_uid IS NULL
                AND scope_project_uid IS NOT NULL
                AND cloud_agents.is_valid_identifier(scope_project_uid)
            )
        ),
    CONSTRAINT role_bindings_scope_key_present CHECK (scope_key IS NOT NULL),
    CONSTRAINT role_bindings_state CHECK (state IN ('active', 'revoked')),
    CONSTRAINT role_bindings_resource_version CHECK (resource_version > 0),
    CONSTRAINT role_bindings_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT role_bindings_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT role_bindings_scope_tenant_fk
        FOREIGN KEY (tenant_id, scope_tenant_uid)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT role_bindings_scope_organization_fk
        FOREIGN KEY (tenant_id, scope_organization_uid)
        REFERENCES cloud_agents.organizations (tenant_id, organization_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT role_bindings_scope_project_fk
        FOREIGN KEY (tenant_id, scope_project_uid)
        REFERENCES cloud_agents.projects (tenant_id, project_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT role_bindings_role_fk
        FOREIGN KEY (role_name, role_version, scope_level)
        REFERENCES cloud_agents.builtin_roles (role_name, role_version, scope_level)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT role_bindings_change_fk
        FOREIGN KEY (tenant_id, resource_version, resource_kind, role_binding_uid)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (tenant_id, role_binding_name),
    UNIQUE (
        tenant_id,
        subject_kind,
        subject_issuer,
        subject_value,
        role_name,
        role_version,
        scope_key,
        live_marker
    )
);

CREATE INDEX role_bindings_tenant_fk_idx
    ON cloud_agents.role_bindings (tenant_id, tenant_ref_id);

CREATE INDEX role_bindings_scope_tenant_fk_idx
    ON cloud_agents.role_bindings (tenant_id, scope_tenant_uid);

CREATE INDEX role_bindings_scope_organization_fk_idx
    ON cloud_agents.role_bindings (tenant_id, scope_organization_uid);

CREATE INDEX role_bindings_scope_project_fk_idx
    ON cloud_agents.role_bindings (tenant_id, scope_project_uid);

CREATE INDEX role_bindings_role_fk_idx
    ON cloud_agents.role_bindings (role_name, role_version, scope_level);

CREATE INDEX role_bindings_change_fk_idx
    ON cloud_agents.role_bindings
    (tenant_id, resource_version, resource_kind, role_binding_uid);

CREATE INDEX role_bindings_subject_lookup_idx
    ON cloud_agents.role_bindings
    (tenant_id, subject_kind, subject_value);

ALTER TABLE cloud_agents.builtin_roles OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.builtin_role_permissions OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.memberships OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.role_bindings OWNER TO cloud_agents_migration_owner;

ALTER TABLE cloud_agents.memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.memberships FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.role_bindings ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.role_bindings FORCE ROW LEVEL SECURITY;

CREATE POLICY memberships_runtime_tenant
    ON cloud_agents.memberships
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY memberships_migration_owner
    ON cloud_agents.memberships
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY role_bindings_runtime_tenant
    ON cloud_agents.role_bindings
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY role_bindings_migration_owner
    ON cloud_agents.role_bindings
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

REVOKE ALL ON TABLE cloud_agents.builtin_roles FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.builtin_role_permissions FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.memberships FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.role_bindings FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.builtin_roles FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.builtin_role_permissions FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.memberships FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.role_bindings FROM cloud_agents_bootstrap_admin;

GRANT SELECT ON TABLE cloud_agents.builtin_roles TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.builtin_role_permissions TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.memberships TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.role_bindings TO cloud_agents_runtime;

INSERT INTO cloud_agents.builtin_roles (
    role_name,
    role_version,
    catalog_revision,
    scope_level,
    state,
    published_at
)
VALUES
    ('organization.admin', 1, 1, 'organization', 'active', '2026-08-17T00:00:00Z'),
    ('platform.admin', 1, 1, 'platform', 'active', '2026-08-17T00:00:00Z'),
    ('project.admin', 1, 1, 'project', 'active', '2026-08-17T00:00:00Z'),
    ('project.developer', 1, 1, 'project', 'active', '2026-08-17T00:00:00Z'),
    ('project.operator', 1, 1, 'project', 'active', '2026-08-17T00:00:00Z'),
    ('project.viewer', 1, 1, 'project', 'active', '2026-08-17T00:00:00Z'),
    ('tenant.admin', 1, 1, 'tenant', 'active', '2026-08-17T00:00:00Z');

INSERT INTO cloud_agents.builtin_role_permissions (role_name, role_version, permission)
SELECT
    seed.role_name,
    1,
    permission_row.permission
FROM (
    VALUES
        (
            'organization.admin',
            ARRAY[
                'memberships.bind',
                'memberships.create',
                'memberships.delete',
                'memberships.get',
                'memberships.list',
                'memberships.update',
                'memberships.watch',
                'operations.get',
                'operations.list',
                'operations.watch',
                'organizations.delete',
                'organizations.get',
                'organizations.list',
                'organizations.update',
                'organizations.watch',
                'projects.act',
                'projects.create',
                'projects.delete',
                'projects.get',
                'projects.list',
                'projects.update',
                'projects.watch',
                'role-bindings.bind',
                'role-bindings.create',
                'role-bindings.delete',
                'role-bindings.get',
                'role-bindings.list',
                'role-bindings.watch',
                'roles.get',
                'roles.list',
                'roles.watch'
            ]::text[]
        ),
        (
            'platform.admin',
            ARRAY[
                'memberships.bind',
                'memberships.create',
                'memberships.delete',
                'memberships.get',
                'memberships.list',
                'memberships.update',
                'memberships.watch',
                'operations.get',
                'operations.list',
                'operations.watch',
                'organizations.create',
                'organizations.delete',
                'organizations.get',
                'organizations.list',
                'organizations.update',
                'organizations.watch',
                'projects.act',
                'projects.create',
                'projects.delete',
                'projects.get',
                'projects.list',
                'projects.update',
                'projects.watch',
                'role-bindings.bind',
                'role-bindings.create',
                'role-bindings.delete',
                'role-bindings.get',
                'role-bindings.list',
                'role-bindings.watch',
                'roles.get',
                'roles.list',
                'roles.watch',
                'tenants.get',
                'tenants.update'
            ]::text[]
        ),
        (
            'project.admin',
            ARRAY[
                'memberships.bind',
                'memberships.create',
                'memberships.delete',
                'memberships.get',
                'memberships.list',
                'memberships.update',
                'memberships.watch',
                'operations.get',
                'operations.list',
                'operations.watch',
                'projects.act',
                'projects.delete',
                'projects.get',
                'projects.list',
                'projects.update',
                'projects.watch',
                'role-bindings.bind',
                'role-bindings.create',
                'role-bindings.delete',
                'role-bindings.get',
                'role-bindings.list',
                'role-bindings.watch',
                'roles.get',
                'roles.list',
                'roles.watch'
            ]::text[]
        ),
        (
            'project.developer',
            ARRAY[
                'operations.get',
                'operations.list',
                'operations.watch',
                'projects.get',
                'projects.list',
                'projects.update',
                'projects.watch'
            ]::text[]
        ),
        (
            'project.operator',
            ARRAY[
                'operations.get',
                'operations.list',
                'operations.watch',
                'projects.act',
                'projects.get',
                'projects.list',
                'projects.watch'
            ]::text[]
        ),
        (
            'project.viewer',
            ARRAY[
                'projects.get',
                'projects.list',
                'projects.watch'
            ]::text[]
        ),
        (
            'tenant.admin',
            ARRAY[
                'memberships.bind',
                'memberships.create',
                'memberships.delete',
                'memberships.get',
                'memberships.list',
                'memberships.update',
                'memberships.watch',
                'operations.get',
                'operations.list',
                'operations.watch',
                'organizations.create',
                'organizations.delete',
                'organizations.get',
                'organizations.list',
                'organizations.update',
                'organizations.watch',
                'projects.act',
                'projects.create',
                'projects.delete',
                'projects.get',
                'projects.list',
                'projects.update',
                'projects.watch',
                'role-bindings.bind',
                'role-bindings.create',
                'role-bindings.delete',
                'role-bindings.get',
                'role-bindings.list',
                'role-bindings.watch',
                'roles.get',
                'roles.list',
                'roles.watch',
                'tenants.get',
                'tenants.update'
            ]::text[]
        )
) AS seed (role_name, permissions)
CROSS JOIN LATERAL pg_catalog.unnest(seed.permissions) AS permission_row (permission);
