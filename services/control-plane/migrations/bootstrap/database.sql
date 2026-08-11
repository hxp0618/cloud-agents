\set ON_ERROR_STOP on

-- Database-scoped bootstrap for the Cloud Agents migration authority.
--
-- The caller must provide two non-secret psql variables:
--   cloud_agents_database       exact target database name
--   cloud_agents_database_owner exact existing deployment-owned database owner
--
-- This script does not create a database, schema, LOGIN role, credential, or
-- business data. It grants only the database CREATE privilege needed by the
-- dedicated migration LOGIN after SET ROLE cloud_agents_migration_owner and
-- removes TEMPORARY from all Cloud Agents workload authorities.

\if :{?cloud_agents_database}
\else
DO $cloud_agents_missing_database$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '22023',
        MESSAGE = 'cloud_agents_database psql variable is required';
END
$cloud_agents_missing_database$;
\endif

\if :{?cloud_agents_database_owner}
\else
DO $cloud_agents_missing_database_owner$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '22023',
        MESSAGE = 'cloud_agents_database_owner psql variable is required';
END
$cloud_agents_missing_database_owner$;
\endif

SELECT pg_catalog.set_config(
    'cloud_agents.bootstrap.expected_database',
    :'cloud_agents_database',
    false
);
SELECT pg_catalog.set_config(
    'cloud_agents.bootstrap.expected_database_owner',
    :'cloud_agents_database_owner',
    false
);

DO $cloud_agents_database$
DECLARE
    target_database record;
    caller_role record;
    required_role text;
    existing_required_role record;
    incoming_membership record;
    overlapping_member_name text;
    unknown_database_grantee text;
    unknown_database_privilege text;
BEGIN
    SELECT
        database_row.oid,
        database_row.datname,
        database_row.datdba,
        database_row.datallowconn,
        database_row.datistemplate,
        owner_role.rolname AS owner_name,
        owner_role.rolcanlogin AS owner_can_login,
        owner_role.rolsuper AS owner_is_superuser,
        owner_role.rolcreatedb AS owner_can_create_database,
        owner_role.rolcreaterole AS owner_can_create_role,
        owner_role.rolreplication AS owner_can_replicate,
        owner_role.rolbypassrls AS owner_can_bypass_rls
    INTO STRICT target_database
    FROM pg_catalog.pg_database AS database_row
    JOIN pg_catalog.pg_roles AS owner_role
        ON owner_role.oid = database_row.datdba
    WHERE database_row.datname = pg_catalog.current_database();

    IF target_database.datname
        IS DISTINCT FROM pg_catalog.current_setting(
            'cloud_agents.bootstrap.expected_database'
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'connected database does not match cloud_agents_database';
    END IF;

    IF target_database.owner_name
        IS DISTINCT FROM pg_catalog.current_setting(
            'cloud_agents.bootstrap.expected_database_owner'
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'database owner does not match cloud_agents_database_owner';
    END IF;

    IF NOT target_database.datallowconn OR target_database.datistemplate THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'target database is not an ordinary connectable database';
    END IF;

    FOREACH required_role IN ARRAY ARRAY[
        'cloud_agents_migration_owner',
        'cloud_agents_runtime',
        'cloud_agents_bootstrap_admin'
    ]
    LOOP
        SELECT
            role_row.oid,
            role_row.rolcanlogin,
            role_row.rolsuper,
            role_row.rolinherit,
            role_row.rolcreatedb,
            role_row.rolcreaterole,
            role_row.rolreplication,
            role_row.rolbypassrls
        INTO existing_required_role
        FROM pg_catalog.pg_roles AS role_row
        WHERE role_row.rolname = required_role;

        IF NOT FOUND
            OR existing_required_role.rolcanlogin
            OR existing_required_role.rolsuper
            OR existing_required_role.rolinherit
            OR existing_required_role.rolcreatedb
            OR existing_required_role.rolcreaterole
            OR existing_required_role.rolreplication
            OR existing_required_role.rolbypassrls
        THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = pg_catalog.format(
                    'database role %I is absent or has incompatible attributes',
                    required_role
                );
        END IF;

        IF EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = existing_required_role.oid
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = pg_catalog.format(
                    'database role %I inherits another role; refusing database bootstrap',
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
                    existing_required_role.oid,
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
            WHERE membership.roleid = existing_required_role.oid
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
                        AND membership.roleid <> existing_required_role.oid
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
                        'database role %I has a delegable member; refusing database bootstrap',
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

    IF target_database.owner_name IN (
        'cloud_agents_migration_owner',
        'cloud_agents_runtime',
        'cloud_agents_bootstrap_admin'
    )
        OR target_database.owner_is_superuser
        OR target_database.owner_can_create_database
        OR target_database.owner_can_create_role
        OR target_database.owner_can_replicate
        OR target_database.owner_can_bypass_rls
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'target database owner violates the deployment-owner boundary';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM pg_catalog.pg_auth_members AS membership
        WHERE membership.roleid = target_database.datdba
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'target database owner is delegated to another role';
    END IF;

    FOREACH required_role IN ARRAY ARRAY[
        'cloud_agents_migration_owner',
        'cloud_agents_runtime',
        'cloud_agents_bootstrap_admin'
    ]
    LOOP
        IF pg_catalog.pg_has_role(
            target_database.owner_name,
            required_role,
            'MEMBER'
        ) THEN
            RAISE EXCEPTION USING
                ERRCODE = '42501',
                MESSAGE = 'target database owner has conflicting Cloud Agents authority';
        END IF;
    END LOOP;

    SELECT
        role_row.rolcanlogin,
        role_row.rolsuper
    INTO STRICT caller_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = SESSION_USER;

    IF NOT caller_role.rolcanlogin OR CURRENT_USER IS DISTINCT FROM SESSION_USER THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'database bootstrap requires an unswitched LOGIN session';
    END IF;

    IF NOT caller_role.rolsuper THEN
        FOREACH required_role IN ARRAY ARRAY[
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin'
        ]
        LOOP
            IF pg_catalog.pg_has_role(SESSION_USER, required_role, 'MEMBER') THEN
                RAISE EXCEPTION USING
                    ERRCODE = '42501',
                    MESSAGE = 'database bootstrap caller has conflicting Cloud Agents authority';
            END IF;
        END LOOP;
    END IF;

    IF NOT caller_role.rolsuper
        AND (
            SESSION_USER IS DISTINCT FROM target_database.owner_name
            OR NOT target_database.owner_can_login
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'database bootstrap caller must be the LOGIN owner or a superuser';
    END IF;

    SELECT
        coalesce(grantee_role.rolname, 'PUBLIC'),
        database_acl.privilege_type
    INTO unknown_database_grantee, unknown_database_privilege
    FROM pg_catalog.aclexplode(
        coalesce(
            (
                SELECT database_row.datacl
                FROM pg_catalog.pg_database AS database_row
                WHERE database_row.oid = target_database.oid
            ),
            pg_catalog.acldefault('d', target_database.datdba)
        )
    ) AS database_acl
    LEFT JOIN pg_catalog.pg_roles AS grantee_role
        ON grantee_role.oid = database_acl.grantee
    WHERE database_acl.privilege_type IN ('CREATE', 'TEMPORARY')
        AND database_acl.grantee <> target_database.datdba
        AND coalesce(grantee_role.rolname, 'PUBLIC') NOT IN (
            'PUBLIC',
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin'
        )
    LIMIT 1;

    IF unknown_database_grantee IS NOT NULL THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = pg_catalog.format(
                'database %s privilege has unknown grantee %I',
                unknown_database_privilege,
                unknown_database_grantee
            );
    END IF;

    EXECUTE pg_catalog.format(
        'REVOKE CREATE, TEMPORARY ON DATABASE %I FROM PUBLIC, cloud_agents_migration_owner, cloud_agents_runtime, cloud_agents_bootstrap_admin',
        target_database.datname
    );
    EXECUTE pg_catalog.format(
        'GRANT CREATE ON DATABASE %I TO cloud_agents_migration_owner',
        target_database.datname
    );

    IF NOT pg_catalog.has_database_privilege(
        'cloud_agents_migration_owner',
        target_database.datname,
        'CREATE'
    )
        OR pg_catalog.has_database_privilege(
            'cloud_agents_migration_owner',
            target_database.datname,
            'TEMPORARY'
        )
        OR pg_catalog.has_database_privilege(
            'cloud_agents_runtime',
            target_database.datname,
            'CREATE'
        )
        OR pg_catalog.has_database_privilege(
            'cloud_agents_runtime',
            target_database.datname,
            'TEMPORARY'
        )
        OR pg_catalog.has_database_privilege(
            'cloud_agents_bootstrap_admin',
            target_database.datname,
            'CREATE'
        )
        OR pg_catalog.has_database_privilege(
            'cloud_agents_bootstrap_admin',
            target_database.datname,
            'TEMPORARY'
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'database CREATE/TEMPORARY privilege closure does not match the frozen boundary';
    END IF;
END
$cloud_agents_database$;
