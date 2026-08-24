package migration

import "time"

const (
	projectionMaxQueryRows                  uint64 = 8_192
	projectionMaxRowBytes                   uint64 = 65_536
	projectionMaxTotalResultBytes           uint64 = 8_388_608
	projectionMaxPrincipals                 uint64 = 256
	projectionMaxMembershipEdges            uint64 = 1_024
	projectionMaxMembershipDepth            uint64 = 32
	projectionMaxCanonicalWitnessCandidates uint64 = 4_096
	projectionMaxACLEntries                 uint64 = 4_096
	projectionMaxDefaultACLEntries          uint64 = 512
	projectionMaxCatalogObjects             uint64 = 4_096
	projectionMaxDependencyEdges            uint64 = 8_192
	projectionMaxExpressionNodes            uint64 = 4_096
	projectionMaxSecurityLabelsPerObject    uint64 = 32
	projectionMaxRoleSettings               uint64 = 512
	projectionMaxQueriesPerProjection       uint32 = 128
	projectionQueryTimeout                         = 5 * time.Second
	projectionLockTimeout                          = 1 * time.Second
	projectionSnapshotLifetime                     = 30 * time.Second
	projectionIdleInTransactionTimeout             = 60 * time.Second
)

// ProjectionBounds is returned by value. Mutating a copy cannot change the
// fixed v1 limits used by snapshots or projectors.
type ProjectionBounds struct {
	MaxQueryRows                    uint64
	MaxRowBytes                     uint64
	MaxTotalResultBytes             uint64
	MaxPrincipals                   uint64
	MaxMembershipEdges              uint64
	MaxMembershipDepth              uint64
	MaxCanonicalWitnessCandidates   uint64
	MaxACLEntries                   uint64
	MaxDefaultACLEntries            uint64
	MaxCatalogObjects               uint64
	MaxDependencyEdges              uint64
	MaxExpressionNodes              uint64
	MaxSecurityLabelsPerObject      uint64
	MaxRoleSettings                 uint64
	MaxQueriesPerProjection         uint32
	QueryTimeout                    time.Duration
	LockTimeout                     time.Duration
	SnapshotLifetime                time.Duration
	IdleInTransactionSessionTimeout time.Duration
}

func FixedProjectionBounds() ProjectionBounds {
	return ProjectionBounds{
		MaxQueryRows:                    projectionMaxQueryRows,
		MaxRowBytes:                     projectionMaxRowBytes,
		MaxTotalResultBytes:             projectionMaxTotalResultBytes,
		MaxPrincipals:                   projectionMaxPrincipals,
		MaxMembershipEdges:              projectionMaxMembershipEdges,
		MaxMembershipDepth:              projectionMaxMembershipDepth,
		MaxCanonicalWitnessCandidates:   projectionMaxCanonicalWitnessCandidates,
		MaxACLEntries:                   projectionMaxACLEntries,
		MaxDefaultACLEntries:            projectionMaxDefaultACLEntries,
		MaxCatalogObjects:               projectionMaxCatalogObjects,
		MaxDependencyEdges:              projectionMaxDependencyEdges,
		MaxExpressionNodes:              projectionMaxExpressionNodes,
		MaxSecurityLabelsPerObject:      projectionMaxSecurityLabelsPerObject,
		MaxRoleSettings:                 projectionMaxRoleSettings,
		MaxQueriesPerProjection:         projectionMaxQueriesPerProjection,
		QueryTimeout:                    projectionQueryTimeout,
		LockTimeout:                     projectionLockTimeout,
		SnapshotLifetime:                projectionSnapshotLifetime,
		IdleInTransactionSessionTimeout: projectionIdleInTransactionTimeout,
	}
}

func validateProjectionBounds(bounds ProjectionBounds) error {
	if bounds != FixedProjectionBounds() {
		return projectionFailure(CodeProjectionLimitOverride, "projection-limits", "limits_profile", 0, false, "projection limits v1 cannot be overridden")
	}
	return nil
}
