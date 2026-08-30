ALTER TABLE cloud_agents.audit_facts
    DROP CONSTRAINT audit_facts_action;

ALTER TABLE cloud_agents.audit_facts
    ADD CONSTRAINT audit_facts_action
    CHECK (
        action IN (
            'platform_tenant.bootstrap',
            'organization.create',
            'membership.create',
            'membership.resume',
            'membership.suspend',
            'membership.revoke',
            'role_binding.bind',
            'role_binding.revoke'
        )
    );

CREATE OR REPLACE FUNCTION cloud_agents.transition_membership(
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
        OR p_target_state NOT IN ('active', 'suspended', 'revoked')
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
            (
                p_target_state = 'active'
                AND membership.state = 'suspended'
                AND (membership.expires_at IS NULL OR mutation_at < membership.expires_at)
            )
            OR (p_target_state = 'suspended' AND membership.state = 'active')
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
        CASE p_target_state
            WHEN 'active' THEN 'membership.resume'
            WHEN 'revoked' THEN 'membership.revoke'
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

CREATE FUNCTION cloud_agents.resume_membership(
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
        'active',
        p_audit_fact_uid,
        p_reason_code
    )
$cloud_agents_function$;

REVOKE ALL ON FUNCTION cloud_agents.resume_membership(text, bigint, text, bigint, text, text)
    FROM PUBLIC;

GRANT EXECUTE ON FUNCTION cloud_agents.resume_membership(text, bigint, text, bigint, text, text)
    TO cloud_agents_runtime;
