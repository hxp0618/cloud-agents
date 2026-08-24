package migration

type pg17Normalizer struct{}

func (pg17Normalizer) membershipOptions(inherit, set *string) (bool, bool, error) {
	inheritOption, err := boolText(inherit, "membership.inherit_option")
	if err != nil {
		return false, false, err
	}
	setOption, err := boolText(set, "membership.set_option")
	if err != nil {
		return false, false, err
	}
	return inheritOption, setOption, nil
}

func (pg17Normalizer) usageEdgeAllowed(_ RoleProjection, edge membershipGraphEdge) bool {
	return edge.inherit
}

func (pg17Normalizer) databaseProfile(provider string, icuLocale, icuRules, collationVersion *string) (string, *string, *string, *string, error) {
	return normalizeLibcDatabaseProfile(17, provider, icuLocale, icuRules, collationVersion)
}
