package main

import "testing"

func TestParseProductMigrationConfigUsesDatabaseEnvironment(t *testing.T) {
	config, err := parseProductMigrationConfig([]string{"--repository-root", "/srv/cloud-agents"}, func(key string) string {
		if key == databaseURLEnvironment {
			return "postgres://migration@db/cloud_agents?sslmode=require"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.databaseURL == "" || config.repositoryRoot != "/srv/cloud-agents" || config.selector != "product-000044" {
		t.Fatalf("config = %+v", config)
	}
}

func TestParseProductMigrationConfigRejectsSelectorDrift(t *testing.T) {
	if _, err := parseProductMigrationConfig([]string{"--selector", "product-000017", "--database-url", "postgres://migration@db/cloud_agents"}, nil); err == nil {
		t.Fatal("accepted a non-current independent product selector")
	}
}
