package migration

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	signedIntegerDecimalPattern = regexp.MustCompile(`^(0|-?[1-9][0-9]{0,18})$`)
	exactNumericPattern         = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]*[1-9])?$`)
	ryuDecimalPattern           = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]*[1-9])?(e-?(0|[1-9][0-9]*))?$`)
	deploymentIDPattern         = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	canonicalRFC3339UTCPattern  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$`)
)

var privilegeOrder = map[string]int{
	"CONNECT": 0, "CREATE": 1, "DELETE": 2, "EXECUTE": 3,
	"INSERT": 4, "REFERENCES": 5, "SELECT": 6, "TEMPORARY": 7,
	"TRIGGER": 8, "TRUNCATE": 9, "UPDATE": 10, "USAGE": 11,
}

func DecodeAuthorityBinding(data []byte) (*AuthorityBinding, error) {
	var binding AuthorityBinding
	if _, err := DecodeStrict(data, &binding); err != nil {
		return nil, err
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	return &binding, nil
}

func DecodeAuthorityProfile(data []byte) (*AuthorityProfile, error) {
	return DecodeAuthorityContract(data)
}

func (contract AuthorityContract) Validate() error {
	bootstrap := contract.PublicationStatus == "UNPUBLISHED_BOOTSTRAP_MUTABLE" && contract.RuntimeIntrospectionStatus == "NOT_IMPLEMENTED"
	executable := contract.PublicationStatus == "PUBLISHED_IMMUTABLE" && contract.RuntimeIntrospectionStatus == "IMPLEMENTED"
	if !bootstrap && !executable {
		return invalidProjection("authority-profile", "publication and introspection status combination is invalid")
	}
	if contract.Database.Encoding != "UTF8" || contract.Database.LocaleProvider != "libc" || contract.Database.Datcollate != "C" || contract.Database.Datctype != "C" || contract.Database.ICULocale != nil || contract.Database.ICURules != nil || contract.Database.CollationVersion != nil {
		return invalidProjection("authority-profile", "database locale profile differs from ADR-0010 v1")
	}
	expectedRoles := []string{MigrationOwnerRole, RuntimeRole, BootstrapAdminRole}
	if !equalStrings(contract.GroupRoles, expectedRoles) {
		return invalidProjection("authority-profile", "group role closure differs from ADR-0010 v1")
	}
	expectedProjectionFields := []string{"phase", "session_user", "current_user", "database_name", "database_owner", "database_encoding", "locale_provider", "datcollate", "datctype", "icu_locale", "icu_rules", "collation_version", "database_acl", "roles", "direct_memberships", "membership_reachability", "database_role_settings", "effective_create", "effective_temporary"}
	if !equalStrings(contract.RequiredProjectionFields, expectedProjectionFields) {
		return invalidProjection("authority-profile", "required projection field closure differs from ADR-0010 v1")
	}
	if !bootstrap || contract.RequiredBindingFields != nil {
		expectedBindingFields := []string{"authority_profile_digest", "deployment_id", "issued_at", "expires_at", "security_epoch", "expected_projections"}
		if !equalStrings(contract.RequiredBindingFields, expectedBindingFields) {
			return invalidProjection("authority-profile", "required binding field closure differs from ADR-0010 v1")
		}
	}
	return nil
}

func (model bootstrapCatalogProjectionModel) Validate() error {
	if model.ProjectionSlice != "A2.1a_namespace_only" ||
		!equalStrings(model.CatalogProjectionFields, []string{"schema_head", "body"}) ||
		!equalStrings(model.BodyFields, []string{"schema", "default_acl", "relations", "functions", "dependencies", "object_count", "declared_objects", "denied_objects"}) ||
		!equalStrings(model.SchemaFields, []string{"name", "owner", "explicit_acl", "effective_acl", "comment", "security_labels"}) ||
		!equalStrings(model.DefaultACLFields, []string{"owner", "schema", "object_kind", "acl"}) ||
		!equalStrings(model.ACLSetFields, []string{"catalog_value", "entries"}) ||
		!equalStrings(model.ACLEntryFields, []string{"grantor", "grantee", "privileges", "grantable", "origin"}) ||
		!equalStrings(model.DeferredToA21b, []string{"relation_projection", "function_projection", "dependency_projection", "expression_projection"}) {
		return invalidProjection("catalog-contract", "bootstrap projection model differs from the frozen A2.1a namespace slice")
	}
	return nil
}

func (binding AuthorityBinding) Validate() error {
	if binding.FormatVersion != AuthorityBindingFormat || !deploymentIDPattern.MatchString(binding.DeploymentID) || binding.SecurityEpoch == 0 {
		return invalidProjection("authority-binding", "invalid binding identity or security epoch")
	}
	if err := requireDigest("authority_binding.authority_profile_digest", binding.AuthorityProfileDigest); err != nil {
		return err
	}
	issued, err := parseCanonicalUTCTime(binding.IssuedAt)
	if err != nil {
		return err
	}
	expires, err := parseCanonicalUTCTime(binding.ExpiresAt)
	if err != nil || !expires.After(issued) {
		return invalidProjection("authority-binding", "binding expiry must be canonical UTC and after issue time")
	}
	phases := []struct {
		want       AuthorityPhase
		projection AuthorityProjection
	}{
		{AuthorityPhaseConnectedSession, binding.ExpectedProjections.ConnectedSession},
		{AuthorityPhaseMigrationRole, binding.ExpectedProjections.MigrationRole},
		{AuthorityPhaseMigrationTransaction, binding.ExpectedProjections.MigrationTransaction},
	}
	for _, phase := range phases {
		if phase.projection.Phase != phase.want {
			return invalidProjection("authority-binding", "expected projection phase does not match its binding branch")
		}
		if err := phase.projection.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func parseCanonicalUTCTime(value string) (time.Time, error) {
	if !canonicalRFC3339UTCPattern.MatchString(value) {
		return time.Time{}, invalidProjection("authority-binding", "timestamp must use UTC Z form")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, invalidProjection("authority-binding", "timestamp is not canonical RFC3339 UTC")
	}
	return parsed, nil
}

func (projection AuthorityProjection) Validate() error {
	switch projection.Phase {
	case AuthorityPhaseConnectedSession, AuthorityPhaseMigrationRole, AuthorityPhaseMigrationTransaction:
	default:
		return invalidProjection("authority-projection", "unknown authority phase")
	}
	for field, value := range map[string]string{
		"session_user": projection.SessionUser, "current_user": projection.CurrentUser,
		"database_name": projection.DatabaseName, "database_owner": projection.DatabaseOwner,
		"database_encoding": projection.DatabaseEncoding, "locale_provider": projection.LocaleProvider,
		"datcollate": projection.Datcollate, "datctype": projection.Datctype,
	} {
		if value == "" {
			return invalidProjection("authority-projection", field+" is empty")
		}
	}
	if projection.ICULocale != nil || projection.ICURules != nil || projection.CollationVersion != nil {
		return invalidProjection("authority-projection", "ICU locale, rules, and collation version must be null in the v1 libc/C profile")
	}
	if projection.Roles == nil || projection.DirectMemberships == nil || projection.MembershipReachability == nil || projection.DatabaseRoleSettings == nil || projection.EffectiveCreate == nil || projection.EffectiveTemporary == nil {
		return invalidProjection("authority-projection", "arrays and effective privilege maps must be explicit")
	}
	if err := projection.DatabaseACL.Validate(); err != nil {
		return err
	}
	if err := validateACLOrigins(projection.DatabaseACL.Entries, "catalog_explicit"); err != nil {
		return err
	}
	if err := validateACLPrivileges(projection.DatabaseACL.Entries, "CONNECT", "CREATE", "TEMPORARY"); err != nil {
		return err
	}
	roleKeys := make([]string, len(projection.Roles))
	rolesByName := make(map[string]RoleProjection, len(projection.Roles))
	for index, role := range projection.Roles {
		if role.Name == "" || role.Config == nil {
			return invalidProjection("authority-projection", "role identity and config are required")
		}
		connectionLimit, err := ValidateSignedIntegerDecimal(role.ConnectionLimitInt32Decimal, 32)
		if err != nil {
			return err
		}
		if connectionLimit < -1 {
			return invalidProjection("authority-projection", "role connection limit is below PostgreSQL's canonical -1 sentinel")
		}
		if role.ValidUntil != nil && *role.ValidUntil == "" {
			return invalidProjection("authority-projection", "non-null valid_until must be non-empty")
		}
		if err := validateRoleSettings(role.Config); err != nil {
			return err
		}
		if !strictlySorted(role.Config) {
			return invalidProjection("authority-projection", "role config is duplicate or unsorted")
		}
		if len(role.Config) != 0 {
			return invalidProjection("authority-projection", "initial A2.1a role config must be empty")
		}
		roleKeys[index] = role.Name
		rolesByName[role.Name] = role
	}
	if !strictlySorted(roleKeys) {
		return invalidProjection("authority-projection", "roles are not strictly UTF-8 sorted")
	}
	sessionRole, sessionPresent := rolesByName[projection.SessionUser]
	_, currentPresent := rolesByName[projection.CurrentUser]
	databaseOwnerRole, databaseOwnerPresent := rolesByName[projection.DatabaseOwner]
	if !sessionPresent || !currentPresent || !databaseOwnerPresent {
		return invalidProjection("authority-projection", "session, current, and database-owner principals must be in the explicit role closure")
	}
	switch projection.Phase {
	case AuthorityPhaseConnectedSession:
		if projection.CurrentUser != projection.SessionUser {
			return invalidProjection("authority-projection", "connected-session current_user must equal session_user")
		}
	case AuthorityPhaseMigrationRole, AuthorityPhaseMigrationTransaction:
		if projection.CurrentUser != MigrationOwnerRole {
			return invalidProjection("authority-projection", "migration role and transaction phases require the migration owner current_user")
		}
	}
	groupRoles := []string{BootstrapAdminRole, MigrationOwnerRole, RuntimeRole}
	for _, groupName := range groupRoles {
		group, ok := rolesByName[groupName]
		if !ok {
			return invalidProjection("authority-projection", "required Cloud Agents group role is absent")
		}
		if group.Login || group.Inherit || hasUnsafeAuthorityAttributes(group) {
			return invalidProjection("authority-projection", "Cloud Agents group role attributes differ from the closed NOLOGIN/NOINHERIT profile")
		}
	}
	if !sessionRole.Login || sessionRole.Inherit || hasUnsafeAuthorityAttributes(sessionRole) {
		return invalidProjection("authority-projection", "session workload must be a safe NOINHERIT LOGIN")
	}
	if projection.SessionUser == projection.DatabaseOwner || projection.SessionUser == BootstrapAdminRole || projection.SessionUser == MigrationOwnerRole || projection.SessionUser == RuntimeRole {
		return invalidProjection("authority-projection", "session workload cannot be a group role or database owner")
	}
	if projection.DatabaseOwner == BootstrapAdminRole || projection.DatabaseOwner == MigrationOwnerRole || projection.DatabaseOwner == RuntimeRole || hasUnsafeAuthorityAttributes(databaseOwnerRole) {
		return invalidProjection("authority-projection", "database owner differs from the safe independent authority profile")
	}
	directKeys := make([]string, len(projection.DirectMemberships))
	directGraph := make(map[string][]authorityValidationEdge, len(projection.Roles))
	directEndpoints := make(map[string]struct{}, len(projection.DirectMemberships))
	workloadMembership := make(map[string]string, len(projection.DirectMemberships))
	membershipGrantors := make(map[string]struct{}, len(projection.DirectMemberships))
	for index, membership := range projection.DirectMemberships {
		if membership.Role == "" || membership.Member == "" || membership.Grantor == "" {
			return invalidProjection("authority-projection", "direct membership loses role, member, or grantor provenance")
		}
		if _, ok := rolesByName[membership.Role]; !ok {
			return invalidProjection("authority-projection", "direct membership role is outside the explicit role closure")
		}
		if _, ok := rolesByName[membership.Member]; !ok {
			return invalidProjection("authority-projection", "direct membership member is outside the explicit role closure")
		}
		if _, ok := rolesByName[membership.Grantor]; !ok {
			return invalidProjection("authority-projection", "direct membership grantor is outside the explicit role closure")
		}
		grantor := rolesByName[membership.Grantor]
		if membership.Grantor == projection.SessionUser || !grantor.Superuser {
			return invalidProjection("authority-projection", "direct membership grantor must be a distinct trusted superuser")
		}
		membershipGrantors[membership.Grantor] = struct{}{}
		if membership.Role == projection.DatabaseOwner || membership.Member == projection.DatabaseOwner {
			return invalidProjection("authority-projection", "database owner cannot participate in delegated membership")
		}
		memberRole := rolesByName[membership.Member]
		if !memberRole.Login || hasUnsafeAuthorityAttributes(memberRole) || membership.AdminOption {
			return invalidProjection("authority-projection", "direct group member must be a safe non-admin workload LOGIN")
		}
		switch membership.Role {
		case MigrationOwnerRole:
			if memberRole.Inherit || !membership.SetOption {
				return invalidProjection("authority-projection", "migration workload must be NOINHERIT with a SET-enabled membership")
			}
		case RuntimeRole, BootstrapAdminRole:
			if !memberRole.Inherit {
				return invalidProjection("authority-projection", "runtime and bootstrap workloads must be INHERIT LOGINs")
			}
		default:
			return invalidProjection("authority-projection", "direct membership targets a role outside the three group authorities")
		}
		if _, duplicate := workloadMembership[membership.Member]; duplicate {
			return invalidProjection("authority-projection", "workload LOGIN belongs to more than one direct role")
		}
		workloadMembership[membership.Member] = membership.Role
		endpointKey := membership.Member + "\x00" + membership.Role
		if _, duplicate := directEndpoints[endpointKey]; duplicate {
			return invalidProjection("authority-projection", "direct memberships contain duplicate logical witness edges")
		}
		directEndpoints[endpointKey] = struct{}{}
		directGraph[membership.Member] = append(directGraph[membership.Member], authorityValidationEdge{
			to: membership.Role, inherit: membership.InheritOption, set: membership.SetOption,
		})
		directKeys[index] = membership.Role + "\x00" + membership.Member + "\x00" + membership.Grantor
	}
	if !strictlySorted(directKeys) {
		return invalidProjection("authority-projection", "direct memberships are not strictly sorted")
	}
	if workloadMembership[projection.SessionUser] != MigrationOwnerRole {
		return invalidProjection("authority-projection", "session workload must be a direct member of exactly the migration owner role")
	}
	for grantor := range membershipGrantors {
		if _, workload := workloadMembership[grantor]; workload {
			return invalidProjection("authority-projection", "trusted membership grantor cannot also be a workload LOGIN")
		}
	}
	expectedRoleClosure := map[string]struct{}{
		BootstrapAdminRole: {}, MigrationOwnerRole: {}, RuntimeRole: {}, projection.DatabaseOwner: {},
	}
	for workload := range workloadMembership {
		expectedRoleClosure[workload] = struct{}{}
	}
	for grantor := range membershipGrantors {
		expectedRoleClosure[grantor] = struct{}{}
	}
	if len(expectedRoleClosure) != len(rolesByName) {
		return invalidProjection("authority-projection", "role projection contains principals outside the mechanical authority closure")
	}
	for role := range rolesByName {
		if _, ok := expectedRoleClosure[role]; !ok {
			return invalidProjection("authority-projection", "role projection contains a principal outside the mechanical authority closure")
		}
	}
	for member := range directGraph {
		sort.Slice(directGraph[member], func(left, right int) bool {
			return directGraph[member][left].to < directGraph[member][right].to
		})
	}
	if err := validateAuthorityMembershipGraph(directGraph, rolesByName); err != nil {
		return err
	}
	if uint64(len(projection.DirectMemberships)) > uint64(math.MaxUint32) {
		return invalidProjection("authority-projection", "direct membership closure exceeds uint32 edge_count")
	}
	completeEdgeCount := uint32(len(projection.DirectMemberships))
	if len(projection.MembershipReachability) != len(projection.DirectMemberships) {
		return invalidProjection("authority-projection", "reachability endpoint closure differs from direct workload memberships")
	}
	reachabilityKeys := make([]string, len(projection.MembershipReachability))
	for index, reachability := range projection.MembershipReachability {
		if err := reachability.Validate(); err != nil {
			return err
		}
		if _, ok := rolesByName[reachability.Role]; !ok {
			return invalidProjection("authority-projection", "reachability role is outside the explicit role closure")
		}
		if _, ok := rolesByName[reachability.Member]; !ok {
			return invalidProjection("authority-projection", "reachability member is outside the explicit role closure")
		}
		if workloadMembership[reachability.Member] != reachability.Role {
			return invalidProjection("authority-projection", "reachability endpoint differs from the workload direct membership")
		}
		for privilegeIndex := range reachability.Privileges {
			privilege := reachability.Privileges[privilegeIndex]
			if privilege.EdgeCount != completeEdgeCount {
				return invalidProjection("authority-projection", "reachability edge_count differs from the complete direct membership closure")
			}
			if err := validateAuthorityReachabilityPrivilege(directGraph, reachability, privilege); err != nil {
				return err
			}
		}
		reachabilityKeys[index] = reachability.Role + "\x00" + reachability.Member
	}
	if !strictlySorted(reachabilityKeys) {
		return invalidProjection("authority-projection", "membership reachability is not strictly sorted")
	}
	settingKeys := make([]string, len(projection.DatabaseRoleSettings))
	for index, setting := range projection.DatabaseRoleSettings {
		if setting.Database == "" || setting.Role == "" || setting.Settings == nil {
			return invalidProjection("authority-projection", "database role setting scope is incomplete")
		}
		if err := validateRoleSettings(setting.Settings); err != nil {
			return err
		}
		if !strictlySorted(setting.Settings) {
			return invalidProjection("authority-projection", "database role settings are duplicate or unsorted")
		}
		settingKeys[index] = setting.Database + "\x00" + setting.Role
	}
	if !strictlySorted(settingKeys) {
		return invalidProjection("authority-projection", "database role settings are not strictly sorted")
	}
	if len(projection.DatabaseRoleSettings) != 0 {
		return invalidProjection("authority-projection", "initial A2.1a database role settings must be empty")
	}
	if len(projection.EffectiveCreate) == 0 || len(projection.EffectiveTemporary) == 0 {
		return invalidProjection("authority-projection", "effective privilege maps must be non-empty")
	}
	for surface, privileges := range map[string]map[string]bool{"effective_create": projection.EffectiveCreate, "effective_temporary": projection.EffectiveTemporary} {
		for identity := range privileges {
			if identity == "" {
				return invalidProjection("authority-projection", surface+" contains an empty principal identity")
			}
		}
	}
	return nil
}

func hasUnsafeAuthorityAttributes(role RoleProjection) bool {
	return role.Superuser || role.BypassRLS || role.CreateRole || role.CreateDB || role.Replication
}

func (projection ReachabilityProjection) Validate() error {
	if projection.Role == "" || projection.Member == "" || len(projection.Privileges) != 3 {
		return invalidProjection("authority-projection", "reachability must contain role, member, and three privileges")
	}
	expected := []string{"member", "usage", "set"}
	for index, privilege := range projection.Privileges {
		if privilege.PrivilegeKind != expected[index] {
			return invalidProjection("authority-projection", "reachability privilege order or kind is invalid")
		}
		if privilege.Reachable {
			if privilege.MinDepth == nil || privilege.CanonicalWitness == nil || len(*privilege.CanonicalWitness) == 0 {
				return invalidProjection("authority-projection", "reachable privilege lacks bounded witness metadata")
			}
			if uint64(len(*privilege.CanonicalWitness)-1) != uint64(*privilege.MinDepth) {
				return invalidProjection("authority-projection", "reachable privilege depth differs from witness path length")
			}
			for _, principal := range *privilege.CanonicalWitness {
				if principal == "" {
					return invalidProjection("authority-projection", "canonical witness contains an empty principal identity")
				}
			}
		} else if privilege.MinDepth != nil || privilege.CanonicalWitness != nil {
			return invalidProjection("authority-projection", "unreachable privilege carries non-null witness metadata")
		}
	}
	return nil
}

type authorityValidationEdge struct {
	to      string
	inherit bool
	set     bool
}

func validateAuthorityMembershipGraph(graph map[string][]authorityValidationEdge, roles map[string]RoleProjection) error {
	const (
		authorityUnseen = iota
		authorityVisiting
		authorityVisited
	)
	states := make(map[string]int, len(roles))
	var visit func(string, uint64) error
	visit = func(role string, depth uint64) error {
		if depth > projectionMaxMembershipDepth {
			return invalidProjection("authority-projection", "direct membership graph exceeds the closed depth limit")
		}
		switch states[role] {
		case authorityVisiting:
			return invalidProjection("authority-projection", "direct membership graph contains a cycle")
		case authorityVisited:
			return nil
		}
		states[role] = authorityVisiting
		for _, edge := range graph[role] {
			if err := visit(edge.to, depth+1); err != nil {
				return err
			}
		}
		states[role] = authorityVisited
		return nil
	}
	roleNames := make([]string, 0, len(roles))
	for role := range roles {
		roleNames = append(roleNames, role)
	}
	sort.Strings(roleNames)
	for _, role := range roleNames {
		if err := visit(role, 0); err != nil {
			return err
		}
	}
	return nil
}

func validateAuthorityReachabilityPrivilege(graph map[string][]authorityValidationEdge, reachability ReachabilityProjection, privilege ReachabilityPrivilegeProjection) error {
	if privilege.Reachable {
		if err := validateAuthorityWitnessPath(graph, reachability.Member, reachability.Role, privilege.PrivilegeKind, *privilege.CanonicalWitness); err != nil {
			return err
		}
	}
	if privilege.PrivilegeKind == "usage" {
		// PostgreSQL 15 derives USAGE from role-level INHERIT while 16/17 derive it
		// from per-edge inherit_option. AuthorityProjection deliberately carries no
		// major, so the typed seam can only validate its path shape and adjacency;
		// the exact major adapter computes and verifies USAGE reachability.
		return nil
	}
	reachable, minDepth, witness, err := canonicalAuthorityMembershipWitness(
		graph, reachability.Member, reachability.Role, privilege.PrivilegeKind,
	)
	if err != nil {
		return err
	}
	if privilege.Reachable != reachable {
		return invalidProjection("authority-projection", "reported reachability differs from the direct membership graph")
	}
	if reachable && (privilege.MinDepth == nil || *privilege.MinDepth != minDepth || privilege.CanonicalWitness == nil || !equalStrings(*privilege.CanonicalWitness, witness)) {
		return invalidProjection("authority-projection", "reachability witness is not the canonical shortest direct membership path")
	}
	return nil
}

func validateAuthorityWitnessPath(graph map[string][]authorityValidationEdge, member, target, kind string, witness []string) error {
	if len(witness) == 0 || witness[0] != member || witness[len(witness)-1] != target {
		return invalidProjection("authority-projection", "reachability witness endpoints differ from member and role")
	}
	for index := 0; index+1 < len(witness); index++ {
		found := false
		for _, edge := range graph[witness[index]] {
			if edge.to == witness[index+1] && authorityMembershipEdgeAllowed(kind, edge) {
				found = true
				break
			}
		}
		if !found {
			return invalidProjection("authority-projection", "reachability witness contains a missing or privilege-ineligible direct edge")
		}
	}
	return nil
}

func canonicalAuthorityMembershipWitness(graph map[string][]authorityValidationEdge, member, target, kind string) (bool, uint32, []string, error) {
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
				return false, 0, nil, invalidProjection("authority-projection", "canonical witness encoding failed")
			}
			seenKeys := map[string]struct{}{bestKey: {}}
			for _, candidate := range matches[1:] {
				key, err := canonicalContractKey(candidate)
				if err != nil {
					return false, 0, nil, invalidProjection("authority-projection", "canonical witness encoding failed")
				}
				if _, duplicate := seenKeys[key]; duplicate {
					return false, 0, nil, invalidProjection("authority-projection", "canonical witness candidate is duplicated")
				}
				seenKeys[key] = struct{}{}
				if key < bestKey {
					best, bestKey = candidate, key
				}
			}
			return true, uint32(depth), append([]string(nil), best...), nil
		}
		if depth == projectionMaxMembershipDepth {
			break
		}
		next := make([][]string, 0)
		for _, path := range paths {
			current := path[len(path)-1]
			for _, edge := range graph[current] {
				if !authorityMembershipEdgeAllowed(kind, edge) {
					continue
				}
				candidates++
				if candidates > projectionMaxCanonicalWitnessCandidates {
					return false, 0, nil, invalidProjection("authority-projection", "canonical witness candidate limit exceeded")
				}
				next = append(next, append(append([]string(nil), path...), edge.to))
			}
		}
		if len(next) == 0 {
			break
		}
		paths = next
	}
	return false, 0, nil, nil
}

func authorityMembershipEdgeAllowed(kind string, edge authorityValidationEdge) bool {
	switch kind {
	case "member":
		return true
	case "usage":
		return true
	case "set":
		return edge.set
	default:
		return false
	}
}

func validateRoleSettings(settings []string) error {
	for _, setting := range settings {
		name, _, ok := strings.Cut(setting, "=")
		if !ok {
			name = setting
		}
		switch name {
		case "client_encoding", "search_path", "statement_timeout", "lock_timeout", "idle_in_transaction_session_timeout":
		default:
			return invalidProjection("authority-projection", "role setting name is outside the safe allowlist")
		}
	}
	return nil
}

func (set ACLSetProjection) Validate() error {
	if set.Entries == nil {
		return invalidProjection("acl-set", "ACL entries must be an explicit array")
	}
	switch set.CatalogValue {
	case "null":
		if len(set.Entries) != 0 {
			return invalidProjection("acl-set", "null catalog ACL must have no entries")
		}
	case "explicit":
	default:
		return invalidProjection("acl-set", "unknown catalog ACL value")
	}
	keys := make([]string, len(set.Entries))
	for index, entry := range set.Entries {
		if entry.Grantor == "" || entry.Grantee == "" || entry.Privileges == nil || entry.Grantable == nil {
			return invalidProjection("acl-set", "ACL provenance and arrays are required")
		}
		switch entry.Origin {
		case "catalog_explicit", "owner_implicit", "public_default", "default_acl_catalog":
		default:
			return invalidProjection("acl-set", "unknown ACL origin")
		}
		if err := validatePrivilegeList(entry.Privileges); err != nil {
			return err
		}
		if err := validatePrivilegeList(entry.Grantable); err != nil {
			return err
		}
		granted := make(map[string]struct{}, len(entry.Privileges))
		for _, privilege := range entry.Privileges {
			granted[privilege] = struct{}{}
		}
		for _, privilege := range entry.Grantable {
			if _, ok := granted[privilege]; !ok {
				return invalidProjection("acl-set", "grantable privilege is absent from privileges")
			}
		}
		keys[index] = entry.Grantor + "\x00" + entry.Grantee
	}
	if !strictlySorted(keys) {
		return invalidProjection("acl-set", "ACL entries are not strictly sorted")
	}
	return nil
}

func validatePrivilegeList(privileges []string) error {
	previousRank := -1
	for index, privilege := range privileges {
		rank, ok := privilegeOrder[privilege]
		if !ok || (index > 0 && previousRank >= rank) {
			return invalidProjection("acl-set", "privilege list contains unknown, duplicate, or unsorted values")
		}
		previousRank = rank
	}
	return nil
}

func validateACLOrigins(entries []ACLProjection, allowed ...string) error {
	allowlist := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		allowlist[origin] = struct{}{}
	}
	for _, entry := range entries {
		if _, ok := allowlist[entry.Origin]; !ok {
			return invalidProjection("acl-set", "ACL origin is invalid for this projection surface")
		}
	}
	return nil
}

func validateACLPrivileges(entries []ACLProjection, allowed ...string) error {
	allowlist := make(map[string]struct{}, len(allowed))
	for _, privilege := range allowed {
		allowlist[privilege] = struct{}{}
	}
	for _, entry := range entries {
		for _, privileges := range [][]string{entry.Privileges, entry.Grantable} {
			for _, privilege := range privileges {
				if _, ok := allowlist[privilege]; !ok {
					return invalidProjection("acl-set", "privilege is invalid for this projection surface")
				}
			}
		}
	}
	return nil
}

func (identity ObjectIdentityProjection) branch() (any, error) {
	branches := []any{identity.Schema, identity.Relation, identity.Column, identity.Index, identity.Policy, identity.Type, identity.Extension, identity.Collation, identity.Opclass, identity.Function, identity.Operator, identity.Cast, identity.Constraint, identity.Trigger, identity.Internal}
	var selected any
	count := 0
	for _, branch := range branches {
		if !isNilInterface(branch) {
			selected = branch
			count++
		}
	}
	if count != 1 {
		return nil, invalidProjection("object-identity", "object identity must contain exactly one branch")
	}
	return selected, nil
}

func isNilInterface(value any) bool {
	switch typed := value.(type) {
	case *SchemaObjectIdentity:
		return typed == nil
	case *RelationObjectIdentity:
		return typed == nil
	case *ColumnObjectIdentity:
		return typed == nil
	case *IndexObjectIdentity:
		return typed == nil
	case *PolicyObjectIdentity:
		return typed == nil
	case *TypeObjectIdentity:
		return typed == nil
	case *ExtensionObjectIdentity:
		return typed == nil
	case *CollationObjectIdentity:
		return typed == nil
	case *OpclassObjectIdentity:
		return typed == nil
	case *SQLObjectIdentity:
		return typed == nil
	case *CastObjectIdentity:
		return typed == nil
	case *ConstraintObjectIdentity:
		return typed == nil
	case *TriggerObjectIdentity:
		return typed == nil
	case *InternalObjectIdentity:
		return typed == nil
	default:
		return true
	}
}

func (identity ObjectIdentityProjection) Validate() error {
	branch, err := identity.branch()
	if err != nil {
		return err
	}
	var kind string
	switch typed := branch.(type) {
	case *SchemaObjectIdentity:
		kind = typed.Kind
		if typed.Name == "" {
			return invalidProjection("object-identity", "schema name is empty")
		}
	case *RelationObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
	case *ColumnObjectIdentity:
		kind = typed.Kind
		err = typed.Relation.Validate()
		if typed.Name == "" {
			err = invalidProjection("object-identity", "column name is empty")
		}
	case *IndexObjectIdentity:
		kind = typed.Kind
		err = firstError(typed.Identity.Validate(), typed.Relation.Validate())
	case *PolicyObjectIdentity:
		kind = typed.Kind
		err = typed.Relation.Validate()
		if typed.Name == "" {
			err = invalidProjection("object-identity", "policy name is empty")
		}
	case *TypeObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
	case *ExtensionObjectIdentity:
		kind = typed.Kind
		if typed.Name == "" {
			err = invalidProjection("object-identity", "extension name is empty")
		}
	case *CollationObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
	case *OpclassObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
		if typed.AccessMethod == "" {
			err = invalidProjection("object-identity", "opclass access method is empty")
		}
	case *SQLObjectIdentity:
		kind = typed.Kind
		err = typed.Identity.Validate()
	case *CastObjectIdentity:
		kind = typed.Kind
		err = firstError(typed.SourceType.Validate(), typed.TargetType.Validate())
	case *ConstraintObjectIdentity:
		kind = typed.Kind
		err = typed.Relation.Validate()
		if typed.Name == "" {
			err = invalidProjection("object-identity", "constraint name is empty")
		}
	case *TriggerObjectIdentity:
		kind = typed.Kind
		err = typed.Relation.Validate()
		if typed.Name == "" {
			err = invalidProjection("object-identity", "trigger name is empty")
		}
		if typed.OwningConstraint != nil {
			if typed.OwningConstraint.Kind != "constraint" {
				err = invalidProjection("object-identity", "trigger owner is not a constraint identity")
			} else if ownerErr := typed.OwningConstraint.Relation.Validate(); ownerErr != nil || typed.OwningConstraint.Name == "" {
				err = invalidProjection("object-identity", "trigger owning constraint identity is incomplete")
			}
		}
	case *InternalObjectIdentity:
		kind = typed.Kind
		if typed.SemanticKind == "" {
			err = invalidProjection("object-identity", "internal semantic kind is empty")
		}
		if typed.OwningObject.Internal != nil {
			err = invalidProjection("object-identity", "internal identity cannot own another internal identity")
		}
		if ownerErr := typed.OwningObject.Validate(); err == nil {
			err = ownerErr
		}
	}
	if err != nil {
		return err
	}
	if kind == "" || kind != identity.Kind() {
		return invalidProjection("object-identity", "object identity discriminator differs from its branch")
	}
	return nil
}

func (identity ObjectIdentityProjection) Kind() string {
	switch {
	case identity.Schema != nil:
		return "schema"
	case identity.Relation != nil:
		return "relation"
	case identity.Column != nil:
		return "column"
	case identity.Index != nil:
		return "index"
	case identity.Policy != nil:
		return "policy"
	case identity.Type != nil:
		return "type"
	case identity.Extension != nil:
		return "extension"
	case identity.Collation != nil:
		return "collation"
	case identity.Opclass != nil:
		return "opclass"
	case identity.Function != nil:
		return "function"
	case identity.Operator != nil:
		return "operator"
	case identity.Cast != nil:
		return "cast"
	case identity.Constraint != nil:
		return "constraint"
	case identity.Trigger != nil:
		return "trigger"
	case identity.Internal != nil:
		return "internal"
	default:
		return ""
	}
}

func (identity TypeIdentity) Validate() error {
	if identity.Schema == "" || identity.Name == "" {
		return invalidProjection("object-identity", "type identity is incomplete")
	}
	return nil
}

func (identity SQLIdentity) Validate() error {
	if identity.Schema == "" || identity.Name == "" || identity.Arguments == nil {
		return invalidProjection("object-identity", "SQL identity is incomplete")
	}
	for _, argument := range identity.Arguments {
		if err := argument.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateObjectIdentityClosure(identities []ObjectIdentityProjection) error {
	if identities == nil {
		return invalidProjection("object-identity", "declared object closure must be an explicit array")
	}
	keys := make([]string, len(identities))
	for index, identity := range identities {
		if err := identity.Validate(); err != nil {
			return err
		}
		key, err := canonicalContractKey(identity)
		if err != nil {
			return err
		}
		keys[index] = key
	}
	if !strictlySorted(keys) {
		return invalidProjection("object-identity", "declared object closure is duplicate or not canonically sorted")
	}
	return nil
}

func (scope ProjectionScope) Validate() error {
	if err := validateObjectIdentityClosure(scope.DeclaredObjects); err != nil {
		return err
	}
	switch scope.ScopeKind {
	case "predecessor":
		if scope.SchemaHead != nil || scope.MigrationID == nil || !migrationIDPattern.MatchString(*scope.MigrationID) || scope.ThroughStatementIndex != nil {
			return invalidProjection("projection-scope", "invalid predecessor scope")
		}
	case "statement_prefix":
		if scope.SchemaHead != nil || scope.MigrationID == nil || !migrationIDPattern.MatchString(*scope.MigrationID) || scope.ThroughStatementIndex == nil {
			return invalidProjection("projection-scope", "invalid statement prefix scope")
		}
	case "final":
		if scope.SchemaHead == nil || !migrationIDPattern.MatchString(*scope.SchemaHead) || scope.MigrationID != nil || scope.ThroughStatementIndex != nil {
			return invalidProjection("projection-scope", "invalid final scope")
		}
	default:
		return invalidProjection("projection-scope", "unknown projection scope kind")
	}
	return nil
}

func (state CatalogStateProjection) Validate() error {
	switch {
	case state.Absent != nil && state.Present == nil:
		if state.Absent.State != "schema_absent" || state.Absent.Schema != "cloud_agents" || state.Absent.Scope.ScopeKind != "predecessor" || len(state.Absent.Scope.DeclaredObjects) != 0 {
			return invalidProjection("catalog-state", "invalid schema_absent branch")
		}
		return state.Absent.Scope.Validate()
	case state.Absent == nil && state.Present != nil:
		if state.Present.State != "schema_present" {
			return invalidProjection("catalog-state", "invalid schema_present branch")
		}
		if err := state.Present.Scope.Validate(); err != nil {
			return err
		}
		if err := state.Present.Body.Validate(); err != nil {
			return err
		}
		if !equalObjectIdentityClosures(state.Present.Scope.DeclaredObjects, state.Present.Body.DeclaredObjects) {
			return invalidProjection("catalog-state", "scope and body declared object closures differ")
		}
		return nil
	default:
		return invalidProjection("catalog-state", "catalog state must contain exactly one branch")
	}
}

func (projection CatalogProjection) Validate() error {
	if !migrationIDPattern.MatchString(projection.SchemaHead) {
		return invalidProjection("catalog-projection", "invalid schema head")
	}
	return projection.Body.Validate()
}

func (body CatalogProjectionBody) Validate() error {
	if body.Schema.Name != "cloud_agents" || body.Schema.Owner == "" || body.Schema.EffectiveACL == nil || body.Schema.SecurityLabels == nil || body.DefaultACL == nil || body.Relations == nil || body.Functions == nil || body.Dependencies == nil || body.DeclaredObjects == nil || body.DeniedObjects == nil {
		return invalidProjection("catalog-projection", "catalog projection body is sparse")
	}
	if err := body.Schema.ExplicitACL.Validate(); err != nil {
		return err
	}
	if err := validateACLOrigins(body.Schema.ExplicitACL.Entries, "catalog_explicit"); err != nil {
		return err
	}
	if err := validateACLPrivileges(body.Schema.ExplicitACL.Entries, "CREATE", "USAGE"); err != nil {
		return err
	}
	if err := (ACLSetProjection{CatalogValue: "explicit", Entries: body.Schema.EffectiveACL}).Validate(); err != nil {
		return err
	}
	if err := validateACLOrigins(body.Schema.EffectiveACL, "catalog_explicit", "owner_implicit", "public_default"); err != nil {
		return err
	}
	if err := validateACLPrivileges(body.Schema.EffectiveACL, "CREATE", "USAGE"); err != nil {
		return err
	}
	if err := validateObjectIdentityClosure(body.DeclaredObjects); err != nil {
		return err
	}
	if body.ObjectCount != uint32(len(body.DeclaredObjects)) {
		return invalidProjection("catalog-projection", "object_count differs from the declared object closure")
	}
	if err := validateRelationProjections(body.Relations); err != nil {
		return err
	}
	if err := validateFunctionProjections(body.Functions); err != nil {
		return err
	}
	if err := validateProjectedDeclaredCoverage(body); err != nil {
		return err
	}
	labelKeys := make([]string, len(body.Schema.SecurityLabels))
	for index, label := range body.Schema.SecurityLabels {
		if label.Provider == "" || label.Label == "" {
			return invalidProjection("catalog-projection", "security label is incomplete")
		}
		labelKeys[index] = label.Provider
	}
	if !strictlySorted(labelKeys) {
		return invalidProjection("catalog-projection", "security labels are duplicate or unsorted")
	}
	defaultACLKeys := make([]string, len(body.DefaultACL))
	for index, acl := range body.DefaultACL {
		if acl.Owner == "" {
			return invalidProjection("catalog-projection", "default ACL scope is incomplete")
		}
		schemaSortKey := "0"
		if acl.Schema != nil {
			if *acl.Schema != "cloud_agents" {
				return invalidProjection("catalog-projection", "default ACL schema is outside the global or cloud_agents scope")
			}
			schemaSortKey = "1" + *acl.Schema
		}
		switch acl.ObjectKind {
		case "table", "sequence", "function", "type", "schema":
		default:
			return invalidProjection("catalog-projection", "unknown default ACL object kind")
		}
		if acl.ObjectKind == "schema" && acl.Schema != nil {
			return invalidProjection("catalog-projection", "schema default ACL kind is only valid in global scope")
		}
		if acl.ACL.CatalogValue != "explicit" {
			return invalidProjection("catalog-projection", "projected default ACL catalog_value must be explicit")
		}
		if err := acl.ACL.Validate(); err != nil {
			return err
		}
		if err := validateACLOrigins(acl.ACL.Entries, "default_acl_catalog"); err != nil {
			return err
		}
		var allowed []string
		switch acl.ObjectKind {
		case "table":
			allowed = []string{"DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"}
		case "sequence":
			allowed = []string{"SELECT", "UPDATE", "USAGE"}
		case "function":
			allowed = []string{"EXECUTE"}
		case "type":
			allowed = []string{"USAGE"}
		case "schema":
			allowed = []string{"CREATE", "USAGE"}
		}
		if err := validateACLPrivileges(acl.ACL.Entries, allowed...); err != nil {
			return err
		}
		defaultACLKeys[index] = acl.Owner + "\x00" + schemaSortKey + "\x00" + acl.ObjectKind
	}
	if !strictlySorted(defaultACLKeys) {
		return invalidProjection("catalog-projection", "default ACL projections are duplicate or unsorted")
	}
	dependencyKeys := make([]string, len(body.Dependencies))
	for index, dependency := range body.Dependencies {
		if dependency.DependencyKind == "" {
			return invalidProjection("catalog-projection", "dependency kind is empty")
		}
		if err := dependency.Depender.Validate(); err != nil {
			return err
		}
		if err := dependency.DependedOn.Validate(); err != nil {
			return err
		}
		dependerKey, err := canonicalContractKey(dependency.Depender)
		if err != nil {
			return err
		}
		dependedKey, err := canonicalContractKey(dependency.DependedOn)
		if err != nil {
			return err
		}
		dependencyKeys[index] = dependerKey + "\x00" + dependedKey + "\x00" + dependency.DependencyKind
	}
	if !strictlySorted(dependencyKeys) {
		return invalidProjection("catalog-projection", "dependencies are duplicate or unsorted")
	}
	deniedKeys := make([]string, len(body.DeniedObjects))
	for index, denied := range body.DeniedObjects {
		if err := denied.Object.Validate(); err != nil {
			return err
		}
		if denied.Owner != nil && *denied.Owner == "" {
			return invalidProjection("catalog-projection", "non-null denied object owner must be non-empty")
		}
		if denied.DependencyKind != nil && *denied.DependencyKind == "" {
			return invalidProjection("catalog-projection", "non-null denied dependency kind must be non-empty")
		}
		switch denied.ReasonCode {
		case "undeclared_object", "unsupported_object_kind", "unbound_internal_object", "dependency_outside_closure":
		default:
			return invalidProjection("catalog-projection", "denied object reason code is outside the closed set")
		}
		if denied.DependedOn != nil {
			if err := denied.DependedOn.Validate(); err != nil {
				return err
			}
		}
		deniedKey, err := canonicalContractKey(denied.Object)
		if err != nil {
			return err
		}
		deniedKeys[index] = deniedKey
	}
	if !strictlySorted(deniedKeys) {
		return invalidProjection("catalog-projection", "denied objects are duplicate or unsorted")
	}
	if err := validateCatalogExpressionClosure(body); err != nil {
		return err
	}
	return nil
}

func validateProjectedDeclaredCoverage(body CatalogProjectionBody) error {
	declared := make(map[string]ObjectIdentityProjection, len(body.DeclaredObjects))
	for _, identity := range body.DeclaredObjects {
		key, err := canonicalContractKey(identity)
		if err != nil {
			return err
		}
		declared[key] = identity
	}
	actual := make(map[string]struct{})
	requiredDeclared := make(map[string]struct{})
	addActual := func(identity ObjectIdentityProjection, required bool) error {
		key, err := canonicalContractKey(identity)
		if err != nil {
			return err
		}
		actual[key] = struct{}{}
		if required {
			requiredDeclared[key] = struct{}{}
		}
		return nil
	}
	if err := addActual(ObjectIdentityProjection{Schema: &SchemaObjectIdentity{Kind: "schema", Name: body.Schema.Name}}, false); err != nil {
		return err
	}
	for _, relation := range body.Relations {
		if err := addActual(ObjectIdentityProjection{Relation: &RelationObjectIdentity{Kind: "relation", Identity: relation.Identity}}, true); err != nil {
			return err
		}
		for _, column := range relation.Columns {
			if err := addActual(ObjectIdentityProjection{Column: &ColumnObjectIdentity{Kind: "column", Relation: relation.Identity, Name: column.Name}}, false); err != nil {
				return err
			}
		}
		for _, constraint := range relation.Constraints {
			if err := addActual(ObjectIdentityProjection{Constraint: &ConstraintObjectIdentity{Kind: "constraint", Relation: relation.Identity, Name: constraint.Name}}, false); err != nil {
				return err
			}
		}
		for _, index := range relation.Indexes {
			constraintOwned := false
			for _, constraint := range relation.Constraints {
				if constraint.Name != index.Name {
					continue
				}
				constraintOwned = constraint.Type == "primary_key" && index.Primary || constraint.Type == "unique" && index.Unique && !index.Primary || constraint.Type == "exclusion" && index.Exclusion
				if constraintOwned {
					break
				}
			}
			if err := addActual(ObjectIdentityProjection{Index: &IndexObjectIdentity{Kind: "index", Identity: TypeIdentity{Schema: relation.Identity.Schema, Name: index.Name}, Relation: relation.Identity}}, !constraintOwned); err != nil {
				return err
			}
		}
		for _, policy := range relation.Policies {
			if err := addActual(ObjectIdentityProjection{Policy: &PolicyObjectIdentity{Kind: "policy", Relation: relation.Identity, Name: policy.Name}}, true); err != nil {
				return err
			}
		}
		for _, trigger := range relation.Triggers {
			if trigger.Identity.Trigger != nil {
				if err := addActual(trigger.Identity, true); err != nil {
					return err
				}
			}
		}
	}
	for _, function := range body.Functions {
		if err := addActual(ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: function.Identity}}, true); err != nil {
			return err
		}
	}
	for key := range requiredDeclared {
		if _, ok := declared[key]; !ok {
			return invalidProjection("catalog-projection", "projected top-level object is outside declared_objects")
		}
	}
	for key, identity := range declared {
		switch identity.Kind() {
		case "schema", "relation", "column", "index", "policy", "function", "constraint", "trigger":
			if _, ok := actual[key]; !ok {
				return invalidProjection("catalog-projection", "declared object is absent from the catalog projection")
			}
		}
	}
	for _, denied := range body.DeniedObjects {
		key, err := canonicalContractKey(denied.Object)
		if err != nil {
			return err
		}
		if _, ok := declared[key]; ok {
			return invalidProjection("catalog-projection", "denied object overlaps declared_objects")
		}
	}
	return nil
}

func validateRelationProjections(relations []RelationProjection) error {
	keys := make([]string, len(relations))
	for relationIndex, relation := range relations {
		if err := relation.Identity.Validate(); err != nil || relation.Identity.Schema != projectionTargetSchema || relation.Owner == "" {
			return invalidProjection("catalog-projection", "relation identity or owner is invalid")
		}
		switch relation.Relkind {
		case "table", "partitioned_table", "view", "materialized_view", "foreign_table", "sequence":
		default:
			return invalidProjection("catalog-projection", "relation kind is outside the closed profile")
		}
		switch relation.Persistence {
		case "permanent", "unlogged", "temporary":
		default:
			return invalidProjection("catalog-projection", "relation persistence is outside the closed profile")
		}
		if relation.AccessMethod != nil && *relation.AccessMethod == "" {
			return invalidProjection("catalog-projection", "non-null relation access method is empty")
		}
		if relation.Relkind == "table" || relation.Relkind == "partitioned_table" || relation.Relkind == "materialized_view" {
			if relation.AccessMethod == nil {
				return invalidProjection("catalog-projection", "stored relation is missing its access method")
			}
		} else if relation.AccessMethod != nil {
			return invalidProjection("catalog-projection", "non-stored relation carries an access method")
		}
		if relation.ExplicitACL.Entries == nil || relation.Reloptions == nil || relation.Columns == nil || relation.Constraints == nil || relation.Indexes == nil || relation.Policies == nil || relation.Triggers == nil {
			return invalidProjection("catalog-projection", "relation projection is sparse")
		}
		if err := relation.ExplicitACL.Validate(); err != nil {
			return err
		}
		if err := validateACLOrigins(relation.ExplicitACL.Entries, "catalog_explicit"); err != nil {
			return err
		}
		allowedPrivileges := []string{"DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"}
		if relation.Relkind == "sequence" {
			allowedPrivileges = []string{"SELECT", "UPDATE", "USAGE"}
		}
		if err := validateACLPrivileges(relation.ExplicitACL.Entries, allowedPrivileges...); err != nil {
			return err
		}
		if !strictlySorted(relation.Reloptions) && len(relation.Reloptions) > 1 {
			return invalidProjection("catalog-projection", "relation options are duplicate or unsorted")
		}
		switch relation.ReplicaIdentity {
		case "default", "nothing", "full", "index":
		default:
			return invalidProjection("catalog-projection", "relation replica identity is outside the closed profile")
		}
		if relation.RLSForced && !relation.RLSEnabled {
			return invalidProjection("catalog-projection", "forced row security requires row security to be enabled")
		}
		if err := validateColumnProjections(relation); err != nil {
			return err
		}
		if err := validateConstraintProjections(relation); err != nil {
			return err
		}
		if err := validateIndexProjections(relation); err != nil {
			return err
		}
		if err := validatePolicyProjections(relation); err != nil {
			return err
		}
		if err := validateTriggerProjections(relation); err != nil {
			return err
		}
		keys[relationIndex] = relation.Identity.Schema + "\x00" + relation.Identity.Name
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "relations are duplicate or unsorted")
	}
	return nil
}

func validateColumnProjections(relation RelationProjection) error {
	var previousAttnum uint32
	names := make(map[string]struct{}, len(relation.Columns))
	for index, column := range relation.Columns {
		if column.Attnum == 0 || index > 0 && column.Attnum <= previousAttnum || column.Name == "" {
			return invalidProjection("catalog-projection", "columns are duplicate, unsorted, or incomplete")
		}
		if _, duplicate := names[column.Name]; duplicate {
			return invalidProjection("catalog-projection", "column identity is duplicate")
		}
		names[column.Name] = struct{}{}
		previousAttnum = column.Attnum
		if err := column.Type.Validate(); err != nil {
			return err
		}
		if _, err := ValidateSignedIntegerDecimal(column.TypmodInt32Decimal, 32); err != nil {
			return err
		}
		if column.Collation != nil {
			if err := column.Collation.Validate(); err != nil {
				return err
			}
		}
		switch column.Identity {
		case "none", "always", "by_default":
		default:
			return invalidProjection("catalog-projection", "column identity mode is outside the closed profile")
		}
		switch column.Generated {
		case "none", "stored":
		default:
			return invalidProjection("catalog-projection", "column generated mode is outside the PG15-PG17 profile")
		}
		if column.Generated == "stored" && column.Default == nil {
			return invalidProjection("catalog-projection", "generated column lacks its normalized expression")
		}
		if column.Default != nil {
			if err := validateExpressionNodeType(*column.Default, column.Type); err != nil {
				return err
			}
		}
		switch column.Storage {
		case "plain", "external", "extended", "main":
		default:
			return invalidProjection("catalog-projection", "column storage mode is outside the closed profile")
		}
		switch column.Compression {
		case "default", "pglz", "lz4":
		default:
			return invalidProjection("catalog-projection", "column compression is outside the closed profile")
		}
		if column.ExplicitACL.Entries == nil {
			return invalidProjection("catalog-projection", "column ACL is sparse")
		}
		if err := column.ExplicitACL.Validate(); err != nil {
			return err
		}
		if err := validateACLOrigins(column.ExplicitACL.Entries, "catalog_explicit"); err != nil {
			return err
		}
		if err := validateACLPrivileges(column.ExplicitACL.Entries, "INSERT", "REFERENCES", "SELECT", "UPDATE"); err != nil {
			return err
		}
	}
	return nil
}

func validateConstraintProjections(relation RelationProjection) error {
	keys := make([]string, len(relation.Constraints))
	columnSet := make(map[string]struct{}, len(relation.Columns))
	for _, column := range relation.Columns {
		columnSet[column.Name] = struct{}{}
	}
	for index, constraint := range relation.Constraints {
		if constraint.Name == "" || constraint.Columns == nil || constraint.ReferencedColumns == nil {
			return invalidProjection("catalog-projection", "constraint is sparse")
		}
		switch constraint.Type {
		case "primary_key", "unique":
			if constraint.ReferencedRelation != nil || len(constraint.ReferencedColumns) != 0 || constraint.Expression != nil {
				return invalidProjection("catalog-projection", "key constraint carries foreign or expression state")
			}
		case "foreign_key":
			if constraint.ReferencedRelation == nil || len(constraint.Columns) == 0 || len(constraint.Columns) != len(constraint.ReferencedColumns) || constraint.Expression != nil {
				return invalidProjection("catalog-projection", "foreign key constraint is incomplete")
			}
			if err := constraint.ReferencedRelation.Validate(); err != nil {
				return err
			}
		case "check":
			if constraint.ReferencedRelation != nil || len(constraint.ReferencedColumns) != 0 || constraint.Expression == nil {
				return invalidProjection("catalog-projection", "check constraint lacks its normalized expression")
			}
			if err := validateExpressionNodeType(*constraint.Expression, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
				return err
			}
		case "exclusion":
			if constraint.ReferencedRelation != nil || len(constraint.ReferencedColumns) != 0 {
				return invalidProjection("catalog-projection", "exclusion constraint carries foreign-key state")
			}
			if constraint.Expression != nil {
				if err := validateExpressionNodeType(*constraint.Expression, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
					return err
				}
			}
		default:
			return invalidProjection("catalog-projection", "constraint type is outside the closed profile")
		}
		if err := validateDistinctColumnNames("constraint columns", constraint.Columns, columnSet); err != nil {
			return err
		}
		if err := validateDistinctNames("referenced constraint columns", constraint.ReferencedColumns); err != nil {
			return err
		}
		if constraint.Deferred && !constraint.Deferrable {
			return invalidProjection("catalog-projection", "initially deferred constraint is not deferrable")
		}
		if err := validateConstraintActionProfile(constraint); err != nil {
			return err
		}
		keys[index] = constraint.Name
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "constraints are duplicate or unsorted")
	}
	return nil
}

func validateConstraintActionProfile(constraint ConstraintProjection) error {
	if constraint.Type != "foreign_key" {
		if constraint.Match != "none" || constraint.Update != "none" || constraint.Delete != "none" {
			return invalidProjection("catalog-projection", "non-foreign constraint carries referential actions")
		}
		return nil
	}
	switch constraint.Match {
	case "simple", "full", "partial":
	default:
		return invalidProjection("catalog-projection", "foreign key match type is outside the closed profile")
	}
	for _, action := range []string{constraint.Update, constraint.Delete} {
		switch action {
		case "no_action", "restrict", "cascade", "set_null", "set_default":
		default:
			return invalidProjection("catalog-projection", "foreign key action is outside the closed profile")
		}
	}
	return nil
}

func validateIndexProjections(relation RelationProjection) error {
	keys := make([]string, len(relation.Indexes))
	columnSet := make(map[string]struct{}, len(relation.Columns))
	for _, column := range relation.Columns {
		columnSet[column.Name] = struct{}{}
	}
	for index, projected := range relation.Indexes {
		if projected.Name == "" || projected.AccessMethod == "" || projected.Terms == nil || projected.Includes == nil {
			return invalidProjection("catalog-projection", "index projection is sparse")
		}
		if projected.Predicate != nil {
			if err := validateExpressionNodeType(*projected.Predicate, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
				return err
			}
		}
		for termIndex, term := range projected.Terms {
			if term.Ordinal != uint32(termIndex+1) || term.OpclassOptions == nil {
				return invalidProjection("catalog-projection", "index term ordinal or options are invalid")
			}
			switch term.TermKind {
			case "column":
				if term.Column == nil || term.Expression != nil {
					return invalidProjection("catalog-projection", "column index term is incomplete")
				}
				if _, ok := columnSet[*term.Column]; !ok {
					return invalidProjection("catalog-projection", "index term references an unknown column")
				}
			case "expression":
				if term.Column != nil || term.Expression == nil {
					return invalidProjection("catalog-projection", "expression index term is incomplete")
				}
				if err := validateExpressionNode(*term.Expression); err != nil {
					return err
				}
			default:
				return invalidProjection("catalog-projection", "index term kind is outside the closed profile")
			}
			if term.Opclass == nil {
				return invalidProjection("catalog-projection", "index term is missing its operator class")
			}
			if err := term.Opclass.Validate(); err != nil {
				return err
			}
			if !strictlySorted(term.OpclassOptions) && len(term.OpclassOptions) > 1 {
				return invalidProjection("catalog-projection", "index operator class options are duplicate or unsorted")
			}
			if term.Collation != nil {
				if err := term.Collation.Validate(); err != nil {
					return err
				}
			}
			switch term.Order {
			case "asc", "desc":
			default:
				return invalidProjection("catalog-projection", "index order is outside the closed profile")
			}
			switch term.Nulls {
			case "first", "last":
			default:
				return invalidProjection("catalog-projection", "index null ordering is outside the closed profile")
			}
			if term.ExclusionOperator != nil {
				if err := term.ExclusionOperator.Validate(); err != nil {
					return err
				}
			}
		}
		if err := validateDistinctColumnNames("index includes", projected.Includes, columnSet); err != nil {
			return err
		}
		keys[index] = projected.Name
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "indexes are duplicate or unsorted")
	}
	return nil
}

func validatePolicyProjections(relation RelationProjection) error {
	keys := make([]string, len(relation.Policies))
	for index, policy := range relation.Policies {
		if policy.Name == "" || policy.Roles == nil {
			return invalidProjection("catalog-projection", "policy projection is sparse")
		}
		switch policy.Command {
		case "all", "select", "insert", "update", "delete":
		default:
			return invalidProjection("catalog-projection", "policy command is outside the closed profile")
		}
		if !strictlySorted(policy.Roles) || len(policy.Roles) == 0 {
			return invalidProjection("catalog-projection", "policy roles are empty, duplicate, or unsorted")
		}
		for _, expression := range []*ExpressionNode{policy.Using, policy.WithCheck} {
			if expression != nil {
				if err := validateExpressionNodeType(*expression, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
					return err
				}
			}
		}
		keys[index] = policy.Name
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "policies are duplicate or unsorted")
	}
	return nil
}

func validateTriggerProjections(relation RelationProjection) error {
	keys := make([]string, len(relation.Triggers))
	for index, trigger := range relation.Triggers {
		if err := trigger.Identity.Validate(); err != nil || trigger.OwningRelation != relation.Identity {
			return invalidProjection("catalog-projection", "trigger identity or owning relation is invalid")
		}
		if err := trigger.Function.Validate(); err != nil {
			return err
		}
		if trigger.Columns == nil || trigger.Arguments == nil {
			return invalidProjection("catalog-projection", "trigger projection is sparse")
		}
		switch trigger.Enabled {
		case "origin", "always", "replica", "disabled":
		default:
			return invalidProjection("catalog-projection", "trigger enabled state is outside the closed profile")
		}
		if trigger.Type == 0 {
			return invalidProjection("catalog-projection", "trigger event type is empty")
		}
		if trigger.When != nil {
			if err := validateExpressionNodeType(*trigger.When, TypeIdentity{Schema: "pg_catalog", Name: "bool"}); err != nil {
				return err
			}
		}
		if trigger.Internal != (trigger.Identity.Internal != nil) {
			return invalidProjection("catalog-projection", "trigger internal flag differs from its normalized identity")
		}
		key, err := canonicalContractKey(trigger.Identity)
		if err != nil {
			return err
		}
		keys[index] = key
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "triggers are duplicate or unsorted")
	}
	return nil
}

func validateFunctionProjections(functions []FunctionProjection) error {
	keys := make([]string, len(functions))
	for functionIndex, function := range functions {
		if err := function.Identity.Validate(); err != nil || function.Identity.Schema != projectionTargetSchema || function.Language == "" || function.Owner == "" || function.Arguments == nil || function.Config == nil || function.ExplicitACL.Entries == nil {
			return invalidProjection("catalog-projection", "function projection is sparse or has invalid identity")
		}
		switch function.Kind {
		case "function", "procedure", "aggregate", "window":
		default:
			return invalidProjection("catalog-projection", "function kind is outside the closed profile")
		}
		if err := function.Returns.Validate(); err != nil {
			return err
		}
		if function.VariadicType != nil {
			if err := function.VariadicType.Validate(); err != nil {
				return err
			}
		}
		for argumentIndex, argument := range function.Arguments {
			if argument.Ordinal != uint32(argumentIndex+1) {
				return invalidProjection("catalog-projection", "function argument ordinals are not contiguous")
			}
			if argument.Name != nil && *argument.Name == "" {
				return invalidProjection("catalog-projection", "non-null function argument name is empty")
			}
			switch argument.Mode {
			case "in", "out", "inout", "variadic", "table":
			default:
				return invalidProjection("catalog-projection", "function argument mode is outside the closed profile")
			}
			if err := argument.Type.Validate(); err != nil {
				return err
			}
			if argument.Default != nil {
				if err := validateExpressionNodeType(*argument.Default, argument.Type); err != nil {
					return err
				}
			}
		}
		if err := function.ExplicitACL.Validate(); err != nil {
			return err
		}
		if err := validateACLOrigins(function.ExplicitACL.Entries, "catalog_explicit"); err != nil {
			return err
		}
		if err := validateACLPrivileges(function.ExplicitACL.Entries, "EXECUTE"); err != nil {
			return err
		}
		switch function.Volatility {
		case "immutable", "stable", "volatile":
		default:
			return invalidProjection("catalog-projection", "function volatility is outside the closed profile")
		}
		switch function.Parallel {
		case "safe", "restricted", "unsafe":
		default:
			return invalidProjection("catalog-projection", "function parallel mode is outside the closed profile")
		}
		if !strictlySorted(function.Config) && len(function.Config) > 1 {
			return invalidProjection("catalog-projection", "function config is duplicate or unsorted")
		}
		if err := ValidateExactNumeric(function.Cost); err != nil {
			return err
		}
		if err := ValidateExactNumeric(function.Rows); err != nil {
			return err
		}
		if err := requireDigest("catalog-projection.function.prosrc_sha256", function.ProsrcSHA256); err != nil {
			return err
		}
		if function.Probin != nil && *function.Probin == "" {
			return invalidProjection("catalog-projection", "non-null function probin is empty")
		}
		key, err := canonicalContractKey(function.Identity)
		if err != nil {
			return err
		}
		keys[functionIndex] = key
	}
	if !strictlySorted(keys) {
		return invalidProjection("catalog-projection", "functions are duplicate or unsorted")
	}
	return nil
}

func validateDistinctColumnNames(path string, values []string, available map[string]struct{}) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return invalidProjection("catalog-projection", path+" contain an empty name")
		}
		if _, duplicate := seen[value]; duplicate {
			return invalidProjection("catalog-projection", path+" are duplicate")
		}
		if _, ok := available[value]; !ok {
			return invalidProjection("catalog-projection", path+" reference an unknown column")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateDistinctNames(path string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return invalidProjection("catalog-projection", path+" contain an empty name")
		}
		if _, duplicate := seen[value]; duplicate {
			return invalidProjection("catalog-projection", path+" are duplicate")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (transition ExpectedStatementTransition) Validate() error {
	if transition.Profile != StatementTransitionProfile || transition.AuthorityRelation != "unchanged_relative_to_verified_binding" || transition.ControlPlaneDelta == nil {
		return invalidProjection("statement-transition", "invalid transition profile, authority relation, or delta")
	}
	if err := transition.CatalogBefore.Validate(); err != nil {
		return err
	}
	if err := transition.CatalogAfter.Validate(); err != nil {
		return err
	}
	keys := make([]string, len(transition.ControlPlaneDelta))
	for index, delta := range transition.ControlPlaneDelta {
		switch delta.ChangeKind {
		case "create", "alter", "grant", "revoke":
		default:
			return invalidProjection("statement-transition", "unknown object transition kind")
		}
		if err := delta.Object.Validate(); err != nil {
			return err
		}
		objectKey, err := canonicalContractKey(delta.Object)
		if err != nil {
			return err
		}
		grantee := ""
		if delta.Grantee != nil {
			if *delta.Grantee == "" {
				return invalidProjection("statement-transition", "non-null transition grantee must be non-empty")
			}
			grantee = *delta.Grantee
		}
		keys[index] = objectKey + "\x00" + delta.ChangeKind + "\x00" + grantee
	}
	if !strictlySorted(keys) {
		return invalidProjection("statement-transition", "control plane delta is duplicate or unsorted")
	}
	return nil
}

func validateExecutableCatalogBindings(contract CatalogContract) error {
	if !equalObjectIdentityClosures(contract.DeclaredObjectIdentities, contract.ExpectedProjection.Body.DeclaredObjects) {
		return invalidProjection("catalog-contract", "top-level and expected projection declared object closures differ")
	}
	if len(contract.SourceDescriptors) == 0 {
		return invalidProjection("catalog-contract", "executable catalog has no source descriptors")
	}
	finalSeen := false
	for sourceIndex, source := range contract.SourceDescriptors {
		if len(source.Statements) == 0 {
			return invalidProjection("catalog-contract", "executable source descriptor has no statements")
		}
		var previousAfter *CatalogStateDigestRef
		for statementIndex, statement := range source.Statements {
			if statement.Index > uint64(^uint32(0)) {
				return invalidProjection("catalog-contract", "statement index exceeds uint32 projection scope")
			}
			transition := statement.ExpectedTransition
			if statementIndex == 0 {
				if !scopeMatchesPredecessor(transition.CatalogBefore.Scope, source.MigrationID) {
					return invalidProjection("catalog-contract", "first statement before scope differs from its source descriptor")
				}
			} else {
				expectedBeforeIndex := uint32(statementIndex - 1)
				if !scopeMatchesStatementPrefix(transition.CatalogBefore.Scope, source.MigrationID, expectedBeforeIndex) || previousAfter == nil || previousAfter.Digest != transition.CatalogBefore.Digest || previousAfter.StateKind != transition.CatalogBefore.StateKind || !equalProjectionScopes(previousAfter.Scope, transition.CatalogBefore.Scope) {
					return invalidProjection("catalog-contract", "statement transition before state does not continue the signed statement chain")
				}
			}

			isFinal := source.MigrationID == contract.SchemaHead && sourceIndex == len(contract.SourceDescriptors)-1 && statementIndex == len(source.Statements)-1
			if isFinal {
				if finalSeen || transition.CatalogAfter.StateKind != "schema_present" || !scopeMatchesFinal(transition.CatalogAfter.Scope, contract.SchemaHead, contract.DeclaredObjectIdentities) {
					return invalidProjection("catalog-contract", "final transition scope differs from catalog head or declared closure")
				}
				finalState := CatalogStateProjection{Present: &SchemaPresentProjection{State: "schema_present", Scope: transition.CatalogAfter.Scope, Body: contract.ExpectedProjection.Body}}
				expectedDigest, err := finalState.ComputeDigest()
				if err != nil {
					return err
				}
				if transition.CatalogAfter.Digest != expectedDigest {
					return invalidProjection("catalog-contract", "final transition digest differs from expected projection")
				}
				finalSeen = true
			} else if !scopeMatchesStatementPrefix(transition.CatalogAfter.Scope, source.MigrationID, uint32(statementIndex)) {
				return invalidProjection("catalog-contract", "statement transition after scope differs from its source descriptor index")
			}
			previousAfter = &transition.CatalogAfter
		}
	}
	if !finalSeen {
		return invalidProjection("catalog-contract", "executable catalog lacks a final transition for its schema head")
	}
	return nil
}

func scopeMatchesPredecessor(scope ProjectionScope, migrationID string) bool {
	return scope.ScopeKind == "predecessor" && scope.SchemaHead == nil && scope.MigrationID != nil && *scope.MigrationID == migrationID && scope.ThroughStatementIndex == nil
}

func scopeMatchesStatementPrefix(scope ProjectionScope, migrationID string, statementIndex uint32) bool {
	return scope.ScopeKind == "statement_prefix" && scope.SchemaHead == nil && scope.MigrationID != nil && *scope.MigrationID == migrationID && scope.ThroughStatementIndex != nil && *scope.ThroughStatementIndex == statementIndex
}

func scopeMatchesFinal(scope ProjectionScope, schemaHead string, declared []ObjectIdentityProjection) bool {
	return scope.ScopeKind == "final" && scope.SchemaHead != nil && *scope.SchemaHead == schemaHead && scope.MigrationID == nil && scope.ThroughStatementIndex == nil && equalObjectIdentityClosures(scope.DeclaredObjects, declared)
}

func equalProjectionScopes(left, right ProjectionScope) bool {
	leftKey, leftErr := canonicalContractKey(left)
	rightKey, rightErr := canonicalContractKey(right)
	return leftErr == nil && rightErr == nil && leftKey == rightKey
}

func (ref CatalogStateDigestRef) Validate() error {
	if err := ref.Scope.Validate(); err != nil {
		return err
	}
	if ref.StateKind != "schema_absent" && ref.StateKind != "schema_present" {
		return invalidProjection("statement-transition", "unknown catalog state kind")
	}
	return requireDigest("statement_transition.catalog_state.digest", ref.Digest)
}

func ValidateSignedIntegerDecimal(value string, bits int) (int64, error) {
	if !signedIntegerDecimalPattern.MatchString(value) {
		return 0, invalidProjection("numeric-profile", "signed integer is not canonical decimal")
	}
	if bits != 16 && bits != 32 && bits != 64 {
		return 0, invalidProjection("numeric-profile", "unsupported signed integer width")
	}
	parsed, err := strconv.ParseInt(value, 10, bits)
	if err != nil {
		return 0, invalidProjection("numeric-profile", fmt.Sprintf("signed integer exceeds int%d", bits))
	}
	return parsed, nil
}

func ValidateExactNumeric(value string) error {
	if len(value) == 0 || len(value) > 128 || value == "-0" || !exactNumericPattern.MatchString(value) {
		return invalidProjection("numeric-profile", "numeric is not canonical exact decimal")
	}
	return nil
}

// CanonicalExactNumeric accepts PostgreSQL plain-decimal output and returns the
// ADR-0010 exact numeric spelling. Exponents, a leading plus, and negative zero
// remain forbidden; positive zero fractions collapse to "0".
func CanonicalExactNumeric(value string) (string, error) {
	if len(value) == 0 || len(value) > 128 || strings.ContainsAny(value, "eE+") {
		return "", invalidProjection("numeric-profile", "numeric is outside the exact decimal input profile")
	}
	negative := strings.HasPrefix(value, "-")
	unsigned := strings.TrimPrefix(value, "-")
	integer, fraction, hasFraction := strings.Cut(unsigned, ".")
	if integer == "" || (len(integer) > 1 && integer[0] == '0') {
		return "", invalidProjection("numeric-profile", "numeric integer part is not canonical")
	}
	for _, digit := range integer + fraction {
		if digit < '0' || digit > '9' {
			return "", invalidProjection("numeric-profile", "numeric contains a non-decimal digit")
		}
	}
	if hasFraction && fraction == "" {
		return "", invalidProjection("numeric-profile", "numeric fraction is empty")
	}
	fraction = strings.TrimRight(fraction, "0")
	zero := integer == "0" && fraction == ""
	if negative && zero {
		return "", invalidProjection("numeric-profile", "negative zero is forbidden")
	}
	canonical := integer
	if fraction != "" {
		canonical += "." + fraction
	}
	if negative {
		canonical = "-" + canonical
	}
	if err := ValidateExactNumeric(canonical); err != nil {
		return "", err
	}
	return canonical, nil
}

func ValidateRyuFloat32(value string) error { return validateRyuFloat(value, 32) }
func ValidateRyuFloat64(value string) error { return validateRyuFloat(value, 64) }

func validateRyuFloat(value string, bits int) error {
	if len(value) == 0 || len(value) > 32 || !ryuDecimalPattern.MatchString(value) {
		return invalidProjection("numeric-profile", "float is outside cloud-agents-ryu-v1 lexical profile")
	}
	parsed, err := strconv.ParseFloat(value, bits)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return invalidProjection("numeric-profile", "float is not finite in the requested PostgreSQL width")
	}
	if parsed == 0 && math.Signbit(parsed) {
		return invalidProjection("numeric-profile", "negative zero is forbidden")
	}
	canonical := normalizeRyuExponent(strconv.FormatFloat(parsed, 'g', -1, bits))
	if canonical != value {
		return invalidProjection("numeric-profile", "float is not the shortest canonical round-trip decimal")
	}
	return nil
}

func normalizeRyuExponent(value string) string {
	marker := strings.IndexByte(value, 'e')
	if marker < 0 {
		return value
	}
	mantissa, exponent := value[:marker], value[marker+1:]
	negative := strings.HasPrefix(exponent, "-")
	exponent = strings.TrimPrefix(strings.TrimPrefix(exponent, "+"), "-")
	exponent = strings.TrimLeft(exponent, "0")
	if exponent == "" {
		exponent = "0"
	}
	if negative {
		exponent = "-" + exponent
	}
	return mantissa + "e" + exponent
}

func (state StatementIntermediateState) Validate() error {
	if state.AttemptIndex == 0 || !migrationIDPattern.MatchString(state.MigrationID) {
		return invalidProjection("intermediate-state", "attempt and migration identity are required")
	}
	if (state.AttemptIndex == 1) != (state.PreviousAttemptTerminalDigest == nil) {
		return invalidProjection("intermediate-state", "previous attempt terminal linkage is invalid")
	}
	if (state.StatementIndex == 0) != (state.PreviousIntermediateStateDigest == nil) {
		return invalidProjection("intermediate-state", "previous intermediate linkage is invalid")
	}
	for field, digest := range map[string]Digest{
		"schema_bundle": state.SchemaBundleDigest, "catalog_contract": state.CatalogContractDigest,
		"authority_profile": state.AuthorityProfileDigest, "authority_binding": state.AuthorityBindingDigest,
		"statement": state.StatementSHA256, "authority_before": state.AuthorityBeforeDigest,
		"authority_after": state.AuthorityAfterDigest, "catalog_before": state.CatalogBeforeDigest,
		"catalog_after": state.CatalogAfterDigest, "intermediate": state.IntermediateStateDigest,
	} {
		if err := requireDigest("intermediate_state."+field, digest); err != nil {
			return err
		}
	}
	if state.PreviousAttemptTerminalDigest != nil {
		if err := requireDigest("intermediate_state.previous_attempt_terminal", *state.PreviousAttemptTerminalDigest); err != nil {
			return err
		}
	}
	if state.PreviousIntermediateStateDigest != nil {
		if err := requireDigest("intermediate_state.previous_intermediate", *state.PreviousIntermediateStateDigest); err != nil {
			return err
		}
	}
	if err := state.ControlPlaneStates.Validate(); err != nil {
		return err
	}
	expected, err := state.ComputeDigest()
	if err != nil {
		return err
	}
	if expected != state.IntermediateStateDigest {
		return invalidProjection("intermediate-state", "intermediate state digest mismatch")
	}
	return nil
}

func (states ControlPlaneStates) Validate() error {
	if states.TxStatus != "T" || states.CurrentUser != MigrationOwnerRole || states.MigrationRole != MigrationOwnerRole || states.SessionUser == "" || states.SchemaOwner == "" {
		return invalidProjection("control-plane-states", "transaction or authority control-plane state is invalid")
	}
	if states.AdvisoryLock.Domain != AdvisoryLockDomain || !states.AdvisoryLock.Held {
		return invalidProjection("control-plane-states", "advisory lock is not held")
	}
	if _, err := (AdvisoryLock{Domain: states.AdvisoryLock.Domain, Derivation: AdvisoryLockDerivation, KeyInt64Decimal: states.AdvisoryLock.KeyInt64Decimal}).Key(); err != nil {
		return err
	}
	for field, digest := range map[string]Digest{
		"verified_authority_decision": states.VerifiedAuthorityDecisionDigest,
		"schema_explicit_acl":         states.SchemaExplicitACLDigest,
		"schema_effective_acl":        states.SchemaEffectiveACLDigest,
		"default_acl":                 states.DefaultACLDigest,
		"expected_transition":         states.ExpectedTransitionDigest,
	} {
		if err := requireDigest("control_plane_states."+field, digest); err != nil {
			return err
		}
	}
	return nil
}

func (state AttemptTerminalState) Validate(maxAttempts ...uint32) error {
	if state.AttemptIndex == 0 || !migrationIDPattern.MatchString(state.MigrationID) {
		return invalidProjection("attempt-terminal", "attempt index or migration identity is invalid")
	}
	if len(maxAttempts) > 1 || len(maxAttempts) == 1 && (maxAttempts[0] == 0 || state.AttemptIndex > maxAttempts[0]) {
		return invalidProjection("attempt-terminal", "attempt index exceeds external maximum")
	}
	if (state.AttemptIndex == 1) != (state.PreviousAttemptTerminalDigest == nil) {
		return invalidProjection("attempt-terminal", "previous attempt terminal linkage is invalid")
	}
	for field, digest := range map[string]Digest{
		"schema_bundle": state.SchemaBundleDigest, "catalog_contract": state.CatalogContractDigest,
		"authority_profile": state.AuthorityProfileDigest, "authority_binding": state.AuthorityBindingDigest,
		"terminal": state.TerminalDigest,
	} {
		if err := requireDigest("attempt_terminal."+field, digest); err != nil {
			return err
		}
	}
	if state.PreviousAttemptTerminalDigest != nil {
		if err := requireDigest("attempt_terminal.previous", *state.PreviousAttemptTerminalDigest); err != nil {
			return err
		}
	}
	if state.LastIntermediateStateDigest != nil {
		if err := requireDigest("attempt_terminal.last_intermediate", *state.LastIntermediateStateDigest); err != nil {
			return err
		}
	}
	if (state.StableErrorCode == nil) != (state.FailureEvidence == nil) {
		return invalidProjection("attempt-terminal", "stable error and failure evidence nullability differ")
	}
	if state.FailureEvidence != nil {
		if err := state.FailureEvidence.Validate(); err != nil {
			return err
		}
		if string(state.FailureEvidence.Code) != *state.StableErrorCode {
			return invalidProjection("attempt-terminal", "stable error differs from failure evidence")
		}
	}
	if state.RetryProof != nil {
		if err := state.RetryProof.Validate(); err != nil {
			return err
		}
	}
	hasError := state.StableErrorCode != nil
	switch state.Outcome {
	case "committed":
		if hasError || state.RetryProof != nil || state.ReconcileResult != "not_run" || state.LastIntermediateStateDigest == nil {
			return invalidProjection("attempt-terminal", "illegal committed outcome combination")
		}
	case "aborted_retryable":
		if !hasError || !state.FailureEvidence.Retryable || state.RetryProof == nil || state.ReconcileResult != "not_run" || len(maxAttempts) == 1 && state.AttemptIndex >= maxAttempts[0] {
			return invalidProjection("attempt-terminal", "illegal retryable outcome combination")
		}
		kind := state.RetryProof.ProofKind
		if *state.StableErrorCode == string(CodeProjectionCatalogQueryFailed) && kind != "projection_transient_exact_predecessor" ||
			*state.StableErrorCode == string(CodeTransactionBoundary) && kind != "precommit_rollback_exact_predecessor" && kind != "precommit_connection_terminated_exact_predecessor" && kind != "commit_rejected_exact_predecessor" ||
			*state.StableErrorCode != string(CodeProjectionCatalogQueryFailed) && *state.StableErrorCode != string(CodeTransactionBoundary) ||
			state.RetryProof.CommitRejectedReason != nil && *state.RetryProof.CommitRejectedReason == "other_confirmed_postgres_error" {
			return invalidProjection("attempt-terminal", "retry proof does not match retryable outcome")
		}
	case "aborted_terminal":
		if !hasError || state.FailureEvidence.Retryable || state.ReconcileResult != "not_run" || *state.StableErrorCode == string(CodeAmbiguousCommit) {
			return invalidProjection("attempt-terminal", "illegal terminal abort combination")
		}
		if *state.StableErrorCode == string(CodeTransactionBoundary) {
			if state.RetryProof == nil || state.RetryProof.ProofKind == "projection_transient_exact_predecessor" {
				return invalidProjection("attempt-terminal", "transaction abort lacks boundary proof")
			}
		} else if state.RetryProof != nil {
			return invalidProjection("attempt-terminal", "non-transaction abort carries retry proof")
		}
	case "ambiguous_reconciled_committed":
		if !validAmbiguousTerminal(state, "exact_committed") {
			return invalidProjection("attempt-terminal", "illegal reconciled committed combination")
		}
	case "ambiguous_reconciled_pending":
		if !validAmbiguousTerminal(state, "exact_pending") {
			return invalidProjection("attempt-terminal", "illegal reconciled pending combination")
		}
	case "ambiguous_divergent":
		if !validAmbiguousTerminal(state, "divergent") {
			return invalidProjection("attempt-terminal", "illegal divergent combination")
		}
	case "ambiguous_unresolved":
		if !validUnresolvedTerminal(state) {
			return invalidProjection("attempt-terminal", "illegal unresolved combination")
		}
	default:
		return invalidProjection("attempt-terminal", "unknown terminal outcome")
	}
	expected, err := state.ComputeDigest()
	if err != nil {
		return err
	}
	if expected != state.TerminalDigest {
		return invalidProjection("attempt-terminal", "terminal digest mismatch")
	}
	return nil
}

func validAmbiguousTerminal(state AttemptTerminalState, reconcile string) bool {
	if state.StableErrorCode == nil || state.FailureEvidence == nil || state.FailureEvidence.Retryable || state.RetryProof != nil || state.LastIntermediateStateDigest == nil || state.ReconcileResult != reconcile {
		return false
	}
	code := ErrorCode(*state.StableErrorCode)
	return code == CodeAmbiguousCommit || code == CodeEvidenceJournalFailed || code == CodeEvidenceRecoveryRequired
}

func validUnresolvedTerminal(state AttemptTerminalState) bool {
	if state.StableErrorCode == nil || state.FailureEvidence == nil || state.FailureEvidence.Retryable || state.RetryProof != nil || state.LastIntermediateStateDigest == nil || state.ReconcileResult != "unresolved" {
		return false
	}
	switch ErrorCode(*state.StableErrorCode) {
	case CodeAmbiguousCommit, CodeUntrusted, CodeEvidenceJournalFailed, CodeEvidenceRecoveryRequired, CodeContextCanceled, CodeDeadlineExceeded:
		return true
	default:
		return false
	}
}

func validStableProjectionError(code string) bool {
	_, ok := map[string]struct{}{
		"MIGRATION_PROJECTION_UNSUPPORTED_MAJOR": {}, "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED": {},
		"MIGRATION_PROJECTION_CAPABILITY_MISMATCH": {},
		"MIGRATION_PROJECTION_LIMIT_EXCEEDED":      {}, "MIGRATION_PROJECTION_UNKNOWN_OBJECT": {},
		"MIGRATION_PROJECTION_INVALID_EXPRESSION": {}, "MIGRATION_AUTHORITY_DRIFT": {},
		"MIGRATION_PROJECTION_INVALID_SCOPE": {}, "MIGRATION_PROJECTION_NON_CANONICAL_WITNESS": {},
		"MIGRATION_PROJECTION_LIMIT_OVERRIDE": {}, "MIGRATION_PROJECTION_METADATA_MISMATCH": {},
		"MIGRATION_INTERMEDIATE_STATE_MISMATCH": {}, "MIGRATION_PROJECTION_SNAPSHOT_INVALID": {},
		"MIGRATION_PROJECTION_NOT_IMPLEMENTED": {}, "MIGRATION_CATALOG_DRIFT": {},
	}[code]
	return ok
}

func (state CatalogStateProjection) ComputeDigest() (Digest, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}
	return digestFlatDomain(CatalogStateDigestDomain, state, "")
}

func (transition ExpectedStatementTransition) ComputeDigest() (Digest, error) {
	if err := transition.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(transition)
	if err != nil {
		return "", invalidProjection("statement-transition", "cannot encode transition")
	}
	value, err := ParseStrictJSON(raw)
	if err != nil {
		return "", err
	}
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return DigestBytes(canonical), nil
}

func (state StatementIntermediateState) ComputeDigest() (Digest, error) {
	return digestFlatDomain(IntermediateStateDigestDomain, state, "intermediate_state_digest")
}

func (state AttemptTerminalState) ComputeDigest() (Digest, error) {
	return digestFlatDomain(AttemptTerminalDigestDomain, state, "terminal_digest")
}

func digestFlatDomain(domain string, value any, excludedField string) (Digest, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", invalidProjection("projection-digest", "cannot encode typed projection")
	}
	parsed, err := ParseStrictJSON(raw)
	if err != nil {
		return "", err
	}
	object, ok := parsed.(map[string]JSONValue)
	if !ok {
		return "", invalidProjection("projection-digest", "digest input is not an object")
	}
	if excludedField != "" {
		delete(object, excludedField)
	}
	object["domain"] = domain
	canonical, err := CanonicalJSON(object)
	if err != nil {
		return "", err
	}
	return DigestBytes(canonical), nil
}

func canonicalContractKey(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", invalidProjection("projection-contract", "cannot encode typed key")
	}
	parsed, err := ParseStrictJSON(raw)
	if err != nil {
		return "", err
	}
	canonical, err := CanonicalJSON(parsed)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func strictlySorted(values []string) bool {
	return sort.SliceIsSorted(values, func(i, j int) bool { return values[i] < values[j] }) && noDuplicateStrings(values)
}

func noDuplicateStrings(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return false
		}
	}
	return true
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func invalidProjection(op, message string) error {
	return fail(CodeInvalidManifest, op, message, nil)
}

func cloneObjectIdentities(values []ObjectIdentityProjection) []ObjectIdentityProjection {
	if values == nil {
		return nil
	}
	cloned := make([]ObjectIdentityProjection, len(values))
	copy(cloned, values)
	return cloned
}

func equalObjectIdentityClosures(left, right []ObjectIdentityProjection) bool {
	if len(left) != len(right) || (left == nil) != (right == nil) {
		return false
	}
	for index := range left {
		leftKey, leftErr := canonicalContractKey(left[index])
		rightKey, rightErr := canonicalContractKey(right[index])
		if leftErr != nil || rightErr != nil || leftKey != rightKey {
			return false
		}
	}
	return true
}
