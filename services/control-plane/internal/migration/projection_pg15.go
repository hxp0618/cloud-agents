package migration

type pg15Normalizer struct{}

func (pg15Normalizer) membershipOptions(inherit, set *string) (bool, bool, error) {
	if inherit != nil || set != nil {
		return false, false, pgProjectionFailure(CodeProjectionCapabilityMismatch, "membership.options", 15, "PostgreSQL 15 exposed post-15 membership options")
	}
	// PG15 has no per-edge INHERIT/SET options. Both legacy edges are enabled;
	// role-level rolinherit is applied separately for USAGE reachability.
	return true, true, nil
}

func (pg15Normalizer) usageEdgeAllowed(current RoleProjection, edge membershipGraphEdge) bool {
	return current.Inherit && edge.inherit
}

func (pg15Normalizer) databaseProfile(provider string, icuLocale, icuRules, collationVersion *string) (string, *string, *string, *string, error) {
	return normalizeLibcDatabaseProfile(15, provider, icuLocale, icuRules, collationVersion)
}
