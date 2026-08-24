package migration

import (
	"sort"
	"strings"
	"unicode/utf8"
)

type aclAccumulator struct {
	major        uint16
	catalogValue string
	origin       string
	entries      map[string]*aclEntryAccumulator
	count        int
}

type aclEntryAccumulator struct {
	grantor    string
	grantee    string
	privileges map[string]struct{}
	grantable  map[string]struct{}
}

func newACLAccumulator(major uint16, catalogNull bool, origin string) *aclAccumulator {
	catalogValue := "explicit"
	if catalogNull {
		catalogValue = "null"
	}
	return &aclAccumulator{major: major, catalogValue: catalogValue, origin: origin, entries: make(map[string]*aclEntryAccumulator)}
}

func (accumulator *aclAccumulator) add(path, grantor, grantee, privilege string, grantable bool, allowed map[string]struct{}) error {
	if accumulator == nil || accumulator.catalogValue == "null" {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, path, accumulator.major, "null catalog ACL produced an expanded entry")
	}
	if grantor == "" || grantee == "" || !utf8.ValidString(grantor) || !utf8.ValidString(grantee) {
		return pgProjectionFailure(CodeProjectionUnknownObject, path, accumulator.major, "ACL principal is missing or invalid")
	}
	privilege = normalizePrivilege(privilege)
	if _, ok := allowed[privilege]; !ok {
		return pgProjectionFailure(CodeProjectionUnknownObject, path, accumulator.major, "ACL privilege is outside the closed profile")
	}
	key := grantor + "\x00" + grantee
	entry := accumulator.entries[key]
	if entry == nil {
		if uint64(accumulator.count) >= projectionMaxACLEntries {
			return pgProjectionFailure(CodeProjectionLimitExceeded, path, accumulator.major, "ACL entry limit exceeded")
		}
		entry = &aclEntryAccumulator{grantor: grantor, grantee: grantee, privileges: make(map[string]struct{}), grantable: make(map[string]struct{})}
		accumulator.entries[key] = entry
		accumulator.count++
	}
	if _, duplicate := entry.privileges[privilege]; duplicate {
		return pgProjectionFailure(CodeProjectionUnknownObject, path, accumulator.major, "duplicate ACL privilege row")
	}
	entry.privileges[privilege] = struct{}{}
	if grantable {
		entry.grantable[privilege] = struct{}{}
	}
	return nil
}

func (accumulator *aclAccumulator) projection() ACLSetProjection {
	if accumulator == nil {
		return ACLSetProjection{CatalogValue: "null", Entries: []ACLProjection{}}
	}
	keys := make([]string, 0, len(accumulator.entries))
	for key := range accumulator.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]ACLProjection, 0, len(keys))
	for _, key := range keys {
		raw := accumulator.entries[key]
		privileges := privilegeSet(raw.privileges)
		grantable := privilegeSet(raw.grantable)
		entries = append(entries, ACLProjection{Grantor: raw.grantor, Grantee: raw.grantee, Privileges: privileges, Grantable: grantable, Origin: accumulator.origin})
	}
	return ACLSetProjection{CatalogValue: accumulator.catalogValue, Entries: entries}
}

func effectiveSchemaACL(owner string, explicit ACLSetProjection) ([]ACLProjection, error) {
	byPrincipal := make(map[string]ACLProjection, len(explicit.Entries)+1)
	for _, entry := range explicit.Entries {
		entry.Origin = "catalog_explicit"
		byPrincipal[entry.Grantor+"\x00"+entry.Grantee] = entry
	}
	ownerKey := owner + "\x00" + owner
	ownerEntry, present := byPrincipal[ownerKey]
	if !present {
		ownerEntry = ACLProjection{Grantor: owner, Grantee: owner, Privileges: []string{}, Grantable: []string{}, Origin: "owner_implicit"}
	}
	ownerEntry.Origin = "owner_implicit"
	ownerEntry.Privileges = sortedUniquePrivileges(append(ownerEntry.Privileges, "CREATE", "USAGE"))
	ownerEntry.Grantable = sortedUniquePrivileges(append(ownerEntry.Grantable, "CREATE", "USAGE"))
	byPrincipal[ownerKey] = ownerEntry
	keys := make([]string, 0, len(byPrincipal))
	for key := range byPrincipal {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ACLProjection, 0, len(keys))
	for _, key := range keys {
		result = append(result, byPrincipal[key])
	}
	if uint64(len(result)) > projectionMaxACLEntries {
		return nil, pgProjectionFailure(CodeProjectionLimitExceeded, "namespace.effective_acl", 0, "effective ACL entry limit exceeded")
	}
	return result, nil
}

func normalizePrivilege(value string) string {
	value = strings.ToUpper(value)
	if value == "TEMP" {
		return "TEMPORARY"
	}
	return value
}

func privilegeSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sortPrivileges(result)
	return result
}

func sortedUniquePrivileges(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return privilegeSet(set)
}

func sortPrivileges(values []string) {
	sort.Slice(values, func(left, right int) bool {
		leftRank, leftKnown := privilegeOrder[values[left]]
		rightRank, rightKnown := privilegeOrder[values[right]]
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return values[left] < values[right]
	})
}

var databasePrivileges = map[string]struct{}{
	"CONNECT": {}, "CREATE": {}, "TEMPORARY": {},
}

var schemaPrivileges = map[string]struct{}{
	"CREATE": {}, "USAGE": {},
}

var defaultACLPrivileges = map[string]map[string]struct{}{
	"table":    {"DELETE": {}, "INSERT": {}, "REFERENCES": {}, "SELECT": {}, "TRIGGER": {}, "TRUNCATE": {}, "UPDATE": {}},
	"sequence": {"SELECT": {}, "UPDATE": {}, "USAGE": {}},
	"function": {"EXECUTE": {}},
	"type":     {"USAGE": {}},
	"schema":   {"CREATE": {}, "USAGE": {}},
}

func mapDefaultACLObjectKind(raw string) (string, error) {
	switch raw {
	case "r":
		return "table", nil
	case "S":
		return "sequence", nil
	case "f":
		return "function", nil
	case "T":
		return "type", nil
	case "n":
		return "schema", nil
	default:
		return "", pgProjectionFailure(CodeProjectionInvalidScope, "default-acl.object-kind", 0, "default ACL object kind is unknown")
	}
}
