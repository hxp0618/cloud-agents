ALTER TABLE cloud_agents.audit_facts
    DROP CONSTRAINT audit_facts_action;

ALTER TABLE cloud_agents.audit_facts
    DROP CONSTRAINT audit_facts_resource_kind;

ALTER TABLE cloud_agents.audit_facts
    ADD CONSTRAINT audit_facts_action
    CHECK (
        action IN (
            'platform_tenant.bootstrap',
            'membership.create',
            'membership.suspend',
            'membership.revoke',
            'role_binding.bind',
            'role_binding.revoke'
        )
    );

ALTER TABLE cloud_agents.audit_facts
    ADD CONSTRAINT audit_facts_resource_kind
    CHECK (resource_kind IN ('platform_tenant', 'membership', 'role_binding'));

ALTER TABLE cloud_agents.audit_facts
    ADD CONSTRAINT audit_facts_action_resource
    CHECK (
        (action = 'platform_tenant.bootstrap' AND resource_kind = 'platform_tenant')
        OR (action LIKE 'membership.%' AND resource_kind = 'membership')
        OR (action LIKE 'role_binding.%' AND resource_kind = 'role_binding')
    );

CREATE FUNCTION cloud_agents.subject_ref_digest(
    p_subject_kind text,
    p_subject_issuer text,
    p_subject_value text
)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE
STRICT
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    canonical_subject text;
BEGIN
    IF p_subject_kind NOT IN ('user', 'serviceAccount', 'workload')
        OR pg_catalog.char_length(p_subject_issuer) NOT BETWEEN 1 AND 512
        OR p_subject_issuer !~ '^[A-Za-z][A-Za-z0-9+.-]*:'
        OR pg_catalog.char_length(p_subject_value) NOT BETWEEN 1 AND 256
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'subject reference is outside the closed mutation profile';
    END IF;

    canonical_subject :=
        '{"issuer":' || pg_catalog.to_json(p_subject_issuer)::text
        || ',"kind":' || pg_catalog.to_json(p_subject_kind)::text
        || ',"subject":' || pg_catalog.to_json(p_subject_value)::text
        || '}';

    RETURN 'sha256:' || pg_catalog.encode(
        pg_catalog.sha256(pg_catalog.convert_to(canonical_subject, 'UTF8')),
        'hex'
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.require_runtime_mutation_principal()
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    runtime_role record;
    caller_role record;
    caller_membership record;
    incoming_membership record;
BEGIN
    SELECT
        role_row.oid,
        role_row.rolcanlogin,
        role_row.rolinherit,
        role_row.rolsuper,
        role_row.rolcreatedb,
        role_row.rolcreaterole,
        role_row.rolreplication,
        role_row.rolbypassrls
    INTO STRICT runtime_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = 'cloud_agents_runtime';

    IF runtime_role.rolcanlogin
        OR runtime_role.rolinherit
        OR runtime_role.rolsuper
        OR runtime_role.rolcreatedb
        OR runtime_role.rolcreaterole
        OR runtime_role.rolreplication
        OR runtime_role.rolbypassrls
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = runtime_role.oid
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'runtime mutation group authority drift';
    END IF;

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
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.roleid = caller_role.oid
        )
        OR (
            SELECT pg_catalog.count(*)
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = caller_role.oid
        ) <> 1
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'session user lacks the direct runtime mutation boundary';
    END IF;

    SELECT
        membership.admin_option,
        coalesce(
            (pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean,
            true
        ) AS membership_inherits,
        coalesce(
            (pg_catalog.to_jsonb(membership)->>'set_option')::boolean,
            true
        ) AS membership_is_settable,
        grantor_role.oid AS grantor_oid,
        grantor_role.rolname AS grantor_name,
        grantor_role.rolsuper AS grantor_is_superuser
    INTO caller_membership
    FROM pg_catalog.pg_auth_members AS membership
    JOIN pg_catalog.pg_roles AS grantor_role
        ON grantor_role.oid = membership.grantor
    WHERE membership.roleid = runtime_role.oid
        AND membership.member = caller_role.oid;

    IF NOT FOUND
        OR caller_membership.admin_option
        OR NOT caller_membership.membership_inherits
        OR NOT caller_membership.membership_is_settable
        OR NOT caller_membership.grantor_is_superuser
        OR caller_membership.grantor_name IN (
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin'
        )
        OR NOT pg_catalog.pg_has_role(
            SESSION_USER,
            'cloud_agents_runtime',
            'USAGE'
        )
        OR pg_catalog.pg_has_role(
            SESSION_USER,
            'cloud_agents_migration_owner',
            'MEMBER'
        )
        OR pg_catalog.pg_has_role(
            SESSION_USER,
            'cloud_agents_bootstrap_admin',
            'MEMBER'
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
            MESSAGE = 'session user runtime membership has untrusted provenance';
    END IF;

    FOR incoming_membership IN
        SELECT
            membership.admin_option,
            coalesce(
                (pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean,
                true
            ) AS membership_inherits,
            coalesce(
                (pg_catalog.to_jsonb(membership)->>'set_option')::boolean,
                true
            ) AS membership_is_settable,
            member_role.oid AS member_oid,
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
                runtime_role.oid,
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
        WHERE membership.roleid = runtime_role.oid
        ORDER BY membership.member
    LOOP
        IF incoming_membership.admin_option
            OR NOT incoming_membership.membership_inherits
            OR NOT incoming_membership.membership_is_settable
            OR NOT incoming_membership.member_can_login
            OR NOT incoming_membership.member_inherits
            OR NOT incoming_membership.member_uses_authority
            OR incoming_membership.member_is_superuser
            OR incoming_membership.member_can_create_database
            OR incoming_membership.member_can_create_role
            OR incoming_membership.member_can_replicate
            OR incoming_membership.member_can_bypass_rls
            OR EXISTS (
                SELECT 1
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.roleid = incoming_membership.member_oid
            )
            OR (
                SELECT pg_catalog.count(*)
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = incoming_membership.member_oid
            ) <> 1
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
                MESSAGE = pg_catalog.format(
                    'runtime mutation group has unsafe member %I',
                    incoming_membership.member_name
                );
        END IF;
    END LOOP;

    RETURN SESSION_USER;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.allocate_tenant_revision(
    p_tenant_id text,
    p_expected_revision bigint,
    p_mutation_at timestamptz
)
RETURNS bigint
LANGUAGE plpgsql
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    current_revision bigint;
BEGIN
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_tenant_id)
        OR p_expected_revision < 1
        OR p_mutation_at IS NULL
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'tenant revision allocation input is invalid';
    END IF;

    SELECT revision.current_revision
    INTO current_revision
    FROM cloud_agents.tenant_resource_versions AS revision
    JOIN cloud_agents.platform_tenants AS tenant
        ON tenant.tenant_id = revision.tenant_id
        AND tenant.tenant_uid = revision.tenant_uid
    WHERE revision.tenant_id = p_tenant_id
        AND revision.tenant_uid = p_tenant_id
        AND tenant.state = 'active'
    FOR UPDATE OF revision;

    IF NOT FOUND THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'tenant revision root is unavailable';
    END IF;
    IF current_revision IS DISTINCT FROM p_expected_revision THEN
        RAISE EXCEPTION USING
            ERRCODE = '40001',
            MESSAGE = 'tenant resource revision compare-and-swap failed';
    END IF;
    IF current_revision = 9223372036854775807 THEN
        RAISE EXCEPTION USING
            ERRCODE = '22003',
            MESSAGE = 'tenant resource revision exhausted';
    END IF;

    UPDATE cloud_agents.tenant_resource_versions AS revision
    SET
        current_revision = revision.current_revision + 1,
        updated_at = p_mutation_at
    WHERE revision.tenant_id = p_tenant_id
        AND revision.tenant_uid = p_tenant_id;

    RETURN current_revision + 1;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.create_membership(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_membership_uid text,
    p_membership_name text,
    p_subject_kind text,
    p_subject_issuer text,
    p_subject_value text,
    p_scope_level text,
    p_scope_uid text,
    p_expires_at timestamptz,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    mutation_at timestamptz;
    next_revision bigint;
    subject_digest text;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.clock_timestamp();

    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_membership_uid)
        OR NOT cloud_agents.is_valid_identifier(p_membership_name)
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
        OR p_scope_level NOT IN ('tenant', 'organization', 'project')
        OR NOT cloud_agents.is_valid_identifier(p_scope_uid)
        OR (p_scope_level = 'tenant' AND p_scope_uid IS DISTINCT FROM p_tenant_id)
        OR (p_expires_at IS NOT NULL AND p_expires_at <= mutation_at)
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'membership create input is invalid';
    END IF;

    subject_digest := cloud_agents.subject_ref_digest(
        p_subject_kind,
        p_subject_issuer,
        p_subject_value
    );
    next_revision := cloud_agents.allocate_tenant_revision(
        p_tenant_id,
        p_expected_tenant_revision,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        next_revision,
        'membership',
        p_membership_uid,
        'created',
        actor_principal,
        mutation_at
    );

    INSERT INTO cloud_agents.memberships (
        tenant_id,
        tenant_ref_id,
        membership_uid,
        membership_name,
        subject_kind,
        subject_issuer,
        subject_value,
        subject_digest,
        scope_level,
        scope_tenant_uid,
        scope_organization_uid,
        scope_project_uid,
        state,
        expires_at,
        resource_version,
        created_at,
        updated_at
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        p_membership_uid,
        p_membership_name,
        p_subject_kind,
        p_subject_issuer,
        p_subject_value,
        subject_digest,
        p_scope_level,
        CASE WHEN p_scope_level = 'tenant' THEN p_scope_uid END,
        CASE WHEN p_scope_level = 'organization' THEN p_scope_uid END,
        CASE WHEN p_scope_level = 'project' THEN p_scope_uid END,
        'active',
        p_expires_at,
        next_revision,
        mutation_at,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        p_audit_fact_uid,
        next_revision,
        'membership.create',
        'membership',
        p_membership_uid,
        actor_principal,
        p_reason_code,
        mutation_at
    );

    RETURN QUERY SELECT p_membership_uid, next_revision, 'active'::text;
END
$cloud_agents_function$;

GRANT EXECUTE ON FUNCTION cloud_agents.create_membership(
    text, bigint, text, text, text, text, text, text, text, timestamptz, text, text
) TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.transition_membership(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_membership_uid text,
    p_expected_resource_version bigint,
    p_target_state text,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    mutation_at timestamptz;
    next_revision bigint;
    updated_uid text;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.clock_timestamp();

    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_membership_uid)
        OR p_expected_resource_version < 1
        OR p_target_state NOT IN ('suspended', 'revoked')
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'membership transition input is invalid';
    END IF;

    next_revision := cloud_agents.allocate_tenant_revision(
        p_tenant_id,
        p_expected_tenant_revision,
        mutation_at
    );

    UPDATE cloud_agents.memberships AS membership
    SET
        state = p_target_state,
        resource_version = next_revision,
        updated_at = mutation_at
    WHERE membership.tenant_id = p_tenant_id
        AND membership.membership_uid = p_membership_uid
        AND membership.resource_version = p_expected_resource_version
        AND (
            (p_target_state = 'suspended' AND membership.state = 'active')
            OR (p_target_state = 'revoked' AND membership.state IN ('active', 'suspended'))
        )
    RETURNING membership.membership_uid INTO updated_uid;

    IF NOT FOUND THEN
        RAISE EXCEPTION USING
            ERRCODE = '40001',
            MESSAGE = 'membership resource compare-and-swap failed';
    END IF;

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
        p_tenant_id,
        p_tenant_id,
        next_revision,
        'membership',
        p_membership_uid,
        CASE WHEN p_target_state = 'revoked' THEN 'deleted' ELSE 'updated' END,
        actor_principal,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        p_audit_fact_uid,
        next_revision,
        CASE
            WHEN p_target_state = 'revoked' THEN 'membership.revoke'
            ELSE 'membership.suspend'
        END,
        'membership',
        p_membership_uid,
        actor_principal,
        p_reason_code,
        mutation_at
    );

    RETURN QUERY SELECT updated_uid, next_revision, p_target_state;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.suspend_membership(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_membership_uid text,
    p_expected_resource_version bigint,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.transition_membership(
        p_tenant_id,
        p_expected_tenant_revision,
        p_membership_uid,
        p_expected_resource_version,
        'suspended',
        p_audit_fact_uid,
        p_reason_code
    )
$cloud_agents_function$;

GRANT EXECUTE ON FUNCTION cloud_agents.suspend_membership(text, bigint, text, bigint, text, text)
    TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.revoke_membership(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_membership_uid text,
    p_expected_resource_version bigint,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.transition_membership(
        p_tenant_id,
        p_expected_tenant_revision,
        p_membership_uid,
        p_expected_resource_version,
        'revoked',
        p_audit_fact_uid,
        p_reason_code
    )
$cloud_agents_function$;

GRANT EXECUTE ON FUNCTION cloud_agents.revoke_membership(text, bigint, text, bigint, text, text)
    TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.bind_role(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_role_binding_uid text,
    p_role_binding_name text,
    p_subject_kind text,
    p_subject_issuer text,
    p_subject_value text,
    p_role_name text,
    p_role_version bigint,
    p_scope_level text,
    p_scope_uid text,
    p_expires_at timestamptz,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    mutation_at timestamptz;
    next_revision bigint;
    subject_digest text;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.clock_timestamp();

    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_role_binding_uid)
        OR NOT cloud_agents.is_valid_identifier(p_role_binding_name)
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
        OR p_role_version < 1
        OR p_scope_level NOT IN ('tenant', 'organization', 'project')
        OR NOT cloud_agents.is_valid_identifier(p_scope_uid)
        OR (p_scope_level = 'tenant' AND p_scope_uid IS DISTINCT FROM p_tenant_id)
        OR (p_expires_at IS NOT NULL AND p_expires_at <= mutation_at)
        OR p_role_name = 'platform.admin'
        OR NOT EXISTS (
            SELECT 1
            FROM cloud_agents.builtin_roles AS role
            WHERE role.role_name = p_role_name
                AND role.role_version = p_role_version
                AND role.catalog_revision = 1
                AND role.scope_level = p_scope_level
                AND role.state = 'active'
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'role binding input is invalid';
    END IF;

    subject_digest := cloud_agents.subject_ref_digest(
        p_subject_kind,
        p_subject_issuer,
        p_subject_value
    );
    next_revision := cloud_agents.allocate_tenant_revision(
        p_tenant_id,
        p_expected_tenant_revision,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        next_revision,
        'role_binding',
        p_role_binding_uid,
        'created',
        actor_principal,
        mutation_at
    );

    INSERT INTO cloud_agents.role_bindings (
        tenant_id,
        tenant_ref_id,
        role_binding_uid,
        role_binding_name,
        subject_kind,
        subject_issuer,
        subject_value,
        subject_digest,
        role_name,
        role_version,
        scope_level,
        scope_tenant_uid,
        scope_organization_uid,
        scope_project_uid,
        state,
        expires_at,
        resource_version,
        created_at,
        updated_at
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        p_role_binding_uid,
        p_role_binding_name,
        p_subject_kind,
        p_subject_issuer,
        p_subject_value,
        subject_digest,
        p_role_name,
        p_role_version,
        p_scope_level,
        CASE WHEN p_scope_level = 'tenant' THEN p_scope_uid END,
        CASE WHEN p_scope_level = 'organization' THEN p_scope_uid END,
        CASE WHEN p_scope_level = 'project' THEN p_scope_uid END,
        'active',
        p_expires_at,
        next_revision,
        mutation_at,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        p_audit_fact_uid,
        next_revision,
        'role_binding.bind',
        'role_binding',
        p_role_binding_uid,
        actor_principal,
        p_reason_code,
        mutation_at
    );

    RETURN QUERY SELECT p_role_binding_uid, next_revision, 'active'::text;
END
$cloud_agents_function$;

GRANT EXECUTE ON FUNCTION cloud_agents.bind_role(
    text, bigint, text, text, text, text, text, text, bigint, text, text,
    timestamptz, text, text
) TO cloud_agents_runtime;

CREATE FUNCTION cloud_agents.revoke_role_binding(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_role_binding_uid text,
    p_expected_resource_version bigint,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    mutation_at timestamptz;
    next_revision bigint;
    updated_uid text;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.clock_timestamp();

    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_role_binding_uid)
        OR p_expected_resource_version < 1
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'role binding revoke input is invalid';
    END IF;

    next_revision := cloud_agents.allocate_tenant_revision(
        p_tenant_id,
        p_expected_tenant_revision,
        mutation_at
    );

    UPDATE cloud_agents.role_bindings AS binding
    SET
        state = 'revoked',
        resource_version = next_revision,
        updated_at = mutation_at
    WHERE binding.tenant_id = p_tenant_id
        AND binding.role_binding_uid = p_role_binding_uid
        AND binding.resource_version = p_expected_resource_version
        AND binding.state = 'active'
    RETURNING binding.role_binding_uid INTO updated_uid;

    IF NOT FOUND THEN
        RAISE EXCEPTION USING
            ERRCODE = '40001',
            MESSAGE = 'role binding resource compare-and-swap failed';
    END IF;

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
        p_tenant_id,
        p_tenant_id,
        next_revision,
        'role_binding',
        p_role_binding_uid,
        'deleted',
        actor_principal,
        mutation_at
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
        p_tenant_id,
        p_tenant_id,
        p_audit_fact_uid,
        next_revision,
        'role_binding.revoke',
        'role_binding',
        p_role_binding_uid,
        actor_principal,
        p_reason_code,
        mutation_at
    );

    RETURN QUERY SELECT updated_uid, next_revision, 'revoked'::text;
END
$cloud_agents_function$;

GRANT EXECUTE ON FUNCTION cloud_agents.revoke_role_binding(text, bigint, text, bigint, text, text)
    TO cloud_agents_runtime;

REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA cloud_agents FROM PUBLIC;
