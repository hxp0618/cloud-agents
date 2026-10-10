INSERT INTO cloud_agents.builtin_roles (
    role_name,
    role_version,
    catalog_revision,
    scope_level,
    state,
    published_at
)
VALUES ('platform.admin', 2, 2, 'platform', 'active', '2026-10-08T00:00:00Z');

INSERT INTO cloud_agents.builtin_role_permissions (role_name, role_version, permission)
SELECT 'platform.admin', 2, permission
FROM pg_catalog.unnest(ARRAY[
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
]::text[]) AS permission;
