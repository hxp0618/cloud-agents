package migration

import (
	"context"
	"sort"
	"strings"
)

type pgAuthorityComponents struct {
	DatabaseName         string
	DatabaseOwner        string
	DatabaseEncoding     string
	LocaleProvider       string
	Datcollate           string
	Datctype             string
	ICULocale            *string
	ICURules             *string
	CollationVersion     *string
	DatabaseACL          ACLSetProjection
	Roles                []RoleProjection
	RolesByName          map[string]RoleProjection
	DirectMemberships    []DirectMembershipProjection
	DatabaseRoleSettings []DatabaseRoleSettingProjection
	EffectiveCreate      map[string]bool
	EffectiveTemporary   map[string]bool
}

func (projector *PGProjector) readAuthorityComponents(ctx context.Context, snapshot ProjectionSnapshot, expected AuthorityProjection) (pgAuthorityComponents, error) {
	roleNames := make([]string, len(expected.Roles))
	for index, role := range expected.Roles {
		roleNames[index] = role.Name
	}
	if err := requireSortedUniqueStrings("authority.roles", roleNames); err != nil {
		return pgAuthorityComponents{}, err
	}
	roles, rolesByName, err := projector.readRoles(ctx, snapshot, roleNames)
	if err != nil {
		return pgAuthorityComponents{}, err
	}
	memberships, err := projector.readDirectMemberships(ctx, snapshot, roleNames, rolesByName)
	if err != nil {
		return pgAuthorityComponents{}, err
	}
	database, err := projector.readDatabaseAuthority(ctx, snapshot, roleNames)
	if err != nil {
		return pgAuthorityComponents{}, err
	}
	settings, err := projector.readRoleSettings(ctx, snapshot, database.DatabaseName, roleNames)
	if err != nil {
		return pgAuthorityComponents{}, err
	}
	database.Roles = roles
	database.RolesByName = rolesByName
	database.DirectMemberships = memberships
	database.DatabaseRoleSettings = settings
	return database, nil
}

func (projector *PGProjector) readRoles(ctx context.Context, queryer catalogQueryer, names []string) ([]RoleProjection, map[string]RoleProjection, error) {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryAuthorityRoles, names)
	if err != nil {
		return nil, nil, err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	roles := make([]RoleProjection, 0, len(names))
	byName := make(map[string]RoleProjection, len(names))
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}
	for rows.Next() {
		var role RoleProjection
		if err := rows.Scan(&role.Name, &role.Login, &role.Inherit, &role.Superuser, &role.CreateRole, &role.CreateDB, &role.Replication, &role.BypassRLS, &role.ConnectionLimitInt32Decimal, &role.ValidUntil, &role.Config); err != nil {
			return nil, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.roles.scan", projector.major, "role projection scan failed")
		}
		values := append([]string{role.Name, role.ConnectionLimitInt32Decimal, nullableString(role.ValidUntil)}, role.Config...)
		if err := budget.add("authority.roles", values...); err != nil {
			return nil, nil, err
		}
		if _, ok := allowed[role.Name]; !ok {
			return nil, nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.roles", projector.major, "catalog returned an unbound role")
		}
		if _, duplicate := byName[role.Name]; duplicate {
			return nil, nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.roles", projector.major, "catalog returned a duplicate role")
		}
		connectionLimit, err := ValidateSignedIntegerDecimal(role.ConnectionLimitInt32Decimal, 32)
		if err != nil || connectionLimit < -1 {
			return nil, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.roles.connection_limit", projector.major, "role connection limit is invalid")
		}
		sort.Strings(role.Config)
		if err := validateRoleSettings(role.Config); err != nil {
			return nil, nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.roles.config", projector.major, "role setting is outside the safe allowlist")
		}
		byName[role.Name] = role
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.roles.iteration", projector.major, "role projection iteration failed")
	}
	if len(roles) != len(names) {
		return nil, nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.roles", projector.major, "one or more bound roles are absent")
	}
	sort.Slice(roles, func(left, right int) bool { return roles[left].Name < roles[right].Name })
	return roles, byName, nil
}

func (projector *PGProjector) readDirectMemberships(ctx context.Context, queryer catalogQueryer, names []string, roles map[string]RoleProjection) ([]DirectMembershipProjection, error) {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryAuthorityMemberships, names)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	memberships := make([]DirectMembershipProjection, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var membership DirectMembershipProjection
		var inheritText, setText *string
		if err := rows.Scan(&membership.Role, &membership.Member, &membership.Grantor, &membership.AdminOption, &inheritText, &setText); err != nil {
			return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.memberships.scan", projector.major, "membership projection scan failed")
		}
		if err := budget.add("authority.memberships", membership.Role, membership.Member, membership.Grantor, nullableString(inheritText), nullableString(setText)); err != nil {
			return nil, err
		}
		if uint64(len(memberships)) >= projectionMaxMembershipEdges {
			return nil, pgProjectionFailure(CodeProjectionLimitExceeded, "authority.memberships", projector.major, "membership edge limit exceeded")
		}
		if _, ok := roles[membership.Role]; !ok {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.memberships.role", projector.major, "membership role is outside the verified principal closure")
		}
		if _, ok := roles[membership.Member]; !ok {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.memberships.member", projector.major, "membership member is outside the verified principal closure")
		}
		if _, ok := roles[membership.Grantor]; !ok {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.memberships.grantor", projector.major, "membership grantor is outside the verified principal closure")
		}
		membership.InheritOption, membership.SetOption, err = projector.normalizer.membershipOptions(inheritText, setText)
		if err != nil {
			return nil, err
		}
		key := membership.Role + "\x00" + membership.Member + "\x00" + membership.Grantor
		if _, duplicate := seen[key]; duplicate {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.memberships", projector.major, "duplicate direct membership edge")
		}
		seen[key] = struct{}{}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.memberships.iteration", projector.major, "membership projection iteration failed")
	}
	sort.Slice(memberships, func(left, right int) bool {
		leftKey := memberships[left].Role + "\x00" + memberships[left].Member + "\x00" + memberships[left].Grantor
		rightKey := memberships[right].Role + "\x00" + memberships[right].Member + "\x00" + memberships[right].Grantor
		return leftKey < rightKey
	})
	return memberships, nil
}

func (projector *PGProjector) readDatabaseAuthority(ctx context.Context, queryer catalogQueryer, principals []string) (pgAuthorityComponents, error) {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryDatabaseAuthority, principals)
	if err != nil {
		return pgAuthorityComponents{}, err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	result := pgAuthorityComponents{EffectiveCreate: make(map[string]bool, len(principals)), EffectiveTemporary: make(map[string]bool, len(principals))}
	allowedPrincipals := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		allowedPrincipals[principal] = struct{}{}
	}
	var databaseSeen bool
	var acl *aclAccumulator
	for rows.Next() {
		var rowKind, databaseName, owner string
		var encoding, provider, datcollate, datctype *string
		var icuLocale, icuRules, collationVersion *string
		var aclIsNull bool
		var grantor, grantee, privilege *string
		var isGrantable *bool
		var principal *string
		var effectiveCreate, effectiveTemporary *bool
		if err := rows.Scan(&rowKind, &databaseName, &owner, &encoding, &provider, &datcollate, &datctype, &icuLocale, &icuRules, &collationVersion, &aclIsNull, &grantor, &grantee, &privilege, &isGrantable, &principal, &effectiveCreate, &effectiveTemporary); err != nil {
			return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.database.scan", projector.major, "database authority scan failed")
		}
		if err := budget.add("authority.database", rowKind, databaseName, owner, nullableString(encoding), nullableString(provider), nullableString(datcollate), nullableString(datctype), nullableString(icuLocale), nullableString(icuRules), nullableString(collationVersion), nullableString(grantor), nullableString(grantee), nullableString(privilege), nullableString(principal)); err != nil {
			return pgAuthorityComponents{}, err
		}
		switch rowKind {
		case "database":
			if databaseSeen || encoding == nil || provider == nil || datcollate == nil || datctype == nil {
				return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.database", projector.major, "database authority row is duplicate or sparse")
			}
			localeProvider, normalizedLocale, normalizedRules, normalizedVersion, err := projector.normalizer.databaseProfile(*provider, icuLocale, icuRules, collationVersion)
			if err != nil {
				return pgAuthorityComponents{}, err
			}
			result.DatabaseName, result.DatabaseOwner = databaseName, owner
			result.DatabaseEncoding, result.LocaleProvider = *encoding, localeProvider
			result.Datcollate, result.Datctype = *datcollate, *datctype
			result.ICULocale, result.ICURules, result.CollationVersion = normalizedLocale, normalizedRules, normalizedVersion
			acl = newACLAccumulator(projector.major, aclIsNull, "catalog_explicit")
			databaseSeen = true
		case "acl":
			if !databaseSeen {
				return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.database_acl", projector.major, "database ACL row is sparse or out of order")
			}
			if grantor == nil || grantee == nil {
				return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionUnknownObject, "authority.database_acl.principal", projector.major, "database ACL references an unknown principal")
			}
			if privilege == nil || isGrantable == nil {
				return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.database_acl", projector.major, "database ACL row is sparse")
			}
			if err := acl.add("authority.database_acl", *grantor, *grantee, *privilege, *isGrantable, databasePrivileges); err != nil {
				return pgAuthorityComponents{}, err
			}
		case "effective":
			if !databaseSeen || principal == nil || effectiveCreate == nil || effectiveTemporary == nil {
				return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.effective", projector.major, "effective privilege row is sparse or out of order")
			}
			if _, ok := allowedPrincipals[*principal]; !ok {
				return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionUnknownObject, "authority.effective", projector.major, "effective privilege principal is outside the verified closure")
			}
			if _, duplicate := result.EffectiveCreate[*principal]; duplicate {
				return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionUnknownObject, "authority.effective", projector.major, "effective privilege principal is duplicate")
			}
			result.EffectiveCreate[*principal] = *effectiveCreate
			result.EffectiveTemporary[*principal] = *effectiveTemporary
		default:
			return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.database.row_kind", projector.major, "database authority row kind is unknown")
		}
	}
	if err := rows.Err(); err != nil {
		return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.database.iteration", projector.major, "database authority iteration failed")
	}
	if !databaseSeen || len(result.EffectiveCreate) != len(principals) {
		return pgAuthorityComponents{}, pgProjectionFailure(CodeProjectionUnknownObject, "authority.database", projector.major, "database or effective privilege closure is incomplete")
	}
	result.DatabaseACL = acl.projection()
	return result, nil
}

func (projector *PGProjector) readRoleSettings(ctx context.Context, queryer catalogQueryer, database string, roles []string) ([]DatabaseRoleSettingProjection, error) {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryRoleSettings, database, roles)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	settings := make([]DatabaseRoleSettingProjection, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var setting DatabaseRoleSettingProjection
		if err := rows.Scan(&setting.Database, &setting.Role, &setting.Settings); err != nil {
			return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.role_settings.scan", projector.major, "role setting scan failed")
		}
		values := append([]string{setting.Database, setting.Role}, setting.Settings...)
		if err := budget.add("authority.role_settings", values...); err != nil {
			return nil, err
		}
		if uint64(len(settings)) >= projectionMaxRoleSettings {
			return nil, pgProjectionFailure(CodeProjectionLimitExceeded, "authority.role_settings", projector.major, "role setting row limit exceeded")
		}
		if err := validateRoleSettings(setting.Settings); err != nil {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.role_settings", projector.major, "database role setting is outside the safe allowlist")
		}
		sort.Strings(setting.Settings)
		key := setting.Database + "\x00" + setting.Role
		if _, duplicate := seen[key]; duplicate {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.role_settings", projector.major, "database role setting scope is duplicate")
		}
		seen[key] = struct{}{}
		settings = append(settings, setting)
	}
	if err := rows.Err(); err != nil {
		return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.role_settings.iteration", projector.major, "role setting iteration failed")
	}
	sort.Slice(settings, func(left, right int) bool {
		return settings[left].Database+"\x00"+settings[left].Role < settings[right].Database+"\x00"+settings[right].Role
	})
	return settings, nil
}

func normalizeLibcDatabaseProfile(major uint16, provider string, icuLocale, icuRules, collationVersion *string) (string, *string, *string, *string, error) {
	provider = strings.TrimSpace(provider)
	if provider != "c" {
		return "", nil, nil, nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.database.locale_provider", major, "database locale provider is outside the libc profile")
	}
	if icuLocale != nil || icuRules != nil || collationVersion != nil {
		return "", nil, nil, nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.database.locale", major, "libc database carries ICU or collation-version state")
	}
	return "libc", nil, nil, nil, nil
}

type membershipGraphEdge struct {
	to      string
	inherit bool
	set     bool
}

func (projector *PGProjector) projectReachability(expected []ReachabilityProjection, roles map[string]RoleProjection, memberships []DirectMembershipProjection) ([]ReachabilityProjection, error) {
	edgeCount, err := checkedUint32(uint64(len(memberships)), "authority.reachability.edge_count")
	if err != nil {
		return nil, err
	}
	graph := make(map[string][]membershipGraphEdge, len(roles))
	endpointSeen := make(map[string]struct{}, len(memberships))
	for _, membership := range memberships {
		key := membership.Member + "\x00" + membership.Role
		if _, duplicate := endpointSeen[key]; duplicate {
			return nil, pgProjectionFailure(CodeProjectionNonCanonicalWitness, "authority.reachability.edges", projector.major, "multiple direct grants produce the same logical witness edge")
		}
		endpointSeen[key] = struct{}{}
		graph[membership.Member] = append(graph[membership.Member], membershipGraphEdge{to: membership.Role, inherit: membership.InheritOption, set: membership.SetOption})
	}
	for member := range graph {
		sort.Slice(graph[member], func(left, right int) bool { return graph[member][left].to < graph[member][right].to })
	}
	if err := projector.rejectMembershipCycles(graph, roles); err != nil {
		return nil, err
	}
	result := make([]ReachabilityProjection, 0, len(expected))
	previousKey := ""
	for _, requested := range expected {
		key := requested.Role + "\x00" + requested.Member
		if requested.Role == "" || requested.Member == "" || previousKey != "" && previousKey >= key {
			return nil, pgProjectionFailure(CodeProjectionNonCanonicalWitness, "authority.reachability.scope", projector.major, "verified reachability scope is duplicate or unsorted")
		}
		previousKey = key
		if _, ok := roles[requested.Role]; !ok {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.reachability.role", projector.major, "reachability target is outside the verified role closure")
		}
		if _, ok := roles[requested.Member]; !ok {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "authority.reachability.member", projector.major, "reachability member is outside the verified role closure")
		}
		projection := ReachabilityProjection{Role: requested.Role, Member: requested.Member, Privileges: make([]ReachabilityPrivilegeProjection, 0, 3)}
		for _, kind := range []string{"member", "usage", "set"} {
			privilege, err := projector.shortestMembershipWitness(graph, roles, requested.Member, requested.Role, kind, edgeCount)
			if err != nil {
				return nil, err
			}
			projection.Privileges = append(projection.Privileges, privilege)
		}
		result = append(result, projection)
	}
	return result, nil
}

func (projector *PGProjector) crossCheckReachability(ctx context.Context, queryer catalogQueryer, projected []ReachabilityProjection) error {
	members := make([]string, len(projected))
	roles := make([]string, len(projected))
	expected := make(map[string][3]bool, len(projected))
	for index, reachability := range projected {
		if len(reachability.Privileges) != 3 {
			return pgProjectionFailure(CodeAuthorityDrift, "authority.reachability.cross_check", projector.major, "projected reachability shape is invalid")
		}
		members[index], roles[index] = reachability.Member, reachability.Role
		expected[reachability.Role+"\x00"+reachability.Member] = [3]bool{
			reachability.Privileges[0].Reachable,
			reachability.Privileges[1].Reachable,
			reachability.Privileges[2].Reachable,
		}
	}
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryAuthorityReachability, members, roles)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	seen := make(map[string]struct{}, len(expected))
	for rows.Next() {
		var member, role string
		var memberReachable, usageReachable, setReachable bool
		if err := rows.Scan(&member, &role, &memberReachable, &usageReachable, &setReachable); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.reachability.cross_check.scan", projector.major, "pg_has_role cross-check scan failed")
		}
		if err := budget.add("authority.reachability.cross_check", member, role); err != nil {
			return err
		}
		key := role + "\x00" + member
		want, ok := expected[key]
		if !ok {
			return pgProjectionFailure(CodeProjectionUnknownObject, "authority.reachability.cross_check", projector.major, "pg_has_role returned an unbound principal pair")
		}
		if _, duplicate := seen[key]; duplicate {
			return pgProjectionFailure(CodeProjectionUnknownObject, "authority.reachability.cross_check", projector.major, "pg_has_role returned a duplicate principal pair")
		}
		seen[key] = struct{}{}
		if want != [3]bool{memberReachable, usageReachable, setReachable} {
			return pgProjectionFailure(CodeAuthorityDrift, "authority.reachability.cross_check", projector.major, "projected membership semantics differ from pg_has_role")
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "authority.reachability.cross_check.iteration", projector.major, "pg_has_role cross-check iteration failed")
	}
	if len(seen) != len(expected) {
		return pgProjectionFailure(CodeAuthorityDrift, "authority.reachability.cross_check", projector.major, "pg_has_role cross-check result is incomplete")
	}
	return nil
}

func (projector *PGProjector) rejectMembershipCycles(graph map[string][]membershipGraphEdge, roles map[string]RoleProjection) error {
	const (
		unseen = iota
		visiting
		visited
	)
	state := make(map[string]int, len(roles))
	var visit func(string, uint64) error
	visit = func(node string, depth uint64) error {
		if depth > projectionMaxMembershipDepth {
			return pgProjectionFailure(CodeProjectionLimitExceeded, "authority.reachability.depth", projector.major, "membership depth limit exceeded")
		}
		switch state[node] {
		case visiting:
			return pgProjectionFailure(CodeProjectionNonCanonicalWitness, "authority.reachability.cycle", projector.major, "membership graph contains a cycle")
		case visited:
			return nil
		}
		state[node] = visiting
		for _, edge := range graph[node] {
			if err := visit(edge.to, depth+1); err != nil {
				return err
			}
		}
		state[node] = visited
		return nil
	}
	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name, 0); err != nil {
			return err
		}
	}
	return nil
}

func (projector *PGProjector) shortestMembershipWitness(graph map[string][]membershipGraphEdge, roles map[string]RoleProjection, member, target, kind string, edgeCount uint32) (ReachabilityPrivilegeProjection, error) {
	paths := [][]string{{member}}
	var candidates uint64
	for depth := uint64(0); depth <= projectionMaxMembershipDepth; depth++ {
		matches := make([][]string, 0)
		for _, path := range paths {
			if path[len(path)-1] == target {
				matches = append(matches, path)
			}
		}
		if len(matches) != 0 {
			best := matches[0]
			bestKey, err := canonicalContractKey(best)
			if err != nil {
				return ReachabilityPrivilegeProjection{}, pgProjectionFailure(CodeProjectionNonCanonicalWitness, "authority.reachability.witness", projector.major, "canonical witness encoding failed")
			}
			seenKeys := map[string]struct{}{bestKey: {}}
			for _, candidate := range matches[1:] {
				key, err := canonicalContractKey(candidate)
				if err != nil {
					return ReachabilityPrivilegeProjection{}, pgProjectionFailure(CodeProjectionNonCanonicalWitness, "authority.reachability.witness", projector.major, "canonical witness encoding failed")
				}
				if _, duplicate := seenKeys[key]; duplicate {
					return ReachabilityPrivilegeProjection{}, pgProjectionFailure(CodeProjectionNonCanonicalWitness, "authority.reachability.witness", projector.major, "canonical witness candidate is duplicate")
				}
				seenKeys[key] = struct{}{}
				if key < bestKey {
					best, bestKey = candidate, key
				}
			}
			minDepth, err := checkedUint32(depth, "authority.reachability.min_depth")
			if err != nil {
				return ReachabilityPrivilegeProjection{}, err
			}
			witness := append([]string(nil), best...)
			return ReachabilityPrivilegeProjection{PrivilegeKind: kind, Reachable: true, MinDepth: &minDepth, CanonicalWitness: &witness, EdgeCount: edgeCount}, nil
		}
		if depth == projectionMaxMembershipDepth {
			break
		}
		next := make([][]string, 0)
		for _, path := range paths {
			node := path[len(path)-1]
			for _, edge := range graph[node] {
				if !projector.membershipEdgeAllowed(kind, roles[node], edge) {
					continue
				}
				candidates++
				if candidates > projectionMaxCanonicalWitnessCandidates {
					return ReachabilityPrivilegeProjection{}, pgProjectionFailure(CodeProjectionLimitExceeded, "authority.reachability.candidates", projector.major, "canonical witness candidate limit exceeded")
				}
				candidate := append(append([]string(nil), path...), edge.to)
				next = append(next, candidate)
			}
		}
		if len(next) == 0 {
			break
		}
		paths = next
	}
	return ReachabilityPrivilegeProjection{PrivilegeKind: kind, Reachable: false, MinDepth: nil, CanonicalWitness: nil, EdgeCount: edgeCount}, nil
}

func (projector *PGProjector) membershipEdgeAllowed(kind string, current RoleProjection, edge membershipGraphEdge) bool {
	switch kind {
	case "member":
		return true
	case "usage":
		return projector.normalizer.usageEdgeAllowed(current, edge)
	case "set":
		return edge.set
	default:
		return false
	}
}
