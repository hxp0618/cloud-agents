package migration

import (
	"context"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// pgCatalogStructure is an ordinary, package-private structural result. It is
// deliberately not a verified contract or permit. Expression slots remain
// unresolved until the separate A2.1b expression normalizer consumes them.
type pgCatalogStructure struct {
	absent      bool
	body        CatalogProjectionBody
	expressions []pgCatalogExpressionSlot
}

type pgCatalogExpressionSlot struct {
	Object  ObjectIdentityProjection `json:"object"`
	Field   string                   `json:"field"`
	Ordinal uint32                   `json:"ordinal"`
}

type pgCatalogAddress struct {
	class string
	id    string
	sub   uint32
}

func (address pgCatalogAddress) valid() bool {
	return address.class != "" && address.id != ""
}

func (address pgCatalogAddress) key() string {
	return address.class + "\x00" + address.id + "\x00" + strconv.FormatUint(uint64(address.sub), 10)
}

type pgCatalogRelationBuilder struct {
	address     pgCatalogAddress
	projection  RelationProjection
	acl         *aclAccumulator
	columns     map[uint32]*pgCatalogColumnBuilder
	constraints map[string]*pgCatalogConstraintBuilder
	indexes     map[string]*pgCatalogIndexBuilder
	policies    map[string]PolicyProjection
	triggers    map[string]TriggerProjection
}

type pgCatalogColumnBuilder struct {
	address    pgCatalogAddress
	projection ColumnProjection
	acl        *aclAccumulator
}

type pgCatalogConstraintBuilder struct {
	address    pgCatalogAddress
	identity   ObjectIdentityProjection
	projection ConstraintProjection
	indexID    string
}

type pgCatalogIndexBuilder struct {
	address       pgCatalogAddress
	identity      ObjectIdentityProjection
	projection    IndexProjection
	keyCount      uint32
	physicalCount uint32
	terms         map[uint32]IndexTermProjection
	includes      map[uint32]string
}

type pgCatalogFunctionBuilder struct {
	address    pgCatalogAddress
	projection FunctionProjection
	acl        *aclAccumulator
	arguments  map[uint32]FunctionArgumentProjection
}

type pgCatalogBuilder struct {
	major         uint16
	scope         ProjectionScope
	body          CatalogProjectionBody
	sightings     []pgNamespaceObject
	declared      map[string]ObjectIdentityProjection
	relations     map[string]*pgCatalogRelationBuilder
	constraints   map[string]*pgCatalogConstraintBuilder
	functions     map[string]*pgCatalogFunctionBuilder
	addresses     map[string]ObjectIdentityProjection
	dependencies  map[string]DependencyProjection
	internalNames map[string]struct{}
	denied        map[string]DeniedObjectProjection
	expressions   []pgCatalogExpressionSlot
}

func (projector *PGProjector) readCatalogStructure(ctx context.Context, snapshot ProjectionSnapshot, scope ProjectionScope, defaultACLOwners, objectCreatorClosure []string) (pgCatalogStructure, error) {
	namespace, sightings, err := projector.readCatalogNamespace(ctx, snapshot, scope, defaultACLOwners, objectCreatorClosure)
	if err != nil {
		return pgCatalogStructure{}, err
	}
	if namespace.Absent {
		return pgCatalogStructure{absent: true, body: CatalogProjectionBody{}, expressions: []pgCatalogExpressionSlot{}}, nil
	}
	builder, err := newPGCatalogBuilder(projector.major, scope, namespace.Body, sightings)
	if err != nil {
		return pgCatalogStructure{}, err
	}
	readers := []func(context.Context, catalogQueryer, *pgCatalogBuilder) error{
		projector.readCatalogRelations,
		projector.readCatalogColumns,
		projector.readCatalogConstraints,
		projector.readCatalogIndexes,
		projector.readCatalogIndexTerms,
		projector.readCatalogPolicies,
		projector.readCatalogTriggers,
		projector.readCatalogFunctions,
		projector.readCatalogFunctionArguments,
		projector.readCatalogInternalObjects,
		projector.readCatalogDependencies,
	}
	for _, read := range readers {
		if err := read(ctx, snapshot, builder); err != nil {
			return pgCatalogStructure{}, err
		}
	}
	return builder.finish()
}

func newPGCatalogBuilder(major uint16, scope ProjectionScope, body CatalogProjectionBody, sightings []pgNamespaceObject) (*pgCatalogBuilder, error) {
	declared := make(map[string]ObjectIdentityProjection, len(scope.DeclaredObjects))
	for _, identity := range scope.DeclaredObjects {
		key, err := canonicalContractKey(identity)
		if err != nil {
			return nil, pgProjectionFailure(CodeProjectionInvalidScope, "catalog.declared_objects", major, "declared object identity is invalid")
		}
		declared[key] = cloneProjectionValue(identity)
	}
	return &pgCatalogBuilder{
		major: major, scope: cloneProjectionValue(scope), body: cloneProjectionValue(body), sightings: append([]pgNamespaceObject(nil), sightings...),
		declared: declared, relations: make(map[string]*pgCatalogRelationBuilder), constraints: make(map[string]*pgCatalogConstraintBuilder), functions: make(map[string]*pgCatalogFunctionBuilder),
		addresses: make(map[string]ObjectIdentityProjection), dependencies: make(map[string]DependencyProjection), internalNames: make(map[string]struct{}),
		denied: make(map[string]DeniedObjectProjection), expressions: make([]pgCatalogExpressionSlot, 0),
	}, nil
}

func (builder *pgCatalogBuilder) declaredContains(identity ObjectIdentityProjection) bool {
	key, err := canonicalContractKey(identity)
	if err != nil {
		return false
	}
	_, ok := builder.declared[key]
	return ok
}

func (builder *pgCatalogBuilder) registerAddress(address pgCatalogAddress, identity ObjectIdentityProjection) error {
	if !address.valid() {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.address", builder.major, "catalog address is incomplete")
	}
	if err := identity.Validate(); err != nil {
		return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.address.identity", builder.major, "catalog address has an invalid logical identity")
	}
	key := address.key()
	if existing, ok := builder.addresses[key]; ok {
		existingKey, existingErr := canonicalContractKey(existing)
		identityKey, identityErr := canonicalContractKey(identity)
		if existingErr != nil || identityErr != nil || existingKey != identityKey {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.address", builder.major, "catalog address maps to multiple logical identities")
		}
		return nil
	}
	builder.addresses[key] = cloneProjectionValue(identity)
	return nil
}

func (builder *pgCatalogBuilder) addDenied(denied DeniedObjectProjection) error {
	if err := denied.Object.Validate(); err != nil {
		return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.denied", builder.major, "denied object identity is invalid")
	}
	key, err := canonicalContractKey(denied.Object)
	if err != nil {
		return err
	}
	if existing, duplicate := builder.denied[key]; duplicate {
		existingKey, _ := canonicalContractKey(existing.Object)
		if existingKey != key {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.denied", builder.major, "denied object has conflicting identities")
		}
		if deniedReasonRank(denied.ReasonCode) > deniedReasonRank(existing.ReasonCode) {
			builder.denied[key] = cloneProjectionValue(denied)
		}
		return nil
	}
	builder.denied[key] = cloneProjectionValue(denied)
	return nil
}

func deniedReasonRank(reason string) int {
	switch reason {
	case "unbound_internal_object":
		return 4
	case "unsupported_object_kind":
		return 3
	case "dependency_outside_closure":
		return 2
	case "undeclared_object":
		return 1
	default:
		return 0
	}
}

func (builder *pgCatalogBuilder) addExpression(object ObjectIdentityProjection, field string, ordinal uint32) error {
	if err := object.Validate(); err != nil || field == "" {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expression_slot", builder.major, "expression slot identity is invalid")
	}
	builder.expressions = append(builder.expressions, pgCatalogExpressionSlot{Object: cloneProjectionValue(object), Field: field, Ordinal: ordinal})
	if uint64(len(builder.expressions)) > projectionMaxExpressionNodes {
		return pgProjectionFailure(CodeProjectionLimitExceeded, "catalog.expression_slots", builder.major, "expression slot limit exceeded")
	}
	return nil
}

func (projector *PGProjector) readCatalogRelations(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogRelations, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var relationID, name, rawKind, rawPersistence, owner, rawReplica string
		var accessMethod, grantor, grantee, privilege *string
		var aclIsNull, rlsEnabled, rlsForced bool
		var grantable *bool
		var reloptions []string
		if err := rows.Scan(&relationID, &name, &rawKind, &rawPersistence, &accessMethod, &owner, &aclIsNull, &grantor, &grantee, &privilege, &grantable, &reloptions, &rawReplica, &rlsEnabled, &rlsForced); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.relations.scan", projector.major, "relation projection scan failed")
		}
		if err := budget.add("catalog.relations", relationID, name, rawKind, rawPersistence, nullableString(accessMethod), owner, nullableString(grantor), nullableString(grantee), nullableString(privilege), strings.Join(reloptions, "\x00"), rawReplica); err != nil {
			return err
		}
		if name == "" || owner == "" {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.relations", projector.major, "relation row is sparse")
		}
		relation := builder.relations[relationID]
		if relation == nil {
			relkind, err := normalizePGRelationKind(rawKind)
			if err != nil {
				return err
			}
			persistence, err := normalizePGPersistence(rawPersistence)
			if err != nil {
				return err
			}
			replica, err := normalizePGReplicaIdentity(rawReplica)
			if err != nil {
				return err
			}
			sort.Strings(reloptions)
			identity := ObjectIdentityProjection{Relation: &RelationObjectIdentity{Kind: "relation", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: name}}}
			relation = &pgCatalogRelationBuilder{
				address: pgCatalogAddress{class: "pg_class", id: relationID},
				projection: RelationProjection{
					Identity: TypeIdentity{Schema: projectionTargetSchema, Name: name}, Relkind: relkind, Persistence: persistence,
					AccessMethod: cloneStringPointer(accessMethod), Owner: owner, Reloptions: cloneStringsExplicit(reloptions),
					ReplicaIdentity: replica, RLSEnabled: rlsEnabled, RLSForced: rlsForced,
					Columns: []ColumnProjection{}, Constraints: []ConstraintProjection{}, Indexes: []IndexProjection{}, Policies: []PolicyProjection{}, Triggers: []TriggerProjection{},
				},
				acl: newACLAccumulator(projector.major, aclIsNull, "catalog_explicit"), columns: make(map[uint32]*pgCatalogColumnBuilder),
				constraints: make(map[string]*pgCatalogConstraintBuilder), indexes: make(map[string]*pgCatalogIndexBuilder), policies: make(map[string]PolicyProjection), triggers: make(map[string]TriggerProjection),
			}
			builder.relations[relationID] = relation
			if err := builder.registerAddress(relation.address, identity); err != nil {
				return err
			}
			if !builder.declaredContains(identity) {
				ownerCopy := owner
				if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "undeclared_object"}); err != nil {
					return err
				}
			}
		} else if relation.projection.Identity.Name != name || relation.projection.Owner != owner || relation.projection.Relkind != mustNormalizePGRelationKind(rawKind) || relation.projection.Persistence != mustNormalizePGPersistence(rawPersistence) || relation.projection.ReplicaIdentity != mustNormalizePGReplicaIdentity(rawReplica) || !equalStringPointers(relation.projection.AccessMethod, accessMethod) || relation.projection.RLSEnabled != rlsEnabled || relation.projection.RLSForced != rlsForced || !equalStrings(relation.projection.Reloptions, sortedCopy(reloptions)) {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.relations", projector.major, "relation identity row is inconsistent across ACL expansion")
		}
		skipACL, err := normalizeVersionedRelationACL(projector.major, owner, grantor, grantee, privilege, grantable)
		if err != nil {
			return err
		}
		if skipACL {
			continue
		}
		if err := addExpandedACLRow(relation.acl, "catalog.relations.acl", grantor, grantee, privilege, grantable, relationPrivileges(relation.projection.Relkind)); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.relations.iteration", projector.major, "relation projection iteration failed")
	}
	if uint64(len(builder.relations)) > projectionMaxCatalogObjects {
		return pgProjectionFailure(CodeProjectionLimitExceeded, "catalog.relations", projector.major, "relation object limit exceeded")
	}
	return nil
}

func (projector *PGProjector) readCatalogColumns(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogColumns, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var relationID, relationName, attnumText, name, typeID, typeSchema, typeName, typmod string
		var collationID, collationSchema, collationName *string
		var notNull, hasDefault, aclIsNull bool
		var rawIdentity, rawGenerated, rawStorage, rawCompression string
		var grantor, grantee, privilege *string
		var grantable *bool
		if err := rows.Scan(&relationID, &relationName, &attnumText, &name, &typeID, &typeSchema, &typeName, &typmod, &collationID, &collationSchema, &collationName, &notNull, &rawIdentity, &rawGenerated, &hasDefault, &rawStorage, &rawCompression, &aclIsNull, &grantor, &grantee, &privilege, &grantable); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.columns.scan", projector.major, "column projection scan failed")
		}
		if err := budget.add("catalog.columns", relationID, relationName, attnumText, name, typeID, typeSchema, typeName, typmod, nullableString(collationID), nullableString(collationSchema), nullableString(collationName), rawIdentity, rawGenerated, rawStorage, rawCompression, nullableString(grantor), nullableString(grantee), nullableString(privilege)); err != nil {
			return err
		}
		relation := builder.relations[relationID]
		if relation == nil || relation.projection.Identity.Name != relationName {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.columns.relation", projector.major, "column references an unknown relation")
		}
		attnum, err := parsePGUint32(attnumText, "catalog.columns.attnum", projector.major)
		if err != nil || attnum == 0 {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.columns.attnum", projector.major, "column ordinal is invalid")
		}
		column := relation.columns[attnum]
		if column == nil {
			identityMode, err := normalizePGColumnIdentity(rawIdentity)
			if err != nil {
				return err
			}
			generatedMode, err := normalizePGGenerated(rawGenerated)
			if err != nil {
				return err
			}
			storageMode, err := normalizePGStorage(rawStorage)
			if err != nil {
				return err
			}
			compressionMode, err := normalizePGCompression(rawCompression)
			if err != nil {
				return err
			}
			typeIdentity := TypeIdentity{Schema: typeSchema, Name: typeName}
			if err := typeIdentity.Validate(); err != nil {
				return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.columns.type", projector.major, "column type identity is invalid")
			}
			var collation *TypeIdentity
			if collationID != nil || collationSchema != nil || collationName != nil {
				if collationID == nil || collationSchema == nil || collationName == nil {
					return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.columns.collation", projector.major, "column collation row is sparse")
				}
				collation = &TypeIdentity{Schema: *collationSchema, Name: *collationName}
				collationIdentity := ObjectIdentityProjection{Collation: &CollationObjectIdentity{Kind: "collation", Identity: *collation}}
				if err := builder.registerAddress(pgCatalogAddress{class: "pg_collation", id: *collationID}, collationIdentity); err != nil {
					return err
				}
			}
			column = &pgCatalogColumnBuilder{
				address: pgCatalogAddress{class: "pg_class", id: relationID, sub: attnum},
				projection: ColumnProjection{
					Attnum: attnum, Name: name, Type: typeIdentity, TypmodInt32Decimal: typmod, Collation: collation,
					Nullable: !notNull, Identity: identityMode, Generated: generatedMode, Storage: storageMode, Compression: compressionMode,
				},
				acl: newACLAccumulator(projector.major, aclIsNull, "catalog_explicit"),
			}
			relation.columns[attnum] = column
			columnIdentity := ObjectIdentityProjection{Column: &ColumnObjectIdentity{Kind: "column", Relation: relation.projection.Identity, Name: name}}
			if err := builder.registerAddress(column.address, columnIdentity); err != nil {
				return err
			}
			typeObject := ObjectIdentityProjection{Type: &TypeObjectIdentity{Kind: "type", Identity: typeIdentity}}
			if err := builder.registerAddress(pgCatalogAddress{class: "pg_type", id: typeID}, typeObject); err != nil {
				return err
			}
			if hasDefault || generatedMode != "none" {
				if err := builder.addExpression(columnIdentity, "column_default", 0); err != nil {
					return err
				}
			}
		} else if column.projection.Name != name || column.projection.Type != (TypeIdentity{Schema: typeSchema, Name: typeName}) || column.projection.TypmodInt32Decimal != typmod || column.projection.Nullable == notNull {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.columns", projector.major, "column identity row is inconsistent across ACL expansion")
		}
		if err := addExpandedACLRow(column.acl, "catalog.columns.acl", grantor, grantee, privilege, grantable, columnPrivileges); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.columns.iteration", projector.major, "column projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogConstraints(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogConstraints, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var constraintID, relationID, relationName, name, rawType string
		var columns, referencedColumns []string
		var referencedRelationID, referencedSchema, referencedName *string
		var rawMatch, rawUpdate, rawDelete string
		var deferrable, deferred, validated, hasExpression bool
		var indexID *string
		if err := rows.Scan(&constraintID, &relationID, &relationName, &name, &rawType, &columns, &referencedRelationID, &referencedSchema, &referencedName, &referencedColumns, &rawMatch, &rawUpdate, &rawDelete, &deferrable, &deferred, &validated, &hasExpression, &indexID); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.constraints.scan", projector.major, "constraint projection scan failed")
		}
		if err := budget.add("catalog.constraints", constraintID, relationID, relationName, name, rawType, strings.Join(columns, "\x00"), nullableString(referencedRelationID), nullableString(referencedSchema), nullableString(referencedName), strings.Join(referencedColumns, "\x00"), rawMatch, rawUpdate, rawDelete, nullableString(indexID)); err != nil {
			return err
		}
		relation := builder.relations[relationID]
		if relation == nil || relation.projection.Identity.Name != relationName || constraintID == "" || name == "" {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.constraints.relation", projector.major, "constraint references an unknown relation")
		}
		if _, duplicate := relation.constraints[constraintID]; duplicate {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.constraints", projector.major, "constraint catalog identity is duplicate")
		}
		constraintType, err := normalizePGConstraintType(rawType)
		if err != nil {
			return err
		}
		match, update, deleteAction, err := normalizePGConstraintActions(constraintType, rawMatch, rawUpdate, rawDelete)
		if err != nil {
			return err
		}
		identity := ObjectIdentityProjection{Constraint: &ConstraintObjectIdentity{Kind: "constraint", Relation: relation.projection.Identity, Name: name}}
		var referenced *TypeIdentity
		if referencedRelationID != nil || referencedSchema != nil || referencedName != nil {
			if referencedRelationID == nil || referencedSchema == nil || referencedName == nil {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.constraints.reference", projector.major, "constraint reference row is sparse")
			}
			referenced = &TypeIdentity{Schema: *referencedSchema, Name: *referencedName}
			if err := referenced.Validate(); err != nil {
				return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.constraints.reference", projector.major, "constraint referenced relation identity is invalid")
			}
			referencedIdentity := ObjectIdentityProjection{Relation: &RelationObjectIdentity{Kind: "relation", Identity: *referenced}}
			if err := builder.registerAddress(pgCatalogAddress{class: "pg_class", id: *referencedRelationID}, referencedIdentity); err != nil {
				return err
			}
		}
		constraint := &pgCatalogConstraintBuilder{
			address: pgCatalogAddress{class: "pg_constraint", id: constraintID}, identity: identity,
			projection: ConstraintProjection{
				Name: name, Type: constraintType, Columns: cloneStringsExplicit(columns), ReferencedRelation: referenced,
				ReferencedColumns: cloneStringsExplicit(referencedColumns), Match: match, Update: update, Delete: deleteAction,
				Deferrable: deferrable, Deferred: deferred, Validated: validated,
			},
		}
		if indexID != nil {
			constraint.indexID = *indexID
		}
		relation.constraints[constraintID] = constraint
		if _, duplicate := builder.constraints[constraintID]; duplicate {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.constraints", projector.major, "constraint catalog address is duplicate")
		}
		builder.constraints[constraintID] = constraint
		if err := builder.registerAddress(constraint.address, identity); err != nil {
			return err
		}
		if hasExpression {
			if err := builder.addExpression(identity, "constraint_expression", 0); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.constraints.iteration", projector.major, "constraint projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogIndexes(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogIndexes, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var relationID, relationName, indexID, name, accessMethod string
		var unique, primary, valid, ready, live, immediate, clustered, checkXmin, nullsNotDistinct, exclusion, replicaIdentity, hasPredicate bool
		var constraintID *string
		var keyCountText, physicalCountText string
		if err := rows.Scan(&relationID, &relationName, &indexID, &name, &accessMethod, &unique, &primary, &valid, &ready, &live, &immediate, &clustered, &checkXmin, &nullsNotDistinct, &exclusion, &replicaIdentity, &hasPredicate, &constraintID, &keyCountText, &physicalCountText); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.indexes.scan", projector.major, "index projection scan failed")
		}
		if err := budget.add("catalog.indexes", relationID, relationName, indexID, name, accessMethod, nullableString(constraintID), keyCountText, physicalCountText); err != nil {
			return err
		}
		relation := builder.relations[relationID]
		if relation == nil || relation.projection.Identity.Name != relationName || indexID == "" || name == "" || accessMethod == "" {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.indexes.relation", projector.major, "index references an unknown relation")
		}
		if _, duplicate := relation.indexes[indexID]; duplicate {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.indexes", projector.major, "index catalog identity is duplicate")
		}
		keyCount, err := parsePGUint32(keyCountText, "catalog.indexes.key_count", projector.major)
		if err != nil {
			return err
		}
		physicalCount, err := parsePGUint32(physicalCountText, "catalog.indexes.physical_count", projector.major)
		if err != nil || keyCount > physicalCount {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.indexes.physical_count", projector.major, "index key and physical attribute counts are invalid")
		}
		normalIdentity := ObjectIdentityProjection{Index: &IndexObjectIdentity{Kind: "index", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: name}, Relation: relation.projection.Identity}}
		identity := normalIdentity
		if !builder.declaredContains(normalIdentity) {
			var owningConstraint *pgCatalogConstraintBuilder
			if constraintID != nil {
				owningConstraint = relation.constraints[*constraintID]
			}
			if owningConstraint == nil {
				ownerCopy := relation.projection.Owner
				if err := builder.addDenied(DeniedObjectProjection{Object: normalIdentity, Owner: &ownerCopy, ReasonCode: "undeclared_object"}); err != nil {
					return err
				}
			} else {
				identity = ObjectIdentityProjection{Internal: &InternalObjectIdentity{Kind: "internal", SemanticKind: "constraint_index", OwningObject: owningConstraint.identity}}
			}
		}
		index := &pgCatalogIndexBuilder{
			address: pgCatalogAddress{class: "pg_class", id: indexID}, identity: identity,
			projection: IndexProjection{
				Name: name, AccessMethod: accessMethod, Terms: []IndexTermProjection{}, Includes: []string{}, Unique: unique, Primary: primary,
				Valid: valid, Ready: ready, Live: live, Immediate: immediate, Clustered: clustered, CheckXmin: checkXmin,
				NullsNotDistinct: nullsNotDistinct, Exclusion: exclusion, ReplicaIdentity: replicaIdentity,
			},
			keyCount: keyCount, physicalCount: physicalCount, terms: make(map[uint32]IndexTermProjection), includes: make(map[uint32]string),
		}
		relation.indexes[indexID] = index
		if err := builder.registerAddress(index.address, identity); err != nil {
			return err
		}
		if hasPredicate {
			if err := builder.addExpression(identity, "index_predicate", 0); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.indexes.iteration", projector.major, "index projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogIndexTerms(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogIndexTerms, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var relationID, relationName, indexID, indexName, ordinalText string
		var included, hasExpression bool
		var column, opclassID, opclassSchema, opclassName, opclassMethod *string
		var opclassOptions []string
		var collationID, collationSchema, collationName *string
		var order, nulls string
		var operatorID, operatorSchema, operatorName, leftTypeSchema, leftTypeName, rightTypeSchema, rightTypeName *string
		if err := rows.Scan(&relationID, &relationName, &indexID, &indexName, &ordinalText, &included, &column, &hasExpression, &opclassID, &opclassSchema, &opclassName, &opclassMethod, &opclassOptions, &collationID, &collationSchema, &collationName, &order, &nulls, &operatorID, &operatorSchema, &operatorName, &leftTypeSchema, &leftTypeName, &rightTypeSchema, &rightTypeName); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.scan", projector.major, "index term projection scan failed")
		}
		if err := budget.add("catalog.index_terms", relationID, relationName, indexID, indexName, ordinalText, nullableString(column), nullableString(opclassID), nullableString(opclassSchema), nullableString(opclassName), nullableString(opclassMethod), strings.Join(opclassOptions, "\x00"), nullableString(collationID), nullableString(collationSchema), nullableString(collationName), order, nulls, nullableString(operatorID), nullableString(operatorSchema), nullableString(operatorName), nullableString(leftTypeSchema), nullableString(leftTypeName), nullableString(rightTypeSchema), nullableString(rightTypeName)); err != nil {
			return err
		}
		relation := builder.relations[relationID]
		if relation == nil || relation.projection.Identity.Name != relationName {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.index_terms.relation", projector.major, "index term references an unknown relation")
		}
		index := relation.indexes[indexID]
		if index == nil || index.projection.Name != indexName {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.index_terms.index", projector.major, "index term references an unknown index")
		}
		ordinal, err := parsePGUint32(ordinalText, "catalog.index_terms.ordinal", projector.major)
		if err != nil || ordinal == 0 || ordinal > index.physicalCount {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.ordinal", projector.major, "index term ordinal is invalid")
		}
		if included {
			if ordinal <= index.keyCount || column == nil || *column == "" || hasExpression || opclassID != nil || collationID != nil || operatorID != nil {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.include", projector.major, "included index attribute row is invalid")
			}
			if _, duplicate := index.includes[ordinal]; duplicate {
				return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.index_terms.include", projector.major, "included index attribute is duplicate")
			}
			index.includes[ordinal] = *column
			continue
		}
		if ordinal > index.keyCount || opclassID == nil || opclassSchema == nil || opclassName == nil || opclassMethod == nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.key", projector.major, "index key term is sparse")
		}
		if _, duplicate := index.terms[ordinal]; duplicate {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.index_terms.key", projector.major, "index key term is duplicate")
		}
		opclass := TypeIdentity{Schema: *opclassSchema, Name: *opclassName}
		opclassIdentity := ObjectIdentityProjection{Opclass: &OpclassObjectIdentity{Kind: "opclass", Identity: opclass, AccessMethod: *opclassMethod}}
		if err := builder.registerAddress(pgCatalogAddress{class: "pg_opclass", id: *opclassID}, opclassIdentity); err != nil {
			return err
		}
		term := IndexTermProjection{Ordinal: ordinal, Opclass: &opclass, OpclassOptions: sortedCopy(opclassOptions), Order: order, Nulls: nulls}
		if hasExpression {
			if column != nil {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.expression", projector.major, "expression index term also carries a column")
			}
			term.TermKind = "expression"
			if err := builder.addExpression(index.identity, "index_term", ordinal); err != nil {
				return err
			}
		} else {
			if column == nil || *column == "" {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.column", projector.major, "column index term is missing its column")
			}
			term.TermKind = "column"
			term.Column = cloneStringPointer(column)
		}
		if collationID != nil || collationSchema != nil || collationName != nil {
			if collationID == nil || collationSchema == nil || collationName == nil {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.collation", projector.major, "index term collation row is sparse")
			}
			collation := TypeIdentity{Schema: *collationSchema, Name: *collationName}
			term.Collation = &collation
			if err := builder.registerAddress(pgCatalogAddress{class: "pg_collation", id: *collationID}, ObjectIdentityProjection{Collation: &CollationObjectIdentity{Kind: "collation", Identity: collation}}); err != nil {
				return err
			}
		}
		if operatorID != nil || operatorSchema != nil || operatorName != nil || leftTypeSchema != nil || leftTypeName != nil || rightTypeSchema != nil || rightTypeName != nil {
			if operatorID == nil || operatorSchema == nil || operatorName == nil || leftTypeSchema == nil || leftTypeName == nil || rightTypeSchema == nil || rightTypeName == nil {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.operator", projector.major, "exclusion operator row is sparse")
			}
			operator := SQLIdentity{Schema: *operatorSchema, Name: *operatorName, Arguments: []TypeIdentity{{Schema: *leftTypeSchema, Name: *leftTypeName}, {Schema: *rightTypeSchema, Name: *rightTypeName}}}
			term.ExclusionOperator = &operator
			if err := builder.registerAddress(pgCatalogAddress{class: "pg_operator", id: *operatorID}, ObjectIdentityProjection{Operator: &SQLObjectIdentity{Kind: "operator", Identity: operator}}); err != nil {
				return err
			}
		}
		index.terms[ordinal] = term
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms.iteration", projector.major, "index term projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogPolicies(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogPolicies, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var relationID, relationName, policyID, name, rawCommand string
		var permissive, hasUsing, hasWithCheck bool
		var roles []string
		if err := rows.Scan(&relationID, &relationName, &policyID, &name, &permissive, &rawCommand, &roles, &hasUsing, &hasWithCheck); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.policies.scan", projector.major, "policy projection scan failed")
		}
		if err := budget.add("catalog.policies", relationID, relationName, policyID, name, rawCommand, strings.Join(roles, "\x00")); err != nil {
			return err
		}
		relation := builder.relations[relationID]
		if relation == nil || relation.projection.Identity.Name != relationName || policyID == "" || name == "" {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.policies.relation", projector.major, "policy references an unknown relation")
		}
		if _, duplicate := relation.policies[policyID]; duplicate {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.policies", projector.major, "policy catalog identity is duplicate")
		}
		command, err := normalizePGPolicyCommand(rawCommand)
		if err != nil {
			return err
		}
		sort.Strings(roles)
		if len(roles) == 0 || !strictlySorted(roles) {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.policies.roles", projector.major, "policy role closure is empty, duplicate, or invalid")
		}
		identity := ObjectIdentityProjection{Policy: &PolicyObjectIdentity{Kind: "policy", Relation: relation.projection.Identity, Name: name}}
		projection := PolicyProjection{Name: name, Permissive: permissive, Command: command, Roles: cloneStringsExplicit(roles)}
		relation.policies[policyID] = projection
		if err := builder.registerAddress(pgCatalogAddress{class: "pg_policy", id: policyID}, identity); err != nil {
			return err
		}
		if !builder.declaredContains(identity) {
			ownerCopy := relation.projection.Owner
			if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "undeclared_object"}); err != nil {
				return err
			}
		}
		if hasUsing {
			if err := builder.addExpression(identity, "policy_using", 0); err != nil {
				return err
			}
		}
		if hasWithCheck {
			if err := builder.addExpression(identity, "policy_with_check", 0); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.policies.iteration", projector.major, "policy projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogTriggers(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogTriggers, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var relationID, relationName, triggerID, name string
		var internal bool
		var constraintID, constraintName *string
		var functionID, functionSchema, functionName string
		var argumentSchemas, argumentNames []string
		var rawEnabled, typeText string
		var columns []string
		var argumentCountText, argumentsHex string
		var hasWhen bool
		if err := rows.Scan(&relationID, &relationName, &triggerID, &name, &internal, &constraintID, &constraintName, &functionID, &functionSchema, &functionName, &argumentSchemas, &argumentNames, &rawEnabled, &typeText, &columns, &argumentCountText, &argumentsHex, &hasWhen); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.scan", projector.major, "trigger projection scan failed")
		}
		if err := budget.add("catalog.triggers", relationID, relationName, triggerID, name, nullableString(constraintID), nullableString(constraintName), functionID, functionSchema, functionName, strings.Join(argumentSchemas, "\x00"), strings.Join(argumentNames, "\x00"), rawEnabled, typeText, strings.Join(columns, "\x00"), argumentCountText, argumentsHex); err != nil {
			return err
		}
		relation := builder.relations[relationID]
		if relation == nil || relation.projection.Identity.Name != relationName || triggerID == "" || name == "" {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.triggers.relation", projector.major, "trigger references an unknown relation")
		}
		if _, duplicate := relation.triggers[triggerID]; duplicate {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.triggers", projector.major, "trigger catalog identity is duplicate")
		}
		function, err := sqlIdentityFromParallel(functionSchema, functionName, argumentSchemas, argumentNames)
		if err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.function", projector.major, "trigger function identity is invalid")
		}
		functionIdentity := ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: function}}
		if err := builder.registerAddress(pgCatalogAddress{class: "pg_proc", id: functionID}, functionIdentity); err != nil {
			return err
		}
		enabled, err := normalizePGTriggerEnabled(rawEnabled)
		if err != nil {
			return err
		}
		triggerType, err := parsePGUint32(typeText, "catalog.triggers.type", projector.major)
		if err != nil || triggerType == 0 {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.type", projector.major, "trigger type is invalid")
		}
		argumentCount, err := parsePGUint32(argumentCountText, "catalog.triggers.argument_count", projector.major)
		if err != nil {
			return err
		}
		arguments, err := decodeTriggerArguments(argumentsHex, argumentCount)
		if err != nil {
			return err
		}
		var identity ObjectIdentityProjection
		var owningConstraint *ConstraintObjectIdentity
		if internal {
			if constraintID == nil || constraintName == nil {
				fallback := ObjectIdentityProjection{Trigger: &TriggerObjectIdentity{Kind: "trigger", Relation: relation.projection.Identity, Name: name}}
				ownerCopy := relation.projection.Owner
				if err := builder.addDenied(DeniedObjectProjection{Object: fallback, Owner: &ownerCopy, ReasonCode: "unbound_internal_object"}); err != nil {
					return err
				}
				identity = fallback
			} else {
				constraint := builder.constraints[*constraintID]
				if constraint == nil || constraint.projection.Name != *constraintName {
					return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.triggers.constraint", projector.major, "internal trigger owning constraint is unknown")
				}
				owningConstraint = cloneProjectionValue(constraint.identity.Constraint)
				semanticKind, err := normalizePGInternalTriggerKind(function)
				if err != nil {
					ownerCopy := relation.projection.Owner
					fallback := ObjectIdentityProjection{Trigger: &TriggerObjectIdentity{Kind: "trigger", Relation: relation.projection.Identity, Name: name, OwningConstraint: owningConstraint}}
					if deniedErr := builder.addDenied(DeniedObjectProjection{Object: fallback, Owner: &ownerCopy, ReasonCode: "unbound_internal_object"}); deniedErr != nil {
						return deniedErr
					}
					identity = fallback
				} else {
					identity = ObjectIdentityProjection{Internal: &InternalObjectIdentity{Kind: "internal", SemanticKind: semanticKind, OwningObject: constraint.identity}}
				}
			}
		} else {
			identity = ObjectIdentityProjection{Trigger: &TriggerObjectIdentity{Kind: "trigger", Relation: relation.projection.Identity, Name: name}}
			if !builder.declaredContains(identity) {
				ownerCopy := relation.projection.Owner
				if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "undeclared_object"}); err != nil {
					return err
				}
			}
		}
		trigger := TriggerProjection{
			Identity: identity, OwningRelation: relation.projection.Identity, OwningConstraint: owningConstraint,
			Function: function, Enabled: enabled, Type: triggerType, Columns: cloneStringsExplicit(columns), Arguments: arguments, Internal: internal,
		}
		relation.triggers[triggerID] = trigger
		if err := builder.registerAddress(pgCatalogAddress{class: "pg_trigger", id: triggerID}, identity); err != nil {
			return err
		}
		if hasWhen {
			if err := builder.addExpression(identity, "trigger_when", 0); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.iteration", projector.major, "trigger projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogFunctions(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogFunctions, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var functionID, name, rawKind, language string
		var identitySchemas, identityNames []string
		var variadicTypeID, variadicSchema, variadicName *string
		var returnTypeID, returnSchema, returnName, owner string
		var returnSet, aclIsNull bool
		var grantor, grantee, privilege *string
		var grantable *bool
		var securityDefiner bool
		var rawVolatility, rawParallel string
		var leakproof, strict bool
		var config []string
		var cost, rowsValue, prosrc string
		var probin *string
		if err := rows.Scan(&functionID, &name, &rawKind, &language, &identitySchemas, &identityNames, &variadicTypeID, &variadicSchema, &variadicName, &returnTypeID, &returnSchema, &returnName, &returnSet, &owner, &aclIsNull, &grantor, &grantee, &privilege, &grantable, &securityDefiner, &rawVolatility, &rawParallel, &leakproof, &strict, &config, &cost, &rowsValue, &prosrc, &probin); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.functions.scan", projector.major, "function projection scan failed")
		}
		if err := budget.add("catalog.functions", functionID, name, rawKind, language, strings.Join(identitySchemas, "\x00"), strings.Join(identityNames, "\x00"), nullableString(variadicTypeID), nullableString(variadicSchema), nullableString(variadicName), returnTypeID, returnSchema, returnName, owner, nullableString(grantor), nullableString(grantee), nullableString(privilege), rawVolatility, rawParallel, strings.Join(config, "\x00"), cost, rowsValue, prosrc, nullableString(probin)); err != nil {
			return err
		}
		function := builder.functions[functionID]
		if function == nil {
			identity, err := sqlIdentityFromParallel(projectionTargetSchema, name, identitySchemas, identityNames)
			if err != nil {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.functions.identity", projector.major, "function identity arguments are inconsistent")
			}
			kind, err := normalizePGFunctionKind(rawKind)
			if err != nil {
				return err
			}
			volatility, err := normalizePGVolatility(rawVolatility)
			if err != nil {
				return err
			}
			parallel, err := normalizePGParallel(rawParallel)
			if err != nil {
				return err
			}
			cost, err = CanonicalExactNumeric(cost)
			if err != nil {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.functions.cost", projector.major, "function cost is not canonical numeric")
			}
			rowsValue, err = CanonicalExactNumeric(rowsValue)
			if err != nil {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.functions.rows", projector.major, "function rows is not canonical numeric")
			}
			sort.Strings(config)
			var variadic *TypeIdentity
			if variadicTypeID != nil || variadicSchema != nil || variadicName != nil {
				if variadicTypeID == nil || variadicSchema == nil || variadicName == nil {
					return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.functions.variadic", projector.major, "function variadic type row is sparse")
				}
				variadic = &TypeIdentity{Schema: *variadicSchema, Name: *variadicName}
				if err := builder.registerAddress(pgCatalogAddress{class: "pg_type", id: *variadicTypeID}, ObjectIdentityProjection{Type: &TypeObjectIdentity{Kind: "type", Identity: *variadic}}); err != nil {
					return err
				}
			}
			returns := TypeIdentity{Schema: returnSchema, Name: returnName}
			if err := builder.registerAddress(pgCatalogAddress{class: "pg_type", id: returnTypeID}, ObjectIdentityProjection{Type: &TypeObjectIdentity{Kind: "type", Identity: returns}}); err != nil {
				return err
			}
			function = &pgCatalogFunctionBuilder{
				address: pgCatalogAddress{class: "pg_proc", id: functionID},
				projection: FunctionProjection{
					Identity: identity, Kind: kind, Language: language, Arguments: []FunctionArgumentProjection{}, VariadicType: variadic,
					Returns: returns, ReturnSet: returnSet, Owner: owner, SecurityDefiner: securityDefiner, Volatility: volatility,
					Parallel: parallel, Leakproof: leakproof, Strict: strict, Config: cloneStringsExplicit(config), Cost: cost, Rows: rowsValue,
					ProsrcSHA256: DigestBytes([]byte(prosrc)), Probin: cloneStringPointer(probin),
				},
				acl: newACLAccumulator(projector.major, aclIsNull, "catalog_explicit"), arguments: make(map[uint32]FunctionArgumentProjection),
			}
			builder.functions[functionID] = function
			identityObject := ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: identity}}
			if err := builder.registerAddress(function.address, identityObject); err != nil {
				return err
			}
			if !builder.declaredContains(identityObject) {
				ownerCopy := owner
				if err := builder.addDenied(DeniedObjectProjection{Object: identityObject, Owner: &ownerCopy, ReasonCode: "undeclared_object"}); err != nil {
					return err
				}
			}
		} else if function.projection.Identity.Name != name || function.projection.Owner != owner || function.projection.Language != language || function.projection.ProsrcSHA256 != DigestBytes([]byte(prosrc)) {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.functions", projector.major, "function identity row is inconsistent across ACL expansion")
		}
		if err := addExpandedACLRow(function.acl, "catalog.functions.acl", grantor, grantee, privilege, grantable, defaultACLPrivileges["function"]); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.functions.iteration", projector.major, "function projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogFunctionArguments(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogFunctionArguments, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var functionID, functionName, ordinalText string
		var name *string
		var rawMode, typeID, typeSchema, typeName string
		var hasDefault bool
		if err := rows.Scan(&functionID, &functionName, &ordinalText, &name, &rawMode, &typeID, &typeSchema, &typeName, &hasDefault); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.function_arguments.scan", projector.major, "function argument projection scan failed")
		}
		if err := budget.add("catalog.function_arguments", functionID, functionName, ordinalText, nullableString(name), rawMode, typeID, typeSchema, typeName); err != nil {
			return err
		}
		function := builder.functions[functionID]
		if function == nil || function.projection.Identity.Name != functionName {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.function_arguments.function", projector.major, "function argument references an unknown function")
		}
		ordinal, err := parsePGUint32(ordinalText, "catalog.function_arguments.ordinal", projector.major)
		if err != nil || ordinal == 0 {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.function_arguments.ordinal", projector.major, "function argument ordinal is invalid")
		}
		if _, duplicate := function.arguments[ordinal]; duplicate {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.function_arguments", projector.major, "function argument ordinal is duplicate")
		}
		mode, err := normalizePGArgumentMode(rawMode)
		if err != nil {
			return err
		}
		typeIdentity := TypeIdentity{Schema: typeSchema, Name: typeName}
		argument := FunctionArgumentProjection{Ordinal: ordinal, Name: cloneStringPointer(name), Mode: mode, Type: typeIdentity}
		function.arguments[ordinal] = argument
		if err := builder.registerAddress(pgCatalogAddress{class: "pg_type", id: typeID}, ObjectIdentityProjection{Type: &TypeObjectIdentity{Kind: "type", Identity: typeIdentity}}); err != nil {
			return err
		}
		if hasDefault {
			functionIdentity := ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: function.projection.Identity}}
			if err := builder.addExpression(functionIdentity, "function_argument_default", ordinal); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.function_arguments.iteration", projector.major, "function argument projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogInternalObjects(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogInternalObjects, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var objectClass, objectID, subobjectText, objectName, semanticKind, ownerName string
		if err := rows.Scan(&objectClass, &objectID, &subobjectText, &objectName, &semanticKind, &ownerName); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.internal.scan", projector.major, "internal object projection scan failed")
		}
		if err := budget.add("catalog.internal", objectClass, objectID, subobjectText, objectName, semanticKind, ownerName); err != nil {
			return err
		}
		subobject, err := parsePGUint32(subobjectText, "catalog.internal.subobject", projector.major)
		if err != nil {
			return err
		}
		if objectClass != "pg_type" && objectClass != "pg_class" {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.internal.class", projector.major, "internal object class is outside the closed profile")
		}
		if semanticKind != "relation_row_type" && semanticKind != "relation_array_type" && semanticKind != "toast_relation" && semanticKind != "toast_index" && semanticKind != "toast_column_chunk_id" && semanticKind != "toast_column_chunk_seq" && semanticKind != "toast_column_chunk_data" {
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.internal.semantic_kind", projector.major, "internal object semantic kind is outside the closed profile")
		}
		var ownerIdentity ObjectIdentityProjection
		for _, relation := range builder.relations {
			if relation.projection.Identity.Name == ownerName {
				ownerIdentity = ObjectIdentityProjection{Relation: &RelationObjectIdentity{Kind: "relation", Identity: relation.projection.Identity}}
				break
			}
		}
		if ownerIdentity.Relation == nil || !builder.declaredContains(ownerIdentity) {
			fallback := ObjectIdentityProjection{Type: &TypeObjectIdentity{Kind: "type", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: objectName}}}
			if err := builder.addDenied(DeniedObjectProjection{Object: fallback, ReasonCode: "unbound_internal_object"}); err != nil {
				return err
			}
			continue
		}
		identity := ObjectIdentityProjection{Internal: &InternalObjectIdentity{Kind: "internal", SemanticKind: semanticKind, OwningObject: ownerIdentity}}
		if err := builder.registerAddress(pgCatalogAddress{class: objectClass, id: objectID, sub: subobject}, identity); err != nil {
			return err
		}
		if objectClass == "pg_type" {
			builder.internalNames["type\x00"+objectName] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.internal.iteration", projector.major, "internal object projection iteration failed")
	}
	return nil
}

func (projector *PGProjector) readCatalogDependencies(ctx context.Context, queryer catalogQueryer, builder *pgCatalogBuilder) error {
	rows, cancel, err := queryProjectionBounded(ctx, queryer, projectionQueryCatalogDependencies, projectionTargetSchema)
	if err != nil {
		return err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var objectClass, objectID, objectSubText, referencedClass, referencedID, referencedSubText, rawKind string
		var referencedSchema, referencedName, referencedAux *string
		var referencedArgumentSchemas, referencedArgumentNames []string
		if err := rows.Scan(&objectClass, &objectID, &objectSubText, &referencedClass, &referencedID, &referencedSubText, &rawKind, &referencedSchema, &referencedName, &referencedAux, &referencedArgumentSchemas, &referencedArgumentNames); err != nil {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.dependencies.scan", projector.major, "dependency projection scan failed")
		}
		if err := budget.add("catalog.dependencies", objectClass, objectID, objectSubText, referencedClass, referencedID, referencedSubText, rawKind, nullableString(referencedSchema), nullableString(referencedName), nullableString(referencedAux), strings.Join(referencedArgumentSchemas, "\x00"), strings.Join(referencedArgumentNames, "\x00")); err != nil {
			return err
		}
		objectSub, err := parsePGUint32(objectSubText, "catalog.dependencies.object_subid", projector.major)
		if err != nil {
			return err
		}
		referencedSub, err := parsePGUint32(referencedSubText, "catalog.dependencies.referenced_subid", projector.major)
		if err != nil {
			return err
		}
		depender, dependerOK := builder.addresses[(pgCatalogAddress{class: objectClass, id: objectID, sub: objectSub}).key()]
		if !dependerOK {
			// Expression backing objects (pg_attrdef/pg_rewrite) are outside this
			// structural slice and therefore never appear in target_addresses.
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.dependencies.depender", projector.major, "dependency depender is outside the normalized catalog closure")
		}
		if referencedClass == "pg_namespace" || referencedClass == "pg_language" || referencedClass == "pg_am" {
			continue
		}
		dependedOn, dependedOK := builder.addresses[(pgCatalogAddress{class: referencedClass, id: referencedID, sub: referencedSub}).key()]
		if !dependedOK && referencedSub == 0 {
			identity, identityOK, identityErr := dependencyReferenceIdentity(referencedClass, referencedSchema, referencedName, referencedAux, referencedArgumentSchemas, referencedArgumentNames)
			if identityErr != nil {
				return identityErr
			}
			if identityOK {
				if err := builder.registerAddress(pgCatalogAddress{class: referencedClass, id: referencedID}, identity); err != nil {
					return err
				}
				dependedOn, dependedOK = identity, true
			}
		}
		kind, err := normalizePGDependencyKind(rawKind)
		if err != nil {
			return err
		}
		if !dependedOK {
			if builder.hasExpressionFor(depender) {
				continue
			}
			kindCopy := kind
			if err := builder.addDenied(DeniedObjectProjection{Object: depender, DependencyKind: &kindCopy, ReasonCode: "dependency_outside_closure"}); err != nil {
				return err
			}
			continue
		}
		dependerKey, _ := canonicalContractKey(depender)
		dependedKey, _ := canonicalContractKey(dependedOn)
		if dependerKey == dependedKey {
			continue
		}
		dependency := DependencyProjection{Depender: cloneProjectionValue(depender), DependedOn: cloneProjectionValue(dependedOn), DependencyKind: kind}
		key := dependerKey + "\x00" + dependedKey + "\x00" + kind
		if _, duplicate := builder.dependencies[key]; duplicate {
			// Multiple physical catalog edges can collapse to one normalized
			// internal owner edge. The projection is a logical dependency set.
			continue
		}
		builder.dependencies[key] = dependency
		if uint64(len(builder.dependencies)) > projectionMaxDependencyEdges {
			return pgProjectionFailure(CodeProjectionLimitExceeded, "catalog.dependencies", projector.major, "dependency edge limit exceeded")
		}
	}
	if err := rows.Err(); err != nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.dependencies.iteration", projector.major, "dependency projection iteration failed")
	}
	return nil
}

func dependencyReferenceIdentity(class string, schema, name, aux *string, argumentSchemas, argumentNames []string) (ObjectIdentityProjection, bool, error) {
	requireName := func() (TypeIdentity, error) {
		if schema == nil || name == nil {
			return TypeIdentity{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.dependencies.reference", 0, "dependency reference identity is sparse")
		}
		identity := TypeIdentity{Schema: *schema, Name: *name}
		return identity, identity.Validate()
	}
	switch class {
	case "pg_type":
		identity, err := requireName()
		return ObjectIdentityProjection{Type: &TypeObjectIdentity{Kind: "type", Identity: identity}}, err == nil, err
	case "pg_collation":
		identity, err := requireName()
		return ObjectIdentityProjection{Collation: &CollationObjectIdentity{Kind: "collation", Identity: identity}}, err == nil, err
	case "pg_opclass":
		identity, err := requireName()
		if err != nil {
			return ObjectIdentityProjection{}, false, err
		}
		if aux == nil || *aux == "" {
			return ObjectIdentityProjection{}, false, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.dependencies.opclass", 0, "operator class dependency is missing its access method")
		}
		return ObjectIdentityProjection{Opclass: &OpclassObjectIdentity{Kind: "opclass", Identity: identity, AccessMethod: *aux}}, true, nil
	case "pg_operator", "pg_proc":
		if schema == nil || name == nil {
			return ObjectIdentityProjection{}, false, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.dependencies.sql_identity", 0, "SQL dependency identity is sparse")
		}
		identity, err := sqlIdentityFromParallel(*schema, *name, argumentSchemas, argumentNames)
		if err != nil {
			return ObjectIdentityProjection{}, false, err
		}
		if class == "pg_operator" {
			return ObjectIdentityProjection{Operator: &SQLObjectIdentity{Kind: "operator", Identity: identity}}, true, nil
		}
		return ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: identity}}, true, nil
	case "pg_cast":
		if len(argumentSchemas) != 2 || len(argumentNames) != 2 {
			return ObjectIdentityProjection{}, false, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.dependencies.cast", 0, "cast dependency identity is sparse")
		}
		identity := ObjectIdentityProjection{Cast: &CastObjectIdentity{Kind: "cast", SourceType: TypeIdentity{Schema: argumentSchemas[0], Name: argumentNames[0]}, TargetType: TypeIdentity{Schema: argumentSchemas[1], Name: argumentNames[1]}}}
		if err := identity.Validate(); err != nil {
			return ObjectIdentityProjection{}, false, err
		}
		return identity, true, nil
	default:
		return ObjectIdentityProjection{}, false, nil
	}
}

func (builder *pgCatalogBuilder) hasExpressionFor(identity ObjectIdentityProjection) bool {
	key, err := canonicalContractKey(identity)
	if err != nil {
		return false
	}
	for _, slot := range builder.expressions {
		slotKey, slotErr := canonicalContractKey(slot.Object)
		if slotErr == nil && slotKey == key {
			return true
		}
	}
	return false
}

func (builder *pgCatalogBuilder) finish() (pgCatalogStructure, error) {
	if err := builder.finishRelations(); err != nil {
		return pgCatalogStructure{}, err
	}
	if err := builder.finishFunctions(); err != nil {
		return pgCatalogStructure{}, err
	}
	if err := builder.reconcileNamespaceSightings(); err != nil {
		return pgCatalogStructure{}, err
	}
	if err := builder.reconcileDeclaredObjects(); err != nil {
		return pgCatalogStructure{}, err
	}
	dependencyKeys := make([]string, 0, len(builder.dependencies))
	for key := range builder.dependencies {
		dependencyKeys = append(dependencyKeys, key)
	}
	sort.Strings(dependencyKeys)
	builder.body.Dependencies = make([]DependencyProjection, 0, len(dependencyKeys))
	for _, key := range dependencyKeys {
		builder.body.Dependencies = append(builder.body.Dependencies, cloneProjectionValue(builder.dependencies[key]))
	}
	deniedKeys := make([]string, 0, len(builder.denied))
	for key := range builder.denied {
		deniedKeys = append(deniedKeys, key)
	}
	sort.Strings(deniedKeys)
	builder.body.DeniedObjects = make([]DeniedObjectProjection, 0, len(deniedKeys))
	for _, key := range deniedKeys {
		builder.body.DeniedObjects = append(builder.body.DeniedObjects, cloneProjectionValue(builder.denied[key]))
	}
	builder.body.DeclaredObjects = cloneProjectionValue(builder.scope.DeclaredObjects)
	builder.body.ObjectCount = uint32(len(builder.scope.DeclaredObjects))
	sort.Slice(builder.expressions, func(left, right int) bool {
		leftKey, _ := canonicalContractKey(builder.expressions[left].Object)
		rightKey, _ := canonicalContractKey(builder.expressions[right].Object)
		if leftKey != rightKey {
			return leftKey < rightKey
		}
		if builder.expressions[left].Field != builder.expressions[right].Field {
			return builder.expressions[left].Field < builder.expressions[right].Field
		}
		return builder.expressions[left].Ordinal < builder.expressions[right].Ordinal
	})
	for index := 1; index < len(builder.expressions); index++ {
		leftKey, _ := canonicalContractKey(builder.expressions[index-1].Object)
		rightKey, _ := canonicalContractKey(builder.expressions[index].Object)
		if leftKey == rightKey && builder.expressions[index-1].Field == builder.expressions[index].Field && builder.expressions[index-1].Ordinal == builder.expressions[index].Ordinal {
			return pgCatalogStructure{}, pgProjectionFailure(CodeProjectionUnknownObject, "catalog.expression_slots", builder.major, "expression slot is duplicate")
		}
	}
	physicalCount := uint64(len(builder.addresses))
	if physicalCount > projectionMaxCatalogObjects {
		return pgCatalogStructure{}, pgProjectionFailure(CodeProjectionLimitExceeded, "catalog.objects", builder.major, "normalized catalog object limit exceeded")
	}
	result := pgCatalogStructure{body: cloneProjectionValue(builder.body), expressions: cloneProjectionValue(builder.expressions)}
	if len(result.body.DeniedObjects) != 0 {
		return result, pgProjectionFailure(CodeProjectionUnknownObject, "catalog.denied", builder.major, "catalog contains an object or dependency outside the declared closure")
	}
	return result, nil
}

func (structure pgCatalogStructure) completeBody(major uint16) (CatalogProjectionBody, error) {
	if structure.absent {
		return CatalogProjectionBody{}, pgProjectionFailure(CodeCatalogDrift, "catalog.complete", major, "schema is absent at a catalog projection boundary")
	}
	if len(structure.expressions) != 0 {
		return CatalogProjectionBody{}, pgProjectionFailure(CodeProjectionNotImplemented, "catalog.expressions", major, "A2.1b expression projection is not implemented")
	}
	if len(structure.body.DeniedObjects) != 0 {
		return CatalogProjectionBody{}, pgProjectionFailure(CodeProjectionUnknownObject, "catalog.denied", major, "catalog contains an object or dependency outside the declared closure")
	}
	body := cloneProjectionValue(structure.body)
	if err := body.Validate(); err != nil {
		return CatalogProjectionBody{}, pgProjectionFailure(CodeCatalogDrift, "catalog.complete", major, "completed catalog projection body is invalid")
	}
	return body, nil
}

func (builder *pgCatalogBuilder) finishRelations() error {
	result := make([]RelationProjection, 0, len(builder.relations))
	for _, relation := range builder.relations {
		relation.projection.ExplicitACL = relation.acl.projection()
		columnOrdinals := make([]uint32, 0, len(relation.columns))
		for ordinal := range relation.columns {
			columnOrdinals = append(columnOrdinals, ordinal)
		}
		sort.Slice(columnOrdinals, func(left, right int) bool { return columnOrdinals[left] < columnOrdinals[right] })
		relation.projection.Columns = make([]ColumnProjection, 0, len(columnOrdinals))
		for _, ordinal := range columnOrdinals {
			column := relation.columns[ordinal]
			column.projection.ExplicitACL = column.acl.projection()
			relation.projection.Columns = append(relation.projection.Columns, cloneProjectionValue(column.projection))
		}
		constraintKeys := make([]string, 0, len(relation.constraints))
		for key := range relation.constraints {
			constraintKeys = append(constraintKeys, key)
		}
		sort.Slice(constraintKeys, func(left, right int) bool {
			return relation.constraints[constraintKeys[left]].projection.Name < relation.constraints[constraintKeys[right]].projection.Name
		})
		relation.projection.Constraints = make([]ConstraintProjection, 0, len(constraintKeys))
		for _, key := range constraintKeys {
			relation.projection.Constraints = append(relation.projection.Constraints, cloneProjectionValue(relation.constraints[key].projection))
		}
		indexKeys := make([]string, 0, len(relation.indexes))
		for key := range relation.indexes {
			indexKeys = append(indexKeys, key)
		}
		sort.Slice(indexKeys, func(left, right int) bool {
			return relation.indexes[indexKeys[left]].projection.Name < relation.indexes[indexKeys[right]].projection.Name
		})
		relation.projection.Indexes = make([]IndexProjection, 0, len(indexKeys))
		for _, key := range indexKeys {
			index := relation.indexes[key]
			if uint32(len(index.terms)) != index.keyCount || uint32(len(index.terms)+len(index.includes)) != index.physicalCount {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms", builder.major, "index term cardinality differs from pg_index")
			}
			index.projection.Terms = make([]IndexTermProjection, 0, index.keyCount)
			for ordinal := uint32(1); ordinal <= index.keyCount; ordinal++ {
				term, ok := index.terms[ordinal]
				if !ok {
					return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms", builder.major, "index key ordinal is missing")
				}
				index.projection.Terms = append(index.projection.Terms, cloneProjectionValue(term))
			}
			index.projection.Includes = make([]string, 0, len(index.includes))
			for ordinal := index.keyCount + 1; ordinal <= index.physicalCount; ordinal++ {
				column, ok := index.includes[ordinal]
				if !ok {
					return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.index_terms", builder.major, "included index ordinal is missing")
				}
				index.projection.Includes = append(index.projection.Includes, column)
			}
			relation.projection.Indexes = append(relation.projection.Indexes, cloneProjectionValue(index.projection))
		}
		policyKeys := make([]string, 0, len(relation.policies))
		for key := range relation.policies {
			policyKeys = append(policyKeys, key)
		}
		sort.Slice(policyKeys, func(left, right int) bool {
			return relation.policies[policyKeys[left]].Name < relation.policies[policyKeys[right]].Name
		})
		relation.projection.Policies = make([]PolicyProjection, 0, len(policyKeys))
		for _, key := range policyKeys {
			relation.projection.Policies = append(relation.projection.Policies, cloneProjectionValue(relation.policies[key]))
		}
		triggerKeys := make([]string, 0, len(relation.triggers))
		for key := range relation.triggers {
			triggerKeys = append(triggerKeys, key)
		}
		sort.Slice(triggerKeys, func(left, right int) bool {
			leftKey, _ := canonicalContractKey(relation.triggers[triggerKeys[left]].Identity)
			rightKey, _ := canonicalContractKey(relation.triggers[triggerKeys[right]].Identity)
			return leftKey < rightKey
		})
		relation.projection.Triggers = make([]TriggerProjection, 0, len(triggerKeys))
		for _, key := range triggerKeys {
			relation.projection.Triggers = append(relation.projection.Triggers, cloneProjectionValue(relation.triggers[key]))
		}
		result = append(result, cloneProjectionValue(relation.projection))
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Identity.Schema+"\x00"+result[left].Identity.Name < result[right].Identity.Schema+"\x00"+result[right].Identity.Name
	})
	builder.body.Relations = result
	return nil
}

func (builder *pgCatalogBuilder) finishFunctions() error {
	result := make([]FunctionProjection, 0, len(builder.functions))
	for _, function := range builder.functions {
		function.projection.ExplicitACL = function.acl.projection()
		function.projection.Arguments = make([]FunctionArgumentProjection, 0, len(function.arguments))
		for ordinal := uint32(1); ordinal <= uint32(len(function.arguments)); ordinal++ {
			argument, ok := function.arguments[ordinal]
			if !ok {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.function_arguments", builder.major, "function argument ordinal is missing")
			}
			function.projection.Arguments = append(function.projection.Arguments, cloneProjectionValue(argument))
		}
		identityArguments := make([]TypeIdentity, 0, len(function.projection.Arguments))
		for _, argument := range function.projection.Arguments {
			if argument.Mode == "in" || argument.Mode == "inout" || argument.Mode == "variadic" {
				identityArguments = append(identityArguments, argument.Type)
			}
		}
		if len(identityArguments) != len(function.projection.Identity.Arguments) {
			return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.function_arguments.identity", builder.major, "function identity argument cardinality differs from full arguments")
		}
		for index := range identityArguments {
			if identityArguments[index] != function.projection.Identity.Arguments[index] {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.function_arguments.identity", builder.major, "function identity arguments differ from full arguments")
			}
		}
		result = append(result, cloneProjectionValue(function.projection))
	}
	sort.Slice(result, func(left, right int) bool {
		leftKey, _ := canonicalContractKey(result[left].Identity)
		rightKey, _ := canonicalContractKey(result[right].Identity)
		return leftKey < rightKey
	})
	builder.body.Functions = result
	return nil
}

func (builder *pgCatalogBuilder) reconcileNamespaceSightings() error {
	relationNames := make(map[string]struct{})
	indexNames := make(map[string]struct{})
	functionNames := make(map[string]struct{})
	for _, relation := range builder.relations {
		relationNames[relation.projection.Identity.Name] = struct{}{}
		for _, index := range relation.indexes {
			indexNames[index.projection.Name] = struct{}{}
		}
	}
	for _, function := range builder.functions {
		functionNames[function.projection.Identity.Name] = struct{}{}
	}
	for _, sighting := range builder.sightings {
		switch sighting.Kind {
		case "relation":
			if _, ok := relationNames[sighting.Name]; ok {
				continue
			}
			if _, ok := indexNames[sighting.Name]; ok {
				continue
			}
			identity := ObjectIdentityProjection{Relation: &RelationObjectIdentity{Kind: "relation", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: sighting.Name}}}
			ownerCopy := sighting.Owner
			if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "unsupported_object_kind"}); err != nil {
				return err
			}
		case "function":
			if _, ok := functionNames[sighting.Name]; !ok {
				return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.namespace.function", builder.major, "namespace function inventory has no typed function projection")
			}
		case "type":
			if _, ok := builder.internalNames["type\x00"+sighting.Name]; ok {
				continue
			}
			identity := ObjectIdentityProjection{Type: &TypeObjectIdentity{Kind: "type", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: sighting.Name}}}
			ownerCopy := sighting.Owner
			if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "undeclared_object"}); err != nil {
				return err
			}
		case "extension":
			identity := ObjectIdentityProjection{Extension: &ExtensionObjectIdentity{Kind: "extension", Name: sighting.Name}}
			ownerCopy := sighting.Owner
			if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "unsupported_object_kind"}); err != nil {
				return err
			}
		case "collation":
			identity := ObjectIdentityProjection{Collation: &CollationObjectIdentity{Kind: "collation", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: sighting.Name}}}
			ownerCopy := sighting.Owner
			if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "undeclared_object"}); err != nil {
				return err
			}
		case "opclass":
			if sighting.Subkind == "" {
				return pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.namespace.opclass", builder.major, "operator class inventory is missing its access method")
			}
			identity := ObjectIdentityProjection{Opclass: &OpclassObjectIdentity{Kind: "opclass", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: sighting.Name}, AccessMethod: sighting.Subkind}}
			ownerCopy := sighting.Owner
			if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "unsupported_object_kind"}); err != nil {
				return err
			}
		case "operator", "opfamily", "conversion", "ts_config", "ts_dict", "ts_parser", "ts_template", "statistic_ext":
			schemaOwner := ObjectIdentityProjection{Schema: &SchemaObjectIdentity{Kind: "schema", Name: projectionTargetSchema}}
			identity := ObjectIdentityProjection{Internal: &InternalObjectIdentity{Kind: "internal", SemanticKind: "unsupported_namespace_object\x00" + sighting.Kind + "\x00" + sighting.Name + "\x00" + sighting.Subkind, OwningObject: schemaOwner}}
			ownerCopy := sighting.Owner
			if err := builder.addDenied(DeniedObjectProjection{Object: identity, Owner: &ownerCopy, ReasonCode: "unsupported_object_kind"}); err != nil {
				return err
			}
		default:
			return pgProjectionFailure(CodeProjectionUnknownObject, "catalog.namespace."+sighting.Kind, builder.major, "namespace contains an unknown object kind")
		}
	}
	return nil
}

func (builder *pgCatalogBuilder) reconcileDeclaredObjects() error {
	actual := make(map[string]struct{})
	schemaIdentity := ObjectIdentityProjection{Schema: &SchemaObjectIdentity{Kind: "schema", Name: projectionTargetSchema}}
	schemaKey, _ := canonicalContractKey(schemaIdentity)
	actual[schemaKey] = struct{}{}
	if !builder.declaredContains(schemaIdentity) {
		ownerCopy := builder.body.Schema.Owner
		if err := builder.addDenied(DeniedObjectProjection{Object: schemaIdentity, Owner: &ownerCopy, ReasonCode: "undeclared_object"}); err != nil {
			return err
		}
	}
	for _, relation := range builder.relations {
		identity := ObjectIdentityProjection{Relation: &RelationObjectIdentity{Kind: "relation", Identity: relation.projection.Identity}}
		key, _ := canonicalContractKey(identity)
		actual[key] = struct{}{}
		for _, column := range relation.columns {
			identity := ObjectIdentityProjection{Column: &ColumnObjectIdentity{Kind: "column", Relation: relation.projection.Identity, Name: column.projection.Name}}
			key, _ := canonicalContractKey(identity)
			actual[key] = struct{}{}
		}
		for _, constraint := range relation.constraints {
			key, _ := canonicalContractKey(constraint.identity)
			actual[key] = struct{}{}
		}
		for _, index := range relation.indexes {
			if index.identity.Index != nil {
				key, _ := canonicalContractKey(index.identity)
				actual[key] = struct{}{}
			}
		}
		for id, policy := range relation.policies {
			_ = id
			identity := ObjectIdentityProjection{Policy: &PolicyObjectIdentity{Kind: "policy", Relation: relation.projection.Identity, Name: policy.Name}}
			key, _ := canonicalContractKey(identity)
			actual[key] = struct{}{}
		}
		for _, trigger := range relation.triggers {
			key, _ := canonicalContractKey(trigger.Identity)
			actual[key] = struct{}{}
		}
	}
	for _, function := range builder.functions {
		identity := ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: function.projection.Identity}}
		key, _ := canonicalContractKey(identity)
		actual[key] = struct{}{}
	}
	for _, identity := range builder.addresses {
		if identity.Internal == nil {
			continue
		}
		key, _ := canonicalContractKey(identity)
		actual[key] = struct{}{}
	}
	for key, identity := range builder.declared {
		if _, ok := actual[key]; !ok {
			if err := builder.addDenied(DeniedObjectProjection{Object: identity, ReasonCode: "undeclared_object"}); err != nil {
				return err
			}
		}
	}
	return nil
}

func addExpandedACLRow(acl *aclAccumulator, path string, grantor, grantee, privilege *string, grantable *bool, allowed map[string]struct{}) error {
	if grantor == nil && grantee == nil && privilege == nil && grantable == nil {
		return nil
	}
	if grantor == nil || grantee == nil {
		return pgProjectionFailure(CodeProjectionUnknownObject, path, 0, "catalog ACL references an unknown principal")
	}
	if privilege == nil || grantable == nil {
		return pgProjectionFailure(CodeProjectionCatalogQueryFailed, path, 0, "catalog ACL expansion row is sparse")
	}
	return acl.add(path, *grantor, *grantee, *privilege, *grantable, allowed)
}

func sqlIdentityFromParallel(schema, name string, argumentSchemas, argumentNames []string) (SQLIdentity, error) {
	if schema == "" || name == "" || argumentSchemas == nil || argumentNames == nil || len(argumentSchemas) != len(argumentNames) {
		return SQLIdentity{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.sql_identity", 0, "SQL identity row is sparse or misaligned")
	}
	identity := SQLIdentity{Schema: schema, Name: name, Arguments: make([]TypeIdentity, len(argumentSchemas))}
	for index := range argumentSchemas {
		identity.Arguments[index] = TypeIdentity{Schema: argumentSchemas[index], Name: argumentNames[index]}
		if err := identity.Arguments[index].Validate(); err != nil {
			return SQLIdentity{}, err
		}
	}
	return identity, identity.Validate()
}

func normalizePGConstraintType(value string) (string, error) {
	switch value {
	case "p":
		return "primary_key", nil
	case "u":
		return "unique", nil
	case "f":
		return "foreign_key", nil
	case "c":
		return "check", nil
	case "x":
		return "exclusion", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.constraints.type", 0, "constraint type is outside the closed profile")
	}
}

func normalizePGConstraintActions(kind, match, update, deleteAction string) (string, string, string, error) {
	if kind != "foreign_key" {
		return "none", "none", "none", nil
	}
	normalizedMatch := map[string]string{"s": "simple", "f": "full", "p": "partial"}[match]
	normalizedUpdate := map[string]string{"a": "no_action", "r": "restrict", "c": "cascade", "n": "set_null", "d": "set_default"}[update]
	normalizedDelete := map[string]string{"a": "no_action", "r": "restrict", "c": "cascade", "n": "set_null", "d": "set_default"}[deleteAction]
	if normalizedMatch == "" || normalizedUpdate == "" || normalizedDelete == "" {
		return "", "", "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.constraints.action", 0, "foreign key action is outside the closed profile")
	}
	return normalizedMatch, normalizedUpdate, normalizedDelete, nil
}

func normalizePGPolicyCommand(value string) (string, error) {
	switch value {
	case "*":
		return "all", nil
	case "r":
		return "select", nil
	case "a":
		return "insert", nil
	case "w":
		return "update", nil
	case "d":
		return "delete", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.policies.command", 0, "policy command is outside the closed profile")
	}
}

func normalizePGTriggerEnabled(value string) (string, error) {
	switch value {
	case "O":
		return "origin", nil
	case "A":
		return "always", nil
	case "R":
		return "replica", nil
	case "D":
		return "disabled", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.triggers.enabled", 0, "trigger enabled state is outside the closed profile")
	}
}

func normalizePGInternalTriggerKind(function SQLIdentity) (string, error) {
	if function.Schema != "pg_catalog" || len(function.Arguments) != 0 {
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.triggers.internal", 0, "internal trigger function is outside the closed profile")
	}
	kinds := map[string]string{
		"RI_FKey_check_ins":      "fk_check_insert",
		"RI_FKey_check_upd":      "fk_check_update",
		"RI_FKey_noaction_del":   "fk_no_action_delete",
		"RI_FKey_noaction_upd":   "fk_no_action_update",
		"RI_FKey_restrict_del":   "fk_restrict_delete",
		"RI_FKey_restrict_upd":   "fk_restrict_update",
		"RI_FKey_cascade_del":    "fk_cascade_delete",
		"RI_FKey_cascade_upd":    "fk_cascade_update",
		"RI_FKey_setnull_del":    "fk_set_null_delete",
		"RI_FKey_setnull_upd":    "fk_set_null_update",
		"RI_FKey_setdefault_del": "fk_set_default_delete",
		"RI_FKey_setdefault_upd": "fk_set_default_update",
		"unique_key_recheck":     "unique_key_recheck",
	}
	kind := kinds[function.Name]
	if kind == "" {
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.triggers.internal", 0, "internal trigger function is outside the closed profile")
	}
	return "constraint_trigger_" + kind, nil
}

func normalizePGFunctionKind(value string) (string, error) {
	switch value {
	case "f":
		return "function", nil
	case "p":
		return "procedure", nil
	case "a":
		return "aggregate", nil
	case "w":
		return "window", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.functions.kind", 0, "function kind is outside the closed profile")
	}
}

func normalizePGVolatility(value string) (string, error) {
	switch value {
	case "i":
		return "immutable", nil
	case "s":
		return "stable", nil
	case "v":
		return "volatile", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.functions.volatility", 0, "function volatility is outside the closed profile")
	}
}

func normalizePGParallel(value string) (string, error) {
	switch value {
	case "s":
		return "safe", nil
	case "r":
		return "restricted", nil
	case "u":
		return "unsafe", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.functions.parallel", 0, "function parallel mode is outside the closed profile")
	}
}

func normalizePGArgumentMode(value string) (string, error) {
	switch value {
	case "i":
		return "in", nil
	case "o":
		return "out", nil
	case "b":
		return "inout", nil
	case "v":
		return "variadic", nil
	case "t":
		return "table", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.function_arguments.mode", 0, "function argument mode is outside the closed profile")
	}
}

func normalizePGDependencyKind(value string) (string, error) {
	switch value {
	case "n":
		return "normal", nil
	case "a":
		return "automatic", nil
	case "i":
		return "internal", nil
	case "e":
		return "extension", nil
	case "x":
		return "automatic_extension", nil
	case "P":
		return "partition_primary", nil
	case "S":
		return "partition_secondary", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.dependencies.kind", 0, "dependency kind is outside the closed profile")
	}
}

func relationPrivileges(kind string) map[string]struct{} {
	if kind == "sequence" {
		return defaultACLPrivileges["sequence"]
	}
	return defaultACLPrivileges["table"]
}

func normalizeVersionedRelationACL(major uint16, owner string, grantor, grantee, privilege *string, grantable *bool) (bool, error) {
	if privilege == nil || *privilege != "MAINTAIN" {
		return false, nil
	}
	// PostgreSQL 17 added MAINTAIN to the catalog ACL emitted for the
	// relation owner. Ownership is already an exact projection field, so the
	// owner-only, non-grantable baseline is folded out to preserve the PG15-17
	// logical model. Any wider MAINTAIN grant remains an unknown privilege.
	if major == 17 && grantor != nil && grantee != nil && grantable != nil && *grantor == owner && *grantee == owner && !*grantable {
		return true, nil
	}
	return false, pgProjectionFailure(CodeProjectionUnknownObject, "catalog.relations.acl", major, "MAINTAIN privilege is outside the version-neutral relation ACL profile")
}

var columnPrivileges = map[string]struct{}{"INSERT": {}, "REFERENCES": {}, "SELECT": {}, "UPDATE": {}}

func normalizePGRelationKind(value string) (string, error) {
	switch value {
	case "r":
		return "table", nil
	case "p":
		return "partitioned_table", nil
	case "v":
		return "view", nil
	case "m":
		return "materialized_view", nil
	case "f":
		return "foreign_table", nil
	case "S":
		return "sequence", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.relations.relkind", 0, "relation kind is outside the closed PG15-PG17 profile")
	}
}

func mustNormalizePGRelationKind(value string) string {
	result, _ := normalizePGRelationKind(value)
	return result
}

func normalizePGPersistence(value string) (string, error) {
	switch value {
	case "p":
		return "permanent", nil
	case "u":
		return "unlogged", nil
	case "t":
		return "temporary", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.relations.persistence", 0, "relation persistence is outside the closed profile")
	}
}

func mustNormalizePGPersistence(value string) string {
	result, _ := normalizePGPersistence(value)
	return result
}

func normalizePGReplicaIdentity(value string) (string, error) {
	switch value {
	case "d":
		return "default", nil
	case "n":
		return "nothing", nil
	case "f":
		return "full", nil
	case "i":
		return "index", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.relations.replica_identity", 0, "replica identity is outside the closed profile")
	}
}

func mustNormalizePGReplicaIdentity(value string) string {
	result, _ := normalizePGReplicaIdentity(value)
	return result
}

func normalizePGColumnIdentity(value string) (string, error) {
	switch value {
	case "":
		return "none", nil
	case "a":
		return "always", nil
	case "d":
		return "by_default", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.columns.identity", 0, "column identity mode is outside the closed profile")
	}
}

func normalizePGGenerated(value string) (string, error) {
	switch value {
	case "":
		return "none", nil
	case "s":
		return "stored", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.columns.generated", 0, "generated column mode is outside the PG15-PG17 profile")
	}
}

func normalizePGStorage(value string) (string, error) {
	switch value {
	case "p":
		return "plain", nil
	case "e":
		return "external", nil
	case "x":
		return "extended", nil
	case "m":
		return "main", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.columns.storage", 0, "column storage mode is outside the closed profile")
	}
}

func normalizePGCompression(value string) (string, error) {
	switch value {
	case "":
		return "default", nil
	case "p":
		return "pglz", nil
	case "l":
		return "lz4", nil
	default:
		return "", pgProjectionFailure(CodeProjectionUnknownObject, "catalog.columns.compression", 0, "column compression is outside the closed profile")
	}
}

func parsePGUint32(value, path string, major uint16) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, pgProjectionFailure(CodeProjectionCatalogQueryFailed, path, major, "catalog unsigned integer is invalid")
	}
	return uint32(parsed), nil
}

func equalStringPointers(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func sortedCopy(values []string) []string {
	result := cloneStringsExplicit(values)
	sort.Strings(result)
	return result
}

func cloneStringsExplicit(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func decodeTriggerArguments(encoded string, expected uint32) ([]string, error) {
	bytes, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.arguments", 0, "trigger arguments are not valid catalog hex")
	}
	if expected == 0 {
		if len(bytes) != 0 {
			return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.arguments", 0, "zero-argument trigger has argument bytes")
		}
		return []string{}, nil
	}
	if len(bytes) == 0 || bytes[len(bytes)-1] != 0 {
		return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.arguments", 0, "trigger argument catalog bytes are not NUL terminated")
	}
	parts := strings.Split(string(bytes[:len(bytes)-1]), "\x00")
	if uint32(len(parts)) != expected {
		return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.arguments", 0, "trigger argument count differs from catalog bytes")
	}
	for _, part := range parts {
		if !utf8.ValidString(part) {
			return nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.triggers.arguments", 0, "trigger argument is not valid UTF-8")
		}
	}
	return parts, nil
}
