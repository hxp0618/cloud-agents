ALTER TABLE cloud_agents.deployment_targets
    DROP CONSTRAINT deployment_targets_endpoint;
ALTER TABLE cloud_agents.deployment_targets
    ADD CONSTRAINT deployment_targets_endpoint CHECK (
        target_kind = 'ssh'
            AND pg_catalog.octet_length(endpoint) BETWEEN 7 AND 2048
            AND endpoint ~ '^ssh://[^/?#[:space:]@]+/?$'
        OR target_kind IN ('docker', 'kubernetes')
            AND pg_catalog.octet_length(endpoint) BETWEEN 9 AND 2048
            AND endpoint ~ '^https://[^/?#[:space:]@]+/?$'
    );
