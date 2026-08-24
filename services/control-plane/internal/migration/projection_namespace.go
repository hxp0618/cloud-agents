package migration

import (
	"context"
	"sort"
)

const (
	schemaExplicitACLDigestDomain  = "cloud-agents-platform-schema-explicit-acl/v1"
	schemaEffectiveACLDigestDomain = "cloud-agents-platform-schema-effective-acl/v1"
	defaultACLDigestDomain         = "cloud-agents-platform-default-acl/v1"
)

type pgNamespaceProjection struct {
	Absent bool
	Body   CatalogProjectionBody
}

type pgNamespaceObject struct {
	Kind    string
	Name    string
	Subkind string
	Owner   string
}

func (projector *PGProjector) readNamespace(ctx context.Context, snapshot ProjectionSnapshot, scope ProjectionScope, defaultACLOwners, objectCreatorClosure []string) (pgNamespaceProjection, error) {
	projection, _, err := projector.readNamespaceMode(ctx, snapshot, scope, defaultACLOwners, objectCreatorClosure, false)
	return projection, err
}

func (projector *PGProjector) readCatalogNamespace(ctx context.Context, snapshot ProjectionSnapshot, scope ProjectionScope, defaultACLOwners, objectCreatorClosure []string) (pgNamespaceProjection, []pgNamespaceObject, error) {
	return projector.readNamespaceMode(ctx, snapshot, scope, defaultACLOwners, objectCreatorClosure, true)
}

func (projector *PGProjector) readNamespaceMode(ctx context.Context, snapshot ProjectionSnapshot, scope ProjectionScope, defaultACLOwners, objectCreatorClosure []string, catalogMode bool) (pgNamespaceProjection, []pgNamespaceObject, error) {
	if err := scope.Validate(); err != nil {
		return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionInvalidScope, "namespace.scope", projector.major, "projection scope is invalid")
	}
	if !catalogMode {
		for _, object := range scope.DeclaredObjects {
			if object.Schema == nil || object.Schema.Kind != "schema" || object.Schema.Name != projectionTargetSchema {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionUnknownObject, "namespace.scope.declared_objects", projector.major, "A2.1a scope contains an object deferred to A2.1b")
			}
		}
	}
	rows, cancel, err := queryProjectionBounded(ctx, snapshot, projectionQueryNamespace, projectionTargetSchema)
	if err != nil {
		return pgNamespaceProjection{}, nil, err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	var absent, schemaSeen bool
	var schema SchemaProjection
	var acl *aclAccumulator
	objects := make([]pgNamespaceObject, 0)
	for rows.Next() {
		var rowKind string
		var namespace, owner *string
		var aclIsNull bool
		var grantor, grantee, privilege *string
		var isGrantable *bool
		var value1, value2, value3 *string
		if err := rows.Scan(&rowKind, &namespace, &owner, &aclIsNull, &grantor, &grantee, &privilege, &isGrantable, &value1, &value2, &value3); err != nil {
			return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.scan", projector.major, "namespace projection scan failed")
		}
		if err := budget.add("namespace", rowKind, nullableString(namespace), nullableString(owner), nullableString(grantor), nullableString(grantee), nullableString(privilege), nullableString(value1), nullableString(value2), nullableString(value3)); err != nil {
			return pgNamespaceProjection{}, nil, err
		}
		switch rowKind {
		case "absent":
			if absent || schemaSeen || namespace == nil || *namespace != projectionTargetSchema {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.absent", projector.major, "schema absence row is invalid")
			}
			absent = true
		case "schema":
			if absent || schemaSeen || namespace == nil || owner == nil || *namespace != projectionTargetSchema {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.schema", projector.major, "schema identity row is duplicate or invalid")
			}
			schemaSeen = true
			schema = SchemaProjection{Name: *namespace, Owner: *owner, Comment: value1, SecurityLabels: []SecurityLabel{}}
			acl = newACLAccumulator(projector.major, aclIsNull, "catalog_explicit")
		case "schema_acl":
			if !schemaSeen {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.schema_acl", projector.major, "schema ACL row is sparse or out of order")
			}
			if grantor == nil || grantee == nil {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionUnknownObject, "namespace.schema_acl.principal", projector.major, "schema ACL references an unknown principal")
			}
			if privilege == nil || isGrantable == nil {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.schema_acl", projector.major, "schema ACL row is sparse")
			}
			if err := acl.add("namespace.schema_acl", *grantor, *grantee, *privilege, *isGrantable, schemaPrivileges); err != nil {
				return pgNamespaceProjection{}, nil, err
			}
		case "security_label":
			if !schemaSeen || value1 == nil || value2 == nil {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.security_labels", projector.major, "security label row is sparse or out of order")
			}
			if uint64(len(schema.SecurityLabels)) >= projectionMaxSecurityLabelsPerObject {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionLimitExceeded, "namespace.security_labels", projector.major, "security label limit exceeded")
			}
			schema.SecurityLabels = append(schema.SecurityLabels, SecurityLabel{Provider: *value1, Label: *value2})
		case "relation", "function", "type", "extension", "collation", "operator", "opclass", "opfamily", "conversion", "ts_config", "ts_dict", "ts_parser", "ts_template", "statistic_ext":
			if !catalogMode {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionUnknownObject, "namespace.objects."+rowKind, projector.major, "A2.1a detected an object deferred to A2.1b")
			}
			if namespace == nil || *namespace != projectionTargetSchema || value1 == nil || owner == nil || *value1 == "" || *owner == "" {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.objects."+rowKind, projector.major, "catalog object inventory row is sparse")
			}
			objects = append(objects, pgNamespaceObject{Kind: rowKind, Name: *value1, Subkind: nullableString(value2), Owner: *owner})
			if uint64(len(objects)) > projectionMaxCatalogObjects {
				return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionLimitExceeded, "namespace.objects", projector.major, "catalog object limit exceeded")
			}
		default:
			return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionUnknownObject, "namespace.row_kind", projector.major, "namespace query returned an unknown object kind")
		}
	}
	if err := rows.Err(); err != nil {
		return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.iteration", projector.major, "namespace projection iteration failed")
	}
	if absent {
		return pgNamespaceProjection{Absent: true}, []pgNamespaceObject{}, nil
	}
	if !schemaSeen || acl == nil {
		return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.schema", projector.major, "namespace query omitted schema state")
	}
	schema.ExplicitACL = acl.projection()
	schema.EffectiveACL, err = effectiveSchemaACL(schema.Owner, schema.ExplicitACL)
	if err != nil {
		return pgNamespaceProjection{}, nil, err
	}
	if err := projector.reconcileObjectCreators(ctx, snapshot, objectCreatorClosure); err != nil {
		return pgNamespaceProjection{}, nil, err
	}
	sort.Slice(schema.SecurityLabels, func(left, right int) bool {
		return schema.SecurityLabels[left].Provider < schema.SecurityLabels[right].Provider
	})
	for index := 1; index < len(schema.SecurityLabels); index++ {
		if schema.SecurityLabels[index-1].Provider == schema.SecurityLabels[index].Provider {
			return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionUnknownObject, "namespace.security_labels", projector.major, "security label provider is duplicate")
		}
	}
	defaultACL, err := projector.readDefaultACL(ctx, snapshot, defaultACLOwners, objectCreatorClosure)
	if err != nil {
		return pgNamespaceProjection{}, nil, err
	}
	totalACLEntries := uint64(len(schema.ExplicitACL.Entries) + len(schema.EffectiveACL))
	for _, row := range defaultACL {
		totalACLEntries += uint64(len(row.ACL.Entries))
	}
	if totalACLEntries > projectionMaxACLEntries {
		return pgNamespaceProjection{}, nil, pgProjectionFailure(CodeProjectionLimitExceeded, "namespace.acl_entries", projector.major, "namespace ACL entry limit exceeded")
	}
	return pgNamespaceProjection{Body: CatalogProjectionBody{
		Schema: schema, DefaultACL: defaultACL,
		Relations: []RelationProjection{}, Functions: []FunctionProjection{}, Dependencies: []DependencyProjection{},
		ObjectCount: uint32(len(scope.DeclaredObjects)), DeclaredObjects: cloneProjectionValue(scope.DeclaredObjects), DeniedObjects: []DeniedObjectProjection{},
	}}, objects, nil
}

func (projector *PGProjector) reconcileObjectCreators(ctx context.Context, queryer catalogQueryer, expected []string) error {
	if err := requireSortedUniqueStrings("namespace.object_creator_closure", expected); err != nil {
		return err
	}
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryNamespaceCreators, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	actual := make([]string, 0, len(expected))
	for rows.Next() {
		var principal string
		if err := rows.Scan(&principal); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.object_creator_closure.scan", projector.major, "effective schema CREATE principal scan failed")
		}
		if err := budget.add("namespace.object_creator_closure", principal); err != nil {
			return err
		}
		if uint64(len(actual)) >= projectionMaxPrincipals {
			return pgProjectionFailure(CodeProjectionLimitExceeded, "namespace.object_creator_closure", projector.major, "effective schema CREATE principal limit exceeded")
		}
		actual = append(actual, principal)
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "namespace.object_creator_closure.iteration", projector.major, "effective schema CREATE principal iteration failed")
	}
	if err := requireSortedUniqueStrings("namespace.object_creator_closure.actual", actual); err != nil {
		return err
	}
	if len(actual) != len(expected) {
		return pgProjectionFailure(CodeAuthorityDrift, "namespace.object_creator_closure", projector.major, "effective schema CREATE principal closure differs from the signed scope")
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return pgProjectionFailure(CodeAuthorityDrift, "namespace.object_creator_closure", projector.major, "effective schema CREATE principal closure differs from the signed scope")
		}
	}
	return nil
}

type defaultACLGroup struct {
	rowIdentity string
	owner       string
	schema      *string
	objectKind  string
	acl         *aclAccumulator
}

func (projector *PGProjector) readDefaultACL(ctx context.Context, queryer catalogQueryer, defaultACLOwners, objectCreatorClosure []string) ([]DefaultACLProjection, error) {
	if err := requireSortedUniqueStrings("default-acl.owners", defaultACLOwners); err != nil {
		return nil, err
	}
	if err := requireSortedUniqueStrings("default-acl.object_creator_closure", objectCreatorClosure); err != nil {
		return nil, err
	}
	creatorSet := make(map[string]struct{}, len(objectCreatorClosure))
	for _, creator := range objectCreatorClosure {
		creatorSet[creator] = struct{}{}
	}
	allowedOwners := make(map[string]struct{}, len(defaultACLOwners))
	for _, owner := range defaultACLOwners {
		if _, ok := creatorSet[owner]; !ok {
			return nil, pgProjectionFailure(CodeAuthorityDrift, "default-acl.owner_closure", projector.major, "signed default ACL owner is outside the object creator closure")
		}
		allowedOwners[owner] = struct{}{}
	}
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryDefaultACLs, projectionTargetSchema, defaultACLOwners)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	groups := make(map[string]*defaultACLGroup)
	var logicalACLEntries uint64
	for rows.Next() {
		var rowIdentity, owner, rawKind string
		var schema, grantor, grantee, privilege *string
		var grantable *bool
		var effectiveCreate bool
		if err := rows.Scan(&rowIdentity, &owner, &schema, &rawKind, &grantor, &grantee, &privilege, &grantable, &effectiveCreate); err != nil {
			return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "default-acl.scan", projector.major, "default ACL projection scan failed")
		}
		if err := budget.add("default-acl", rowIdentity, owner, nullableString(schema), rawKind, nullableString(grantor), nullableString(grantee), nullableString(privilege)); err != nil {
			return nil, err
		}
		objectKind, err := mapDefaultACLObjectKind(rawKind)
		if err != nil {
			return nil, err
		}
		if schema != nil && *schema != projectionTargetSchema {
			return nil, pgProjectionFailure(CodeProjectionInvalidScope, "default-acl.schema", projector.major, "default ACL schema is outside the target namespace")
		}
		if objectKind == "schema" && schema != nil {
			return nil, pgProjectionFailure(CodeProjectionInvalidScope, "default-acl.schema", projector.major, "schema default ACL is only valid at global scope")
		}
		if _, allowed := allowedOwners[owner]; !allowed {
			if effectiveCreate {
				return nil, pgProjectionFailure(CodeAuthorityDrift, "default-acl.owner", projector.major, "unbound default ACL owner has effective CREATE on the target schema")
			}
			if schema != nil {
				return nil, pgProjectionFailure(CodeProjectionUnknownObject, "default-acl.owner", projector.major, "unbound owner has a target-schema default ACL row")
			}
			continue
		}
		key := owner + "\x00" + nullableString(schema) + "\x00" + objectKind
		group := groups[key]
		if group == nil {
			if uint64(len(groups)) >= projectionMaxDefaultACLEntries {
				return nil, pgProjectionFailure(CodeProjectionLimitExceeded, "default-acl", projector.major, "default ACL row limit exceeded")
			}
			group = &defaultACLGroup{rowIdentity: rowIdentity, owner: owner, schema: cloneStringPointer(schema), objectKind: objectKind, acl: newACLAccumulator(projector.major, false, "default_acl_catalog")}
			groups[key] = group
		} else if group.rowIdentity != rowIdentity {
			return nil, pgProjectionFailure(CodeProjectionInvalidScope, "default-acl", projector.major, "default ACL logical scope is duplicate")
		}
		if grantor == nil && grantee == nil && privilege == nil && grantable == nil {
			continue
		}
		if grantor == nil || grantee == nil {
			return nil, pgProjectionFailure(CodeProjectionUnknownObject, "default-acl.principal", projector.major, "default ACL references an unknown principal")
		}
		if privilege == nil || grantable == nil {
			return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "default-acl", projector.major, "default ACL expanded row is sparse")
		}
		beforeEntries := group.acl.count
		if err := group.acl.add("default-acl.entries", *grantor, *grantee, *privilege, *grantable, defaultACLPrivileges[objectKind]); err != nil {
			return nil, err
		}
		if group.acl.count > beforeEntries {
			logicalACLEntries++
			if logicalACLEntries > projectionMaxDefaultACLEntries {
				return nil, pgProjectionFailure(CodeProjectionLimitExceeded, "default-acl.entries", projector.major, "default ACL entry limit exceeded")
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "default-acl.iteration", projector.major, "default ACL projection iteration failed")
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]DefaultACLProjection, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		result = append(result, DefaultACLProjection{Owner: group.owner, Schema: cloneStringPointer(group.schema), ObjectKind: group.objectKind, ACL: group.acl.projection()})
	}
	return result, nil
}

func computeSchemaExplicitACLDigest(schema SchemaProjection) (Digest, error) {
	if schema.Name != projectionTargetSchema {
		return "", pgProjectionFailure(CodeProjectionInvalidScope, "subdigest.schema_explicit", 0, "schema explicit ACL digest target is invalid")
	}
	if err := schema.ExplicitACL.Validate(); err != nil {
		return "", pgProjectionFailure(CodeCatalogDrift, "subdigest.schema_explicit", 0, "schema explicit ACL projection is invalid")
	}
	return digestFlatDomain(schemaExplicitACLDigestDomain, struct {
		Schema      string           `json:"schema"`
		ExplicitACL ACLSetProjection `json:"explicit_acl"`
	}{Schema: schema.Name, ExplicitACL: schema.ExplicitACL}, "")
}

func computeSchemaEffectiveACLDigest(schema SchemaProjection) (Digest, error) {
	if schema.Name != projectionTargetSchema || schema.Owner == "" {
		return "", pgProjectionFailure(CodeProjectionInvalidScope, "subdigest.schema_effective", 0, "schema effective ACL digest target or owner is invalid")
	}
	if err := (ACLSetProjection{CatalogValue: "explicit", Entries: schema.EffectiveACL}).Validate(); err != nil {
		return "", pgProjectionFailure(CodeCatalogDrift, "subdigest.schema_effective", 0, "schema effective ACL projection is invalid")
	}
	return digestFlatDomain(schemaEffectiveACLDigestDomain, struct {
		Schema       string          `json:"schema"`
		Owner        string          `json:"owner"`
		EffectiveACL []ACLProjection `json:"effective_acl"`
	}{Schema: schema.Name, Owner: schema.Owner, EffectiveACL: schema.EffectiveACL}, "")
}

func computeDefaultACLDigest(rows []DefaultACLProjection) (Digest, error) {
	if rows == nil {
		return "", pgProjectionFailure(CodeCatalogDrift, "subdigest.default_acl", 0, "default ACL rows must be an explicit array")
	}
	if err := validateProjectedDefaultACLRows(rows); err != nil {
		return "", err
	}
	return digestFlatDomain(defaultACLDigestDomain, struct {
		TargetSchema string                 `json:"target_schema"`
		Rows         []DefaultACLProjection `json:"rows"`
	}{TargetSchema: projectionTargetSchema, Rows: rows}, "")
}

func validateProjectedDefaultACLRows(rows []DefaultACLProjection) error {
	if uint64(len(rows)) > projectionMaxDefaultACLEntries {
		return pgProjectionFailure(CodeProjectionLimitExceeded, "default-acl.rows", 0, "default ACL row limit exceeded")
	}
	previous := ""
	for _, row := range rows {
		if row.Owner == "" || row.Schema != nil && *row.Schema != projectionTargetSchema {
			return pgProjectionFailure(CodeProjectionInvalidScope, "default-acl.rows", 0, "default ACL owner or schema is invalid")
		}
		allowed, ok := defaultACLPrivileges[row.ObjectKind]
		if !ok || row.ObjectKind == "schema" && row.Schema != nil || row.ACL.CatalogValue != "explicit" {
			return pgProjectionFailure(CodeProjectionInvalidScope, "default-acl.rows", 0, "default ACL object kind or catalog value is invalid")
		}
		if err := row.ACL.Validate(); err != nil {
			return pgProjectionFailure(CodeCatalogDrift, "default-acl.rows.acl", 0, "default ACL entry projection is invalid")
		}
		for _, entry := range row.ACL.Entries {
			if entry.Origin != "default_acl_catalog" {
				return pgProjectionFailure(CodeCatalogDrift, "default-acl.rows.origin", 0, "default ACL origin is invalid")
			}
			for _, privilege := range entry.Privileges {
				if _, allowedPrivilege := allowed[privilege]; !allowedPrivilege {
					return pgProjectionFailure(CodeProjectionUnknownObject, "default-acl.rows.privilege", 0, "default ACL privilege is outside the object-kind profile")
				}
			}
		}
		key := row.Owner + "\x00" + nullableString(row.Schema) + "\x00" + row.ObjectKind
		if previous != "" && previous >= key {
			return pgProjectionFailure(CodeProjectionInvalidScope, "default-acl.rows.order", 0, "default ACL rows are duplicate or unsorted")
		}
		previous = key
	}
	return nil
}
