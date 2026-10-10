CREATE SCHEMA cloud_agents_identity AUTHORIZATION cloud_agents_migration_owner;

REVOKE ALL ON SCHEMA cloud_agents_identity FROM PUBLIC;
GRANT USAGE ON SCHEMA cloud_agents_identity TO cloud_agents_identity_service;

ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner IN SCHEMA cloud_agents_identity
    REVOKE ALL ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner IN SCHEMA cloud_agents_identity
    REVOKE ALL ON SEQUENCES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner
    REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
