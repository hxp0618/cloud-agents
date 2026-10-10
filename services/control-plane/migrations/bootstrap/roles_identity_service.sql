-- Additive cluster bootstrap for the identity service authority group.
-- Run after roles.sql as the same isolated, unswitched superuser LOGIN.

DO $cloud_agents_identity_service_role$
DECLARE
    caller_role record;
    service_role record;
    incoming_membership record;
    overlapping_member_name text;
BEGIN
    SELECT
        role_row.oid,
        role_row.rolname,
        role_row.rolcanlogin,
        role_row.rolsuper
    INTO STRICT caller_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = SESSION_USER;

    IF CURRENT_USER IS DISTINCT FROM SESSION_USER
        OR NOT caller_role.rolcanlogin
        OR NOT caller_role.rolsuper
        OR caller_role.rolname IN (
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin',
            'cloud_agents_identity_service'
        )
        OR EXISTS (
            WITH RECURSIVE caller_memberships (roleid) AS (
                SELECT membership.roleid
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = caller_role.oid

                UNION

                SELECT membership.roleid
                FROM pg_catalog.pg_auth_members AS membership
                JOIN caller_memberships
                    ON caller_memberships.roleid = membership.member
            )
            SELECT 1
            FROM caller_memberships
            JOIN pg_catalog.pg_roles AS inherited_role
                ON inherited_role.oid = caller_memberships.roleid
            WHERE inherited_role.rolname IN (
                'cloud_agents_migration_owner',
                'cloud_agents_runtime',
                'cloud_agents_bootstrap_admin',
                'cloud_agents_identity_service'
            )
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'identity role bootstrap requires an isolated unswitched superuser LOGIN';
    END IF;

    IF (
        SELECT pg_catalog.count(*)
        FROM pg_catalog.pg_roles AS role_row
        WHERE role_row.rolname IN (
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin'
        )
    ) <> 3
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'identity role bootstrap requires the legacy authority roles';
    END IF;

    SELECT
        role_row.oid,
        role_row.rolcanlogin,
        role_row.rolsuper,
        role_row.rolinherit,
        role_row.rolcreaterole,
        role_row.rolcreatedb,
        role_row.rolreplication,
        role_row.rolbypassrls
    INTO service_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = 'cloud_agents_identity_service';

    IF NOT FOUND THEN
        CREATE ROLE cloud_agents_identity_service
            NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
    END IF;

    SELECT
        role_row.oid,
        role_row.rolcanlogin,
        role_row.rolsuper,
        role_row.rolinherit,
        role_row.rolcreaterole,
        role_row.rolcreatedb,
        role_row.rolreplication,
        role_row.rolbypassrls
    INTO STRICT service_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = 'cloud_agents_identity_service';

    IF service_role.rolcanlogin
        OR service_role.rolsuper
        OR service_role.rolinherit
        OR service_role.rolcreaterole
        OR service_role.rolcreatedb
        OR service_role.rolreplication
        OR service_role.rolbypassrls
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = service_role.oid
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'database role cloud_agents_identity_service has unsafe authority';
    END IF;

    FOR incoming_membership IN
        SELECT
            membership.admin_option,
            coalesce(
                (pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean,
                true
            ) AS membership_inherits,
            member_role.oid AS member_oid,
            member_role.rolname AS member_name,
            member_role.rolcanlogin AS member_can_login,
            member_role.rolinherit AS member_inherits,
            member_role.rolsuper AS member_is_superuser,
            member_role.rolcreatedb AS member_can_create_database,
            member_role.rolcreaterole AS member_can_create_role,
            member_role.rolreplication AS member_can_replicate,
            member_role.rolbypassrls AS member_can_bypass_rls,
            pg_catalog.pg_has_role(member_role.oid, service_role.oid, 'USAGE') AS member_uses_authority,
            grantor_role.oid AS grantor_oid,
            grantor_role.rolname AS grantor_name,
            grantor_role.rolsuper AS grantor_is_superuser
        FROM pg_catalog.pg_auth_members AS membership
        JOIN pg_catalog.pg_roles AS member_role
            ON member_role.oid = membership.member
        JOIN pg_catalog.pg_roles AS grantor_role
            ON grantor_role.oid = membership.grantor
        WHERE membership.roleid = service_role.oid
        ORDER BY membership.admin_option, membership.member
    LOOP
        IF incoming_membership.admin_option
            OR NOT incoming_membership.member_can_login
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
                WHERE membership.roleid = incoming_membership.member_oid
            )
            OR (
                SELECT pg_catalog.count(*)
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = incoming_membership.member_oid
            ) <> 1
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = pg_catalog.format(
                    'database role cloud_agents_identity_service has unsafe member %I',
                    incoming_membership.member_name
                );
        END IF;

        IF NOT incoming_membership.grantor_is_superuser
            OR incoming_membership.grantor_name IN (
                'cloud_agents_migration_owner',
                'cloud_agents_runtime',
                'cloud_agents_bootstrap_admin',
                'cloud_agents_identity_service'
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
                    'cloud_agents_bootstrap_admin',
                    'cloud_agents_identity_service'
                )
            )
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = pg_catalog.format(
                    'database role cloud_agents_identity_service has membership from untrusted grantor %I',
                    incoming_membership.grantor_name
                );
        END IF;
    END LOOP;

    WITH RECURSIVE membership_closure (candidate_oid, roleid) AS (
        SELECT membership.member, membership.roleid
        FROM pg_catalog.pg_auth_members AS membership

        UNION

        SELECT membership_closure.candidate_oid, membership.roleid
        FROM membership_closure
        JOIN pg_catalog.pg_auth_members AS membership
            ON membership.member = membership_closure.roleid
    ), overlapping_members AS (
        SELECT membership_closure.candidate_oid
        FROM membership_closure
        JOIN pg_catalog.pg_roles AS authority_role
            ON authority_role.oid = membership_closure.roleid
        WHERE authority_role.rolname IN (
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin',
            'cloud_agents_identity_service'
        )
        GROUP BY membership_closure.candidate_oid
        HAVING pg_catalog.count(DISTINCT membership_closure.roleid) > 1
    )
    SELECT candidate_role.rolname
    INTO overlapping_member_name
    FROM overlapping_members
    JOIN pg_catalog.pg_roles AS candidate_role
        ON candidate_role.oid = overlapping_members.candidate_oid
    ORDER BY candidate_role.rolname
    LIMIT 1;

    IF FOUND THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = pg_catalog.format(
                'database role %I resolves to multiple Cloud Agents authorities',
                overlapping_member_name
            );
    END IF;
END
$cloud_agents_identity_service_role$;
