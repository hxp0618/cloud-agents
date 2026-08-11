CREATE TABLE cloud_agents.platform_tenants (
    tenant_id text NOT NULL,
    tenant_uid text NOT NULL,
    tenant_name text NOT NULL,
    display_name text NOT NULL,
    state text NOT NULL,
    resource_version bigint NOT NULL,
    resource_kind text GENERATED ALWAYS AS ('platform_tenant') STORED,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, tenant_uid),
    CONSTRAINT platform_tenants_tenant_root CHECK (tenant_id = tenant_uid),
    CONSTRAINT platform_tenants_tenant_id CHECK (cloud_agents.is_valid_identifier(tenant_id)),
    CONSTRAINT platform_tenants_tenant_uid CHECK (cloud_agents.is_valid_identifier(tenant_uid)),
    CONSTRAINT platform_tenants_tenant_name CHECK (cloud_agents.is_valid_identifier(tenant_name)),
    CONSTRAINT platform_tenants_display_name
        CHECK (pg_catalog.char_length(display_name) BETWEEN 1 AND 160),
    CONSTRAINT platform_tenants_state CHECK (state IN ('active', 'suspended')),
    CONSTRAINT platform_tenants_resource_version CHECK (resource_version > 0),
    CONSTRAINT platform_tenants_updated_after_created CHECK (updated_at >= created_at),
    UNIQUE (tenant_id, tenant_name)
);

CREATE TABLE cloud_agents.tenant_resource_versions (
    tenant_id text NOT NULL,
    tenant_uid text NOT NULL,
    current_revision bigint NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, tenant_uid),
    CONSTRAINT tenant_resource_versions_tenant_root CHECK (tenant_id = tenant_uid),
    CONSTRAINT tenant_resource_versions_revision CHECK (current_revision > 0),
    CONSTRAINT tenant_resource_versions_tenant_fk
        FOREIGN KEY (tenant_id, tenant_uid)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX tenant_resource_versions_tenant_fk_idx
    ON cloud_agents.tenant_resource_versions (tenant_id, tenant_uid);

CREATE TABLE cloud_agents.resource_changes (
    tenant_id text NOT NULL,
    tenant_uid text NOT NULL,
    resource_version bigint NOT NULL,
    resource_kind text NOT NULL,
    resource_uid text NOT NULL,
    change_kind text NOT NULL,
    actor_database_principal text NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, resource_version),
    CONSTRAINT resource_changes_tenant_root CHECK (tenant_id = tenant_uid),
    CONSTRAINT resource_changes_resource_version CHECK (resource_version > 0),
    CONSTRAINT resource_changes_resource_kind
        CHECK (resource_kind IN ('platform_tenant', 'organization', 'project')),
    CONSTRAINT resource_changes_resource_uid CHECK (cloud_agents.is_valid_identifier(resource_uid)),
    CONSTRAINT resource_changes_change_kind CHECK (change_kind IN ('created', 'updated', 'deleted')),
    CONSTRAINT resource_changes_actor_present
        CHECK (pg_catalog.char_length(actor_database_principal) BETWEEN 1 AND 128),
    CONSTRAINT resource_changes_tenant_fk
        FOREIGN KEY (tenant_id, tenant_uid)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    UNIQUE (tenant_id, resource_version, resource_kind, resource_uid)
);

CREATE INDEX resource_changes_resource_history_idx
    ON cloud_agents.resource_changes
    (tenant_id, resource_kind, resource_uid, resource_version DESC);

CREATE INDEX resource_changes_tenant_fk_idx
    ON cloud_agents.resource_changes (tenant_id, tenant_uid);

ALTER TABLE cloud_agents.platform_tenants
    ADD CONSTRAINT platform_tenants_change_fk
    FOREIGN KEY (tenant_id, resource_version, resource_kind, tenant_uid)
    REFERENCES cloud_agents.resource_changes
        (tenant_id, resource_version, resource_kind, resource_uid)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE cloud_agents.audit_facts (
    tenant_id text NOT NULL,
    tenant_uid text NOT NULL,
    audit_fact_uid text NOT NULL,
    resource_version bigint NOT NULL,
    action text NOT NULL,
    resource_kind text NOT NULL,
    resource_uid text NOT NULL,
    actor_database_principal text NOT NULL,
    reason_code text NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, audit_fact_uid),
    CONSTRAINT audit_facts_tenant_root CHECK (tenant_id = tenant_uid),
    CONSTRAINT audit_facts_uid CHECK (cloud_agents.is_valid_identifier(audit_fact_uid)),
    CONSTRAINT audit_facts_resource_version CHECK (resource_version > 0),
    CONSTRAINT audit_facts_action CHECK (action = 'platform_tenant.bootstrap'),
    CONSTRAINT audit_facts_resource_kind CHECK (resource_kind = 'platform_tenant'),
    CONSTRAINT audit_facts_resource_uid CHECK (cloud_agents.is_valid_identifier(resource_uid)),
    CONSTRAINT audit_facts_actor_present
        CHECK (pg_catalog.char_length(actor_database_principal) BETWEEN 1 AND 128),
    CONSTRAINT audit_facts_reason_code CHECK (cloud_agents.is_valid_identifier(reason_code)),
    CONSTRAINT audit_facts_tenant_fk
        FOREIGN KEY (tenant_id, tenant_uid)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT audit_facts_change_fk
        FOREIGN KEY (tenant_id, resource_version, resource_kind, resource_uid)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX audit_facts_resource_idx
    ON cloud_agents.audit_facts
    (tenant_id, resource_kind, resource_uid, occurred_at DESC);

CREATE INDEX audit_facts_tenant_fk_idx
    ON cloud_agents.audit_facts (tenant_id, tenant_uid);

CREATE INDEX audit_facts_change_fk_idx
    ON cloud_agents.audit_facts
    (tenant_id, resource_version, resource_kind, resource_uid);

CREATE TABLE cloud_agents.organizations (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    organization_uid text NOT NULL,
    organization_name text NOT NULL,
    display_name text NOT NULL,
    state text NOT NULL,
    resource_version bigint NOT NULL,
    resource_kind text GENERATED ALWAYS AS ('organization') STORED,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, organization_uid),
    CONSTRAINT organizations_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT organizations_organization_uid CHECK (cloud_agents.is_valid_identifier(organization_uid)),
    CONSTRAINT organizations_organization_name CHECK (cloud_agents.is_valid_identifier(organization_name)),
    CONSTRAINT organizations_display_name
        CHECK (pg_catalog.char_length(display_name) BETWEEN 1 AND 160),
    CONSTRAINT organizations_state CHECK (state IN ('active', 'suspended')),
    CONSTRAINT organizations_resource_version CHECK (resource_version > 0),
    CONSTRAINT organizations_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT organizations_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT organizations_change_fk
        FOREIGN KEY (tenant_id, resource_version, resource_kind, organization_uid)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (tenant_id, organization_name)
);

CREATE TABLE cloud_agents.projects (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    project_uid text NOT NULL,
    project_name text NOT NULL,
    organization_uid text NOT NULL,
    display_name text NOT NULL,
    state text NOT NULL,
    resource_version bigint NOT NULL,
    resource_kind text GENERATED ALWAYS AS ('project') STORED,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, project_uid),
    CONSTRAINT projects_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT projects_project_uid CHECK (cloud_agents.is_valid_identifier(project_uid)),
    CONSTRAINT projects_project_name CHECK (cloud_agents.is_valid_identifier(project_name)),
    CONSTRAINT projects_organization_uid CHECK (cloud_agents.is_valid_identifier(organization_uid)),
    CONSTRAINT projects_display_name
        CHECK (pg_catalog.char_length(display_name) BETWEEN 1 AND 160),
    CONSTRAINT projects_state CHECK (state IN ('active', 'suspended', 'archived')),
    CONSTRAINT projects_resource_version CHECK (resource_version > 0),
    CONSTRAINT projects_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT projects_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT projects_organization_fk
        FOREIGN KEY (tenant_id, organization_uid)
        REFERENCES cloud_agents.organizations (tenant_id, organization_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT projects_change_fk
        FOREIGN KEY (tenant_id, resource_version, resource_kind, project_uid)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (tenant_id, project_name)
);

CREATE INDEX platform_tenants_change_fk_idx
    ON cloud_agents.platform_tenants
    (tenant_id, resource_version, resource_kind, tenant_uid);

CREATE INDEX organizations_tenant_fk_idx
    ON cloud_agents.organizations (tenant_id, tenant_ref_id);

CREATE INDEX organizations_change_fk_idx
    ON cloud_agents.organizations
    (tenant_id, resource_version, resource_kind, organization_uid);

CREATE INDEX projects_tenant_fk_idx
    ON cloud_agents.projects (tenant_id, tenant_ref_id);

CREATE INDEX projects_organization_fk_idx
    ON cloud_agents.projects (tenant_id, organization_uid);

CREATE INDEX projects_change_fk_idx
    ON cloud_agents.projects
    (tenant_id, resource_version, resource_kind, project_uid);

ALTER TABLE cloud_agents.platform_tenants OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.tenant_resource_versions OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.resource_changes OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.audit_facts OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.organizations OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.projects OWNER TO cloud_agents_migration_owner;

ALTER TABLE cloud_agents.platform_tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.platform_tenants FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.tenant_resource_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.tenant_resource_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.resource_changes ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.resource_changes FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.audit_facts ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.audit_facts FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.organizations FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.projects FORCE ROW LEVEL SECURITY;

CREATE POLICY platform_tenants_runtime_tenant
    ON cloud_agents.platform_tenants
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY platform_tenants_migration_owner
    ON cloud_agents.platform_tenants
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY tenant_resource_versions_runtime_tenant
    ON cloud_agents.tenant_resource_versions
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY tenant_resource_versions_migration_owner
    ON cloud_agents.tenant_resource_versions
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY resource_changes_runtime_tenant
    ON cloud_agents.resource_changes
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY resource_changes_migration_owner
    ON cloud_agents.resource_changes
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY audit_facts_runtime_tenant
    ON cloud_agents.audit_facts
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY audit_facts_migration_owner
    ON cloud_agents.audit_facts
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY organizations_runtime_tenant
    ON cloud_agents.organizations
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY organizations_migration_owner
    ON cloud_agents.organizations
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY projects_runtime_tenant
    ON cloud_agents.projects
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY projects_migration_owner
    ON cloud_agents.projects
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

REVOKE ALL ON TABLE cloud_agents.platform_tenants FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.tenant_resource_versions FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.resource_changes FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.audit_facts FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.organizations FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.projects FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.platform_tenants FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.tenant_resource_versions FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.resource_changes FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.audit_facts FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.organizations FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.projects FROM cloud_agents_bootstrap_admin;

GRANT SELECT ON TABLE cloud_agents.platform_tenants TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.tenant_resource_versions TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.resource_changes TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.organizations TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.projects TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.bootstrap_platform_tenant(
    p_tenant_uid text,
    p_tenant_name text,
    p_display_name text,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (tenant_id text, resource_version bigint)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    bootstrap_at timestamptz;
    bootstrap_role_oid oid;
    caller_role record;
    caller_membership record;
    incoming_membership record;
    existing_intent_matches boolean;
BEGIN
    SELECT role_row.oid
    INTO STRICT bootstrap_role_oid
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = 'cloud_agents_bootstrap_admin';

    SELECT
        role_row.oid,
        role_row.rolcanlogin,
        role_row.rolinherit,
        role_row.rolsuper,
        role_row.rolcreatedb,
        role_row.rolcreaterole,
        role_row.rolreplication,
        role_row.rolbypassrls
    INTO STRICT caller_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = SESSION_USER;

    IF (
        SELECT pg_catalog.count(*)
        FROM pg_catalog.pg_auth_members AS membership
        WHERE membership.member = caller_role.oid
    ) <> 1
        OR NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = caller_role.oid
                AND membership.roleid = bootstrap_role_oid
        )
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.roleid = caller_role.oid
        )
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = bootstrap_role_oid
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'session user must have exactly one nondelegable bootstrap membership';
    END IF;

    SELECT
        membership.admin_option,
        coalesce(
            (pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean,
            true
        ) AS membership_inherits,
        grantor_role.oid AS grantor_oid,
        grantor_role.rolname AS grantor_name,
        grantor_role.rolsuper AS grantor_is_superuser
    INTO caller_membership
    FROM pg_catalog.pg_auth_members AS membership
    JOIN pg_catalog.pg_roles AS grantor_role
        ON grantor_role.oid = membership.grantor
    WHERE membership.roleid = bootstrap_role_oid
        AND membership.member = caller_role.oid;

    IF NOT FOUND
        OR caller_membership.admin_option
        OR NOT caller_membership.membership_inherits
        OR NOT caller_membership.grantor_is_superuser
        OR caller_membership.grantor_name IN (
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin'
        )
        OR EXISTS (
            WITH RECURSIVE grantor_memberships (roleid) AS (
                SELECT membership.roleid
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = caller_membership.grantor_oid

                UNION

                SELECT membership.roleid
                FROM pg_catalog.pg_auth_members AS membership
                JOIN grantor_memberships
                    ON grantor_memberships.roleid = membership.member
            )
            SELECT 1
            FROM grantor_memberships
            JOIN pg_catalog.pg_roles AS inherited_role
                ON inherited_role.oid = grantor_memberships.roleid
            WHERE inherited_role.rolname IN (
                'cloud_agents_migration_owner',
                'cloud_agents_runtime',
                'cloud_agents_bootstrap_admin'
            )
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'session user bootstrap membership has untrusted provenance';
    END IF;

    FOR incoming_membership IN
        SELECT
            membership.admin_option,
            membership.member,
            coalesce(
                (pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean,
                true
            ) AS membership_inherits,
            member_role.rolname AS member_name,
            member_role.rolcanlogin AS member_can_login,
            member_role.rolinherit AS member_inherits,
            member_role.rolsuper AS member_is_superuser,
            member_role.rolcreatedb AS member_can_create_database,
            member_role.rolcreaterole AS member_can_create_role,
            member_role.rolreplication AS member_can_replicate,
            member_role.rolbypassrls AS member_can_bypass_rls,
            pg_catalog.pg_has_role(
                member_role.oid,
                bootstrap_role_oid,
                'USAGE'
            ) AS member_uses_authority,
            grantor_role.oid AS grantor_oid,
            grantor_role.rolname AS grantor_name,
            grantor_role.rolsuper AS grantor_is_superuser
        FROM pg_catalog.pg_auth_members AS membership
        JOIN pg_catalog.pg_roles AS member_role
            ON member_role.oid = membership.member
        JOIN pg_catalog.pg_roles AS grantor_role
            ON grantor_role.oid = membership.grantor
        WHERE membership.roleid = bootstrap_role_oid
        ORDER BY membership.admin_option, membership.member
    LOOP
        IF NOT incoming_membership.member_can_login
            OR NOT incoming_membership.member_inherits
            OR NOT incoming_membership.membership_inherits
            OR NOT incoming_membership.member_uses_authority
            OR incoming_membership.member_is_superuser
            OR incoming_membership.member_can_create_database
            OR incoming_membership.member_can_create_role
            OR incoming_membership.member_can_replicate
            OR incoming_membership.member_can_bypass_rls
            OR EXISTS (
                SELECT 1
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.roleid = incoming_membership.member
            )
            OR (
                SELECT pg_catalog.count(*)
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = incoming_membership.member
            ) <> 1
            OR EXISTS (
                SELECT 1
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = incoming_membership.member
                    AND membership.roleid <> bootstrap_role_oid
            )
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = pg_catalog.format(
                    'bootstrap role has unsafe member %I; all callers are fenced',
                    incoming_membership.member_name
                );
        END IF;

        IF incoming_membership.admin_option
            OR NOT incoming_membership.grantor_is_superuser
            OR incoming_membership.grantor_name IN (
                'cloud_agents_migration_owner',
                'cloud_agents_runtime',
                'cloud_agents_bootstrap_admin'
            )
            OR EXISTS (
                WITH RECURSIVE grantor_memberships (roleid) AS (
                    SELECT membership.roleid
                    FROM pg_catalog.pg_auth_members AS membership
                    WHERE membership.member = incoming_membership.grantor_oid

                    UNION

                    SELECT membership.roleid
                    FROM pg_catalog.pg_auth_members AS membership
                    JOIN grantor_memberships
                        ON grantor_memberships.roleid = membership.member
                )
                SELECT 1
                FROM grantor_memberships
                JOIN pg_catalog.pg_roles AS inherited_role
                    ON inherited_role.oid = grantor_memberships.roleid
                WHERE inherited_role.rolname IN (
                    'cloud_agents_migration_owner',
                    'cloud_agents_runtime',
                    'cloud_agents_bootstrap_admin'
                )
            )
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = 'bootstrap role membership provenance drift; all callers are fenced';
        END IF;
    END LOOP;

    IF SESSION_USER IN (
        'cloud_agents_migration_owner',
        'cloud_agents_runtime',
        'cloud_agents_bootstrap_admin'
    )
        OR NOT caller_role.rolcanlogin
        OR NOT caller_role.rolinherit
        OR caller_role.rolsuper
        OR caller_role.rolcreatedb
        OR caller_role.rolcreaterole
        OR caller_role.rolreplication
        OR caller_role.rolbypassrls
        OR NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.roleid = bootstrap_role_oid
                AND membership.member = caller_role.oid
                AND NOT membership.admin_option
        )
        OR NOT pg_catalog.pg_has_role(
            SESSION_USER,
            'cloud_agents_bootstrap_admin',
            'USAGE'
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'session user lacks the direct bootstrap authority boundary';
    END IF;

    IF pg_catalog.pg_has_role(
        SESSION_USER,
        'cloud_agents_migration_owner',
        'MEMBER'
    )
        OR pg_catalog.pg_has_role(
            SESSION_USER,
            'cloud_agents_runtime',
            'MEMBER'
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'bootstrap session user has conflicting database authority';
    END IF;

    IF NOT cloud_agents.is_valid_identifier(p_tenant_uid)
        OR NOT cloud_agents.is_valid_identifier(p_tenant_name)
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'bootstrap identifiers must satisfy the public opaque identifier contract';
    END IF;

    IF p_display_name IS NULL
        OR pg_catalog.char_length(p_display_name) NOT BETWEEN 1 AND 160
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'tenant display name must contain between 1 and 160 characters';
    END IF;

    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            'cloud_agents.bootstrap_platform_tenant:' || p_tenant_uid,
            0
        )
    );

    IF EXISTS (
        SELECT 1
        FROM cloud_agents.platform_tenants AS tenant
        WHERE tenant.tenant_id = p_tenant_uid
            AND tenant.tenant_uid = p_tenant_uid
    ) THEN
        SELECT EXISTS (
            SELECT 1
            FROM cloud_agents.platform_tenants AS tenant
            JOIN cloud_agents.tenant_resource_versions AS revision
                ON revision.tenant_id = tenant.tenant_id
                AND revision.tenant_uid = tenant.tenant_uid
            JOIN cloud_agents.resource_changes AS change
                ON change.tenant_id = tenant.tenant_id
                AND change.tenant_uid = tenant.tenant_uid
                AND change.resource_version = tenant.resource_version
                AND change.resource_kind = tenant.resource_kind
                AND change.resource_uid = tenant.tenant_uid
            JOIN cloud_agents.audit_facts AS audit
                ON audit.tenant_id = tenant.tenant_id
                AND audit.tenant_uid = tenant.tenant_uid
                AND audit.resource_version = change.resource_version
                AND audit.resource_kind = change.resource_kind
                AND audit.resource_uid = change.resource_uid
            WHERE tenant.tenant_id = p_tenant_uid
                AND tenant.tenant_uid = p_tenant_uid
                AND tenant.tenant_name = p_tenant_name
                AND pg_catalog.convert_to(tenant.display_name, 'UTF8')
                    = pg_catalog.convert_to(p_display_name, 'UTF8')
                AND tenant.state = 'active'
                AND tenant.resource_version = 1
                AND revision.current_revision = 1
                AND change.change_kind = 'created'
                AND change.actor_database_principal = SESSION_USER
                AND audit.audit_fact_uid = p_audit_fact_uid
                AND audit.action = 'platform_tenant.bootstrap'
                AND audit.actor_database_principal = SESSION_USER
                AND audit.reason_code = p_reason_code
                AND tenant.created_at = tenant.updated_at
                AND revision.updated_at = tenant.created_at
                AND change.occurred_at = tenant.created_at
                AND audit.occurred_at = tenant.created_at
        ) INTO existing_intent_matches;

        IF existing_intent_matches THEN
            RETURN QUERY SELECT p_tenant_uid, 1::bigint;
            RETURN;
        END IF;

        RAISE EXCEPTION USING
            ERRCODE = '23505',
            MESSAGE = 'cloud_agents tenant bootstrap intent conflicts with existing state',
            CONSTRAINT = 'bootstrap_platform_tenant_intent';
    END IF;

    bootstrap_at := pg_catalog.clock_timestamp();

    BEGIN
        INSERT INTO cloud_agents.platform_tenants (
            tenant_id,
            tenant_uid,
            tenant_name,
            display_name,
            state,
            resource_version,
            created_at,
            updated_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            p_tenant_name,
            p_display_name,
            'active',
            1,
            bootstrap_at,
            bootstrap_at
        );

        INSERT INTO cloud_agents.tenant_resource_versions (
            tenant_id,
            tenant_uid,
            current_revision,
            updated_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            1,
            bootstrap_at
        );

        INSERT INTO cloud_agents.resource_changes (
            tenant_id,
            tenant_uid,
            resource_version,
            resource_kind,
            resource_uid,
            change_kind,
            actor_database_principal,
            occurred_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            1,
            'platform_tenant',
            p_tenant_uid,
            'created',
            SESSION_USER,
            bootstrap_at
        );

        INSERT INTO cloud_agents.audit_facts (
            tenant_id,
            tenant_uid,
            audit_fact_uid,
            resource_version,
            action,
            resource_kind,
            resource_uid,
            actor_database_principal,
            reason_code,
            occurred_at
        ) VALUES (
            p_tenant_uid,
            p_tenant_uid,
            p_audit_fact_uid,
            1,
            'platform_tenant.bootstrap',
            'platform_tenant',
            p_tenant_uid,
            SESSION_USER,
            p_reason_code,
            bootstrap_at
        );
    EXCEPTION
        WHEN unique_violation THEN
            RAISE EXCEPTION USING
                ERRCODE = '23505',
                MESSAGE = 'cloud_agents tenant bootstrap intent conflicts with existing state',
                CONSTRAINT = 'bootstrap_platform_tenant_intent';
    END;

    RETURN QUERY SELECT p_tenant_uid, 1::bigint;
END
$cloud_agents_function$;

ALTER FUNCTION cloud_agents.bootstrap_platform_tenant(text, text, text, text, text)
    OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.bootstrap_platform_tenant(text, text, text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.bootstrap_platform_tenant(text, text, text, text, text)
    FROM cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.bootstrap_platform_tenant(text, text, text, text, text)
    TO cloud_agents_bootstrap_admin;
