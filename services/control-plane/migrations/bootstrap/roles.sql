-- Cluster-scoped bootstrap for Cloud Agents database group roles.
--
-- This file intentionally creates no LOGIN role, credential, schema, table, or
-- business data. Deployment-owned workload LOGIN roles are provisioned outside
-- this repository and may be granted membership in exactly one group role.

DO $cloud_agents_roles$
DECLARE
    caller_role record;
    required_role text;
    existing_role record;
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
            'cloud_agents_bootstrap_admin'
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
                'cloud_agents_bootstrap_admin'
            )
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'role bootstrap requires an isolated unswitched superuser LOGIN';
    END IF;

    FOREACH required_role IN ARRAY ARRAY[
        'cloud_agents_migration_owner',
        'cloud_agents_runtime',
        'cloud_agents_bootstrap_admin'
    ]
    LOOP
        SELECT
            r.oid,
            r.rolcanlogin,
            r.rolsuper,
            r.rolinherit,
            r.rolcreaterole,
            r.rolcreatedb,
            r.rolreplication,
            r.rolbypassrls
        INTO existing_role
        FROM pg_catalog.pg_roles AS r
        WHERE r.rolname = required_role;

        IF NOT FOUND THEN
            EXECUTE pg_catalog.format(
                'CREATE ROLE %I NOLOGIN NOSUPERUSER NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS',
                required_role
            );
        END IF;

        SELECT
            r.oid,
            r.rolcanlogin,
            r.rolsuper,
            r.rolinherit,
            r.rolcreaterole,
            r.rolcreatedb,
            r.rolreplication,
            r.rolbypassrls
        INTO STRICT existing_role
        FROM pg_catalog.pg_roles AS r
        WHERE r.rolname = required_role;

        IF existing_role.rolcanlogin
            OR existing_role.rolsuper
            OR existing_role.rolinherit
            OR existing_role.rolcreaterole
            OR existing_role.rolcreatedb
            OR existing_role.rolreplication
            OR existing_role.rolbypassrls
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = pg_catalog.format(
                    'database role %I has attributes incompatible with the Cloud Agents authority boundary',
                    required_role
                );
        END IF;

        IF EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = existing_role.oid
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = pg_catalog.format(
                    'database role %I inherits another role; refusing bootstrap',
                    required_role
                );
        END IF;

        FOR incoming_membership IN
            SELECT
                membership.admin_option,
                membership.member,
                coalesce(
                    (pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean,
                    true
                ) AS membership_inherits,
                coalesce(
                    (pg_catalog.to_jsonb(membership)->>'set_option')::boolean,
                    true
                ) AS membership_is_settable,
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
                    existing_role.oid,
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
            WHERE membership.roleid = existing_role.oid
            ORDER BY membership.admin_option, membership.member
        LOOP
            IF NOT incoming_membership.member_can_login
                OR incoming_membership.member_is_superuser
                OR incoming_membership.member_can_create_database
                OR incoming_membership.member_can_create_role
                OR incoming_membership.member_can_replicate
                OR incoming_membership.member_can_bypass_rls
                OR (
                    required_role = 'cloud_agents_migration_owner'
                    AND (
                        incoming_membership.member_inherits
                        OR incoming_membership.member_uses_authority
                        OR NOT incoming_membership.membership_is_settable
                    )
                )
                OR (
                    required_role IN (
                        'cloud_agents_runtime',
                        'cloud_agents_bootstrap_admin'
                    )
                    AND (
                        NOT incoming_membership.member_inherits
                        OR NOT incoming_membership.membership_inherits
                        OR NOT incoming_membership.member_uses_authority
                    )
                )
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
                        AND membership.roleid <> existing_role.oid
                )
            THEN
                RAISE EXCEPTION USING
                    ERRCODE = '42501',
                    MESSAGE = pg_catalog.format(
                        'database role %I has unsafe member %I',
                        required_role,
                        incoming_membership.member_name
                    );
            END IF;

            IF incoming_membership.admin_option THEN
                RAISE EXCEPTION USING
                    ERRCODE = '42501',
                    MESSAGE = pg_catalog.format(
                        'database role %I has a delegable member; refusing bootstrap',
                        required_role
                    );
            END IF;

            IF NOT incoming_membership.grantor_is_superuser
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
                        'database role %I has membership from untrusted grantor %I',
                        required_role,
                        incoming_membership.grantor_name
                    );
            END IF;
        END LOOP;
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
            'cloud_agents_bootstrap_admin'
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
$cloud_agents_roles$;
