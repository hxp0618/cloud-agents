SELECT *
FROM cloud_agents.bootstrap_tenant_administrator_v1(
    :'cloud_agents_tenant_uid',
    :'cloud_agents_tenant_name',
    :'cloud_agents_tenant_display_name',
    :'cloud_agents_organization_uid',
    :'cloud_agents_organization_name',
    :'cloud_agents_organization_display_name',
    :'cloud_agents_admin_subject_kind',
    :'cloud_agents_admin_subject_issuer',
    :'cloud_agents_admin_subject_value',
    :'cloud_agents_admin_membership_uid',
    :'cloud_agents_admin_membership_name',
    :'cloud_agents_admin_role_binding_uid',
    :'cloud_agents_admin_role_binding_name',
    :'cloud_agents_tenant_audit_fact_uid',
    :'cloud_agents_membership_audit_fact_uid',
    :'cloud_agents_role_binding_audit_fact_uid',
    :'cloud_agents_bootstrap_reason_code'
);
