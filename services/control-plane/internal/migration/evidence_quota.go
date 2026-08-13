package migration

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"sync/atomic"
)

const (
	evidenceSegmentMaximumRecords = uint64(4096)
	evidenceSegmentMaximumBytes   = uint64(16 << 20)
	lineageIndexMaximumRecords    = uint64(16384)
	lineageIndexMaximumBytes      = uint64(16 << 20)

	rootFinalObjectMaximumCount = uint64(64)
	rootFinalObjectMaximumBytes = uint64(4 << 30)
	rootTempMaximumCount        = uint64(64)
	rootTempMaximumEachBytes    = uint64(64 << 20)
	rootTempMaximumTotalBytes   = uint64(4 << 30)
	rootJournalMaximumCount     = uint64(16)
	rootJournalMaximumBytes     = uint64(4 << 30)
	rootIndexMaximumCount       = uint64(64)
	rootIndexMaximumBytes       = uint64(1 << 30)
)

// verifiedQuotaBundleFacts is the immutable scalar projection used by quota
// calculation. Its sole production binder rechecks signed catalog descriptors
// against exact SQL statement boundaries after runtime closure validation.
type verifiedQuotaBundleFacts struct {
	schemaBundleDigest  Digest
	maxAttempts         uint64
	statementCounts     []uint64
	runtimeInputs       [32]byte
	outerArtifactDigest Digest
	outerArtifactSize   uint64
	canonical           [32]byte
}

// quotaBundleArithmeticFacts carries no verifier or execution authority. It is
// the closed numeric input shared by verified admission and stored-history
// contradiction checks.
type quotaBundleArithmeticFacts struct {
	maxAttempts     uint64
	statementCounts []uint64
}

func bindVerifiedQuotaBundleFacts(manifest *Manifest, files map[string][]byte, runtimeInputs [32]byte, outerArtifactDigest Digest, outerArtifactSize uint64) (verifiedQuotaBundleFacts, error) {
	maxAttempts, statementCounts, err := inspectQuotaBundleFacts(manifest, files)
	if err != nil {
		return verifiedQuotaBundleFacts{}, err
	}
	facts := verifiedQuotaBundleFacts{
		schemaBundleDigest:  manifest.SchemaBundleDigest,
		maxAttempts:         maxAttempts,
		statementCounts:     statementCounts,
		runtimeInputs:       runtimeInputs,
		outerArtifactDigest: outerArtifactDigest,
		outerArtifactSize:   outerArtifactSize,
	}
	if err := requireDigest("quota.schema_bundle_digest", facts.schemaBundleDigest); err != nil {
		return verifiedQuotaBundleFacts{}, err
	}
	facts.canonical = quotaBundleFactsDigest(facts.schemaBundleDigest, facts.maxAttempts, facts.statementCounts, facts.runtimeInputs, facts.outerArtifactDigest, facts.outerArtifactSize)
	return facts, nil
}

// inspectQuotaBundleFacts performs strict bundle/SQL boundary inspection but
// returns ordinary arithmetic inputs only. The verified binder above remains
// the sole authority constructor.
func inspectQuotaBundleFacts(manifest *Manifest, files map[string][]byte) (uint64, []uint64, error) {
	if manifest == nil || manifest.ExecutionPolicy.MaxAttempts == 0 || len(manifest.SchemaBundle.Migrations) == 0 {
		return 0, nil, quotaLimit("bundle facts are unavailable")
	}
	statementCounts := make([]uint64, len(manifest.SchemaBundle.Migrations))
	for entryIndex, entry := range manifest.SchemaBundle.Migrations {
		catalogRaw, ok := files[entry.CatalogContract.Path]
		if !ok || uint64(len(catalogRaw)) != entry.CatalogContract.SizeBytes || DigestBytes(catalogRaw) != entry.CatalogContract.SHA256 {
			return 0, nil, fail(CodeInvalidArtifact, entry.CatalogContract.Path, "quota catalog artifact differs from its descriptor", nil)
		}
		contract, err := DecodeCatalogContract(catalogRaw)
		if err != nil {
			return 0, nil, err
		}
		source, err := exactMigrationSource(contract.SourceDescriptors, entry.ID)
		if err != nil {
			return 0, nil, err
		}
		if source.SQLSHA256 != entry.SQLArtifact.SHA256 {
			return 0, nil, fail(CodeInvalidManifest, entry.ID, "quota source SQL digest differs from manifest", nil)
		}
		sqlRaw, ok := files[entry.SQLArtifact.Path]
		if !ok || uint64(len(sqlRaw)) != entry.SQLArtifact.SizeBytes || DigestBytes(sqlRaw) != entry.SQLArtifact.SHA256 {
			return 0, nil, fail(CodeInvalidArtifact, entry.SQLArtifact.Path, "quota SQL artifact differs from its descriptor", nil)
		}
		statements, err := SplitPostgreSQLStatements(sqlRaw)
		if err != nil {
			return 0, nil, err
		}
		if len(statements) == 0 || len(statements) != len(source.Statements) {
			return 0, nil, fail(CodeInvalidSQL, entry.ID, "quota statement count differs from signed source descriptor", nil)
		}
		for index, statement := range statements {
			descriptor := source.Statements[index]
			if descriptor.Index != uint64(index) || descriptor.Start != uint64(statement.Start) || descriptor.End != uint64(statement.End) || descriptor.SHA256 != statement.SHA256 {
				return 0, nil, fail(CodeInvalidSQL, entry.ID, "quota statement boundary differs from signed source descriptor", nil)
			}
		}
		statementCounts[entryIndex] = uint64(len(statements))
	}
	return manifest.ExecutionPolicy.MaxAttempts, statementCounts, nil
}

func quotaBundleFactsDigest(schema Digest, attempts uint64, counts []uint64, runtimeInputs [32]byte, outerArtifactDigest Digest, outerArtifactSize uint64) [32]byte {
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-evidence-quota-bundle-facts/v1\x00"))
	h.Write([]byte(schema))
	h.Write(runtimeInputs[:])
	h.Write([]byte(outerArtifactDigest))
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], outerArtifactSize)
	h.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], attempts)
	h.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], uint64(len(counts)))
	h.Write(encoded[:])
	for _, count := range counts {
		binary.BigEndian.PutUint64(encoded[:], count)
		h.Write(encoded[:])
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest
}

func (facts verifiedQuotaBundleFacts) valid() bool {
	if requireDigest("quota.schema_bundle_digest", facts.schemaBundleDigest) != nil || requireDigest("quota.outer_artifact_digest", facts.outerArtifactDigest) != nil || facts.outerArtifactSize == 0 || facts.outerArtifactSize > maxRuntimeTarSize || facts.runtimeInputs == [32]byte{} || facts.maxAttempts == 0 || len(facts.statementCounts) == 0 {
		return false
	}
	for _, count := range facts.statementCounts {
		if count == 0 {
			return false
		}
	}
	return facts.canonical == quotaBundleFactsDigest(facts.schemaBundleDigest, facts.maxAttempts, facts.statementCounts, facts.runtimeInputs, facts.outerArtifactDigest, facts.outerArtifactSize)
}

type evidenceQuotaReservation struct {
	ReservedRecords           uint64
	ReservedJournalBytes      uint64
	ReservedSegments          uint32
	ReservedCheckpointRecords uint64
	ReservedIndexRecords      uint64
	ReservedIndexBytes        uint64
	ReservedBytes             uint64
}

func calculateEvidenceQuotaReservation(facts verifiedQuotaBundleFacts, root *verifiedRootQuotaState) (evidenceQuotaReservation, error) {
	rootFacts, err := root.snapshot()
	if err != nil {
		return evidenceQuotaReservation{}, err
	}
	return calculateEvidenceQuotaReservationForFacts(facts, rootFacts)
}

// calculateEvidenceQuotaReservationForFacts is arithmetic only. It consumes no
// filesystem or lock authority and is kept separate so formula tests cannot
// manufacture a verifiedRootQuotaState.
func calculateEvidenceQuotaReservationForFacts(facts verifiedQuotaBundleFacts, rootFacts rootQuotaUsageFacts) (evidenceQuotaReservation, error) {
	if !facts.valid() {
		return evidenceQuotaReservation{}, fail(CodeUntrusted, "evidence-quota", "verified bundle facts are invalid", nil)
	}
	if !rootFacts.valid() {
		return evidenceQuotaReservation{}, filesystemFailure("quota-root", "root quota facts are invalid")
	}
	return calculateEvidenceQuotaReservationFromArithmeticFacts(quotaBundleArithmeticFacts{facts.maxAttempts, facts.statementCounts}, !rootFacts.targetIndexPresent)
}

// calculateEvidenceQuotaReservationFromArithmeticFacts is authority-free
// arithmetic over already inspected bundle facts. includeIndexHeader is true
// only for a brand-new lineage; every registered historical generation already
// owns its index.
func calculateEvidenceQuotaReservationFromArithmeticFacts(facts quotaBundleArithmeticFacts, includeIndexHeader bool) (evidenceQuotaReservation, error) {
	if facts.maxAttempts == 0 || len(facts.statementCounts) == 0 {
		return evidenceQuotaReservation{}, quotaLimit("bundle facts are unavailable")
	}
	for _, count := range facts.statementCounts {
		if count == 0 {
			return evidenceQuotaReservation{}, quotaLimit("bundle facts are unavailable")
		}
	}
	reservation := evidenceQuotaReservation{ReservedRecords: 1, ReservedJournalBytes: evidenceRecordFrameLimits[EvidenceRecordHeader], ReservedSegments: 1}
	segmentRecords := uint64(1)
	segmentBytes := evidenceRecordFrameLimits[EvidenceRecordHeader]
	appendCaller := func(size uint64) error {
		if size == 0 || size > maxEvidenceFrameBytes {
			return quotaLimit("record-kind maximum is invalid")
		}
		records, overflow := quotaAdd(segmentRecords, 1)
		bytes, byteOverflow := quotaAdd(segmentBytes, size)
		if overflow || byteOverflow {
			return quotaLimit("segment arithmetic overflow")
		}
		if records > evidenceSegmentMaximumRecords || bytes > evidenceSegmentMaximumBytes {
			segments, addOverflow := quotaAdd(uint64(reservation.ReservedSegments), 1)
			if addOverflow || segments > uint64(maxEvidenceReservedSegments) {
				return quotaLimit("journal requires more than sixteen segments")
			}
			reservation.ReservedSegments = uint32(segments)
			segmentRecords, segmentBytes = 1, evidenceRecordFrameLimits[EvidenceRecordHeader]
			var err error
			reservation.ReservedRecords, err = quotaAddValue(reservation.ReservedRecords, 1)
			if err != nil {
				return err
			}
			reservation.ReservedJournalBytes, err = quotaAddValue(reservation.ReservedJournalBytes, evidenceRecordFrameLimits[EvidenceRecordHeader])
			if err != nil {
				return err
			}
		}
		var err error
		segmentRecords, err = quotaAddValue(segmentRecords, 1)
		if err != nil {
			return err
		}
		segmentBytes, err = quotaAddValue(segmentBytes, size)
		if err != nil {
			return err
		}
		reservation.ReservedRecords, err = quotaAddValue(reservation.ReservedRecords, 1)
		if err != nil {
			return err
		}
		reservation.ReservedJournalBytes, err = quotaAddValue(reservation.ReservedJournalBytes, size)
		return err
	}
	for _, statementCount := range facts.statementCounts {
		for attempt := uint64(0); attempt < facts.maxAttempts; attempt++ {
			for statement := uint64(0); statement < statementCount; statement++ {
				if err := appendCaller(evidenceRecordFrameLimits[EvidenceRecordStatementIntent]); err != nil {
					return evidenceQuotaReservation{}, err
				}
				if err := appendCaller(evidenceRecordFrameLimits[EvidenceRecordIntermediate]); err != nil {
					return evidenceQuotaReservation{}, err
				}
			}
			for _, size := range []uint64{evidenceRecordFrameLimits[EvidenceRecordCommitIntent], evidenceRecordFrameLimits[EvidenceRecordAttemptTerminal], evidenceRecordFrameLimits[EvidenceRecordAmbiguousResolution]} {
				if err := appendCaller(size); err != nil {
					return evidenceQuotaReservation{}, err
				}
			}
		}
	}
	reservation.ReservedCheckpointRecords = reservation.ReservedRecords - 1
	reservation.ReservedIndexRecords = 2 + reservation.ReservedCheckpointRecords + 1
	if includeIndexHeader {
		reservation.ReservedIndexRecords++
		reservation.ReservedIndexBytes = lineageRecordFrameLimits[LineageRecordHeader]
	}
	var err error
	for _, size := range []uint64{lineageRecordFrameLimits[LineageRecordGenerationReserved], lineageRecordFrameLimits[LineageRecordGenerationActivated]} {
		reservation.ReservedIndexBytes, err = quotaAddValue(reservation.ReservedIndexBytes, size)
		if err != nil {
			return evidenceQuotaReservation{}, err
		}
	}
	checkpointBytes, overflow := quotaMul(lineageRecordFrameLimits[LineageRecordGenerationCheckpoint], reservation.ReservedCheckpointRecords)
	if overflow {
		return evidenceQuotaReservation{}, quotaLimit("checkpoint reservation overflow")
	}
	reservation.ReservedIndexBytes, err = quotaAddValue(reservation.ReservedIndexBytes, checkpointBytes)
	if err != nil {
		return evidenceQuotaReservation{}, err
	}
	reservation.ReservedIndexBytes, err = quotaAddValue(reservation.ReservedIndexBytes, lineageRecordFrameLimits[LineageRecordGenerationSuperseded])
	if err != nil {
		return evidenceQuotaReservation{}, err
	}
	reservation.ReservedBytes, err = quotaAddValue(reservation.ReservedJournalBytes, reservation.ReservedIndexBytes)
	if err != nil {
		return evidenceQuotaReservation{}, err
	}
	if reservation.ReservedRecords > maxEvidenceReservedRecords || reservation.ReservedJournalBytes > maxEvidenceReservedBytes || reservation.ReservedBytes > maxEvidenceReservedBytes || reservation.ReservedIndexRecords > lineageIndexMaximumRecords || reservation.ReservedIndexBytes > lineageIndexMaximumBytes {
		return evidenceQuotaReservation{}, quotaLimit("whole-bundle reservation exceeds a fixed inclusive maximum")
	}
	return reservation, nil
}

type rootQuotaObjectFact struct {
	digest Digest
	size   uint64
}

type rootQuotaUsageFacts struct {
	finalObjects         []rootQuotaObjectFact
	finalObjectBytes     uint64
	tempCount            uint64
	tempBytes            uint64
	largestTempBytes     uint64
	journalCount         uint64
	journalReservedBytes uint64
	indexCount           uint64
	indexActualBytes     uint64
	targetIndexPresent   bool
	targetIndexRecords   uint64
	targetIndexBytes     uint64
}

// verifiedRootQuotaState owns one exact root scan under the root-wide lock.
// There is deliberately no production constructor in this slice: aggregate
// counters are not scan authority, and accepting them would permit low-report
// admission. The strict filesystem scanner/replayer must mint this state in the
// content/journal slice. Calculator tests construct fixtures in _test.go only.
type verifiedRootQuotaState struct {
	facts     rootQuotaUsageFacts
	canonical [32]byte
	lock      *evidenceLineageLock
	consumed  atomic.Bool
}

// verifiedRootQuotaScan reserves the future filesystem-owned scanner output.
// It has no constructor in this slice and cannot yet authorize admission.
type verifiedRootQuotaScan struct {
	filesystemAuthority any
}

type rootQuotaAdmission struct {
	finalObjectCount         uint64
	finalObjectBytes         uint64
	journalCount             uint64
	journalReservedBytes     uint64
	indexCount               uint64
	indexReservedBytes       uint64
	targetIndexRecords       uint64
	targetIndexReservedBytes uint64
}

func rootQuotaFactsDigest(facts rootQuotaUsageFacts) [32]byte {
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-evidence-root-quota-facts/v1\x00"))
	var encoded [8]byte
	write := func(value uint64) { binary.BigEndian.PutUint64(encoded[:], value); h.Write(encoded[:]) }
	write(uint64(len(facts.finalObjects)))
	for _, object := range facts.finalObjects {
		h.Write([]byte(object.digest))
		h.Write([]byte{0})
		write(object.size)
	}
	for _, value := range []uint64{facts.finalObjectBytes, facts.tempCount, facts.tempBytes, facts.largestTempBytes, facts.journalCount, facts.journalReservedBytes, facts.indexCount, facts.indexActualBytes, facts.targetIndexRecords, facts.targetIndexBytes} {
		write(value)
	}
	if facts.targetIndexPresent {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest
}

func (facts rootQuotaUsageFacts) valid() bool {
	if facts.exceedsLimits() {
		return false
	}
	var total uint64
	previous := Digest("")
	for _, object := range facts.finalObjects {
		if requireDigest("quota.object", object.digest) != nil || object.size == 0 || object.size > rootTempMaximumEachBytes || (previous != "" && object.digest <= previous) {
			return false
		}
		var err error
		total, err = quotaAddValue(total, object.size)
		if err != nil {
			return false
		}
		previous = object.digest
	}
	targetStateExact := (facts.targetIndexPresent && facts.targetIndexRecords > 0 && facts.targetIndexBytes > 0) || (!facts.targetIndexPresent && facts.targetIndexRecords == 0 && facts.targetIndexBytes == 0)
	return total == facts.finalObjectBytes && targetStateExact
}

func (facts rootQuotaUsageFacts) exceedsLimits() bool {
	if facts.finalObjectBytes > rootFinalObjectMaximumBytes || uint64(len(facts.finalObjects)) > rootFinalObjectMaximumCount || facts.tempCount > rootTempMaximumCount || facts.tempBytes > rootTempMaximumTotalBytes || facts.largestTempBytes > rootTempMaximumEachBytes || facts.journalCount > rootJournalMaximumCount || facts.journalReservedBytes > rootJournalMaximumBytes || facts.indexCount > rootIndexMaximumCount || facts.indexActualBytes > rootIndexMaximumBytes || facts.targetIndexRecords > lineageIndexMaximumRecords || facts.targetIndexBytes > lineageIndexMaximumBytes {
		return true
	}
	for _, object := range facts.finalObjects {
		if object.size > rootTempMaximumEachBytes {
			return true
		}
	}
	return false
}

func bindVerifiedRootQuotaState(*evidenceLineageLock, verifiedRootQuotaScan) (*verifiedRootQuotaState, error) {
	return nil, fail(CodeProjectionNotImplemented, "evidence-quota-root-scan", "strict root quota scanner and replay authority are not implemented", nil)
}

func (state *verifiedRootQuotaState) snapshot() (rootQuotaUsageFacts, error) {
	if state == nil || !validRootQuotaLock(state.lock) || state.canonical != rootQuotaFactsDigest(state.facts) || !state.facts.valid() {
		return rootQuotaUsageFacts{}, filesystemFailure("quota-root", "strict root usage authority is invalid")
	}
	owned := state.facts
	owned.finalObjects = append([]rootQuotaObjectFact(nil), state.facts.finalObjects...)
	return owned, nil
}

func validRootQuotaLock(lock *evidenceLineageLock) bool {
	if lock == nil || lock.self != lock || lock.done || lock.rootReleased || lock.root == nil || !lock.root.Active() || !lock.lineageHeld || !validLockFile(lock.lineage, evidenceLineageLockKind) {
		return false
	}
	root, ok := lock.root.(*evidenceFSRootLease)
	if !ok || root.self != root {
		return false
	}
	publicationLease := root.publicationLease()
	return publicationLease != nil && publicationLease.Active()
}

func (bundle *RuntimeBundle) quotaFactsForAdmission() (verifiedQuotaBundleFacts, error) {
	if bundle == nil {
		return verifiedQuotaBundleFacts{}, fail(CodeUntrusted, "evidence-quota", "verified runtime bundle is unavailable", nil)
	}
	manifest, files, err := bundle.ownedInputs.copyVerified()
	if err != nil {
		return verifiedQuotaBundleFacts{}, err
	}
	if !bundle.quotaFacts.valid() || bundle.quotaFacts.runtimeInputs != bundle.ownedInputs.canonical || bundle.quotaFacts.schemaBundleDigest != manifest.SchemaBundleDigest || bundle.quotaFacts.maxAttempts != manifest.ExecutionPolicy.MaxAttempts || bundle.quotaFacts.outerArtifactDigest != bundle.ownedInputs.outerArtifactDigest || bundle.quotaFacts.outerArtifactSize != bundle.ownedInputs.outerArtifactSize {
		return verifiedQuotaBundleFacts{}, fail(CodeUntrusted, "evidence-quota", "bundle quota authority differs from owned runtime inputs", nil)
	}
	rebound, err := bindVerifiedQuotaBundleFacts(manifest, files, bundle.ownedInputs.canonical, bundle.ownedInputs.outerArtifactDigest, bundle.ownedInputs.outerArtifactSize)
	if err != nil || rebound.canonical != bundle.quotaFacts.canonical {
		return verifiedQuotaBundleFacts{}, fail(CodeUntrusted, "evidence-quota", "bundle quota authority cannot be reproduced from owned runtime inputs", err)
	}
	owned := bundle.quotaFacts
	owned.statementCounts = append([]uint64(nil), bundle.quotaFacts.statementCounts...)
	return owned, nil
}

func admitRootQuota(state *verifiedRootQuotaState, bundle *RuntimeBundle, candidate OwnedCurrentCandidate) (rootQuotaAdmission, error) {
	rootFacts, err := state.snapshot()
	if err != nil || state.consumed.Load() {
		if err != nil {
			return rootQuotaAdmission{}, err
		}
		return rootQuotaAdmission{}, filesystemFailure("quota-root", "strict root usage authority was already consumed")
	}
	admission, err := calculateRootQuotaAdmission(rootFacts, bundle, candidate)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	if !state.consumed.CompareAndSwap(false, true) {
		return rootQuotaAdmission{}, filesystemFailure("quota-root", "strict root usage authority was already consumed")
	}
	return admission, nil
}

// calculateRootQuotaAdmission contains the pure quota calculation after a
// caller has obtained exact root facts from an authoritative snapshot.
func calculateRootQuotaAdmission(rootFacts rootQuotaUsageFacts, bundle *RuntimeBundle, candidate OwnedCurrentCandidate) (rootQuotaAdmission, error) {
	if !rootFacts.valid() {
		return rootQuotaAdmission{}, filesystemFailure("quota-root", "root quota facts are invalid")
	}
	facts, err := bundle.quotaFactsForAdmission()
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	reservation, err := calculateEvidenceQuotaReservationForFacts(facts, rootFacts)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	runtime, recovery, err := quotaCandidateArtifacts(facts, candidate)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	runtimeObject, err := quotaRuntimeObject(facts, runtime)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	recoveryObject, err := quotaRecoveryObject(runtime.owner, recovery)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	candidates := []rootQuotaObjectFact{runtimeObject, recoveryObject}
	objects := make(map[Digest]uint64, len(rootFacts.finalObjects)+len(candidates))
	for _, object := range rootFacts.finalObjects {
		objects[object.digest] = object.size
	}
	objectBytes := rootFacts.finalObjectBytes
	for _, candidate := range candidates {
		if size, exists := objects[candidate.digest]; exists {
			if size != candidate.size {
				return rootQuotaAdmission{}, quotaLimit("duplicate object digest has a different exact size")
			}
			continue
		}
		objects[candidate.digest] = candidate.size
		var err error
		objectBytes, err = quotaAddValue(objectBytes, candidate.size)
		if err != nil {
			return rootQuotaAdmission{}, err
		}
	}
	journalCount, err := quotaAddValue(rootFacts.journalCount, 1)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	// Journal and index budgets are independent physical components. Adding the
	// combined reservation here would debit ReservedIndexBytes a second time
	// when indexBytes is calculated below.
	journalBytes, err := quotaAddValue(rootFacts.journalReservedBytes, reservation.ReservedJournalBytes)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	indexCount := rootFacts.indexCount
	if !rootFacts.targetIndexPresent {
		indexCount, err = quotaAddValue(indexCount, 1)
		if err != nil {
			return rootQuotaAdmission{}, err
		}
	}
	indexBytes, err := quotaAddValue(rootFacts.indexActualBytes, reservation.ReservedIndexBytes)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	targetRecords, err := quotaAddValue(rootFacts.targetIndexRecords, reservation.ReservedIndexRecords)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	targetBytes, err := quotaAddValue(rootFacts.targetIndexBytes, reservation.ReservedIndexBytes)
	if err != nil {
		return rootQuotaAdmission{}, err
	}
	if uint64(len(objects)) > rootFinalObjectMaximumCount || objectBytes > rootFinalObjectMaximumBytes || journalCount > rootJournalMaximumCount || journalBytes > rootJournalMaximumBytes || indexCount > rootIndexMaximumCount || indexBytes > rootIndexMaximumBytes || targetRecords > lineageIndexMaximumRecords || targetBytes > lineageIndexMaximumBytes {
		return rootQuotaAdmission{}, quotaLimit("root admission exceeds a fixed inclusive maximum")
	}
	return rootQuotaAdmission{uint64(len(objects)), objectBytes, journalCount, journalBytes, indexCount, indexBytes, targetRecords, targetBytes}, nil
}

// quotaCandidateArtifacts accepts only the total three-way authority minted by
// bindVerifiedEvidenceRun. No structural literal or partial overlay is an
// admissible quota candidate.
func quotaCandidateArtifacts(facts verifiedQuotaBundleFacts, candidate OwnedCurrentCandidate) (VerifiedRuntimeArtifact, VerifiedDecisionRecoveryArtifact, error) {
	if !validOwnedCurrentCandidate(candidate) {
		return VerifiedRuntimeArtifact{}, VerifiedDecisionRecoveryArtifact{}, fail(CodeEvidenceRecoveryRequired, "evidence-quota-candidate", "current candidate was not minted by the total evidence-run binder", nil)
	}
	run := candidate.verifiedRun
	runtime := candidate.runtimeArtifact
	recovery := candidate.decisionRecoveryArtifact
	boundRecovery := run.decisionRecoveryArtifact
	if run.outerArtifactDigest != facts.outerArtifactDigest || run.outerArtifactSizeBytes != facts.outerArtifactSize || run.schemaBundleDigest != facts.schemaBundleDigest || requireDigest("quota.run.runner_projection_decision", run.runnerProjectionDecisionDigest) != nil || recovery.decision != run.runnerProjectionDecisionDigest || boundRecovery.decision != recovery.decision {
		return VerifiedRuntimeArtifact{}, VerifiedDecisionRecoveryArtifact{}, fail(CodeEvidenceRecoveryRequired, "evidence-quota-candidate", "current candidate runtime and recovery authority are not totally bound", nil)
	}
	return runtime, recovery, nil
}

func quotaRuntimeObject(facts verifiedQuotaBundleFacts, artifact VerifiedRuntimeArtifact) (rootQuotaObjectFact, error) {
	if artifact.owner == nil || requireDigest("quota.runtime", artifact.digest) != nil || artifact.digest != facts.outerArtifactDigest || artifact.sizeBytes != facts.outerArtifactSize || artifact.sizeBytes == 0 || artifact.sizeBytes > maxRuntimeTarSize || uint64(len(artifact.bytes)) != artifact.sizeBytes || DigestBytes(artifact.bytes) != artifact.digest {
		return rootQuotaObjectFact{}, fail(CodeUntrusted, "evidence-quota-runtime", "verified runtime artifact is not exact or exceeds 64 MiB", nil)
	}
	return rootQuotaObjectFact{digest: artifact.digest, size: artifact.sizeBytes}, nil
}

func quotaRecoveryObject(runtimeOwner *evidenceOwnerToken, artifact VerifiedDecisionRecoveryArtifact) (rootQuotaObjectFact, error) {
	if runtimeOwner == nil || artifact.owner == nil || artifact.owner.token != runtimeOwner || requireDigest("quota.recovery", artifact.digest) != nil || requireDigest("quota.recovery.decision", artifact.decision) != nil || artifact.sizeBytes == 0 || artifact.sizeBytes > maxDecisionRecoveryArtifactBytes || uint64(len(artifact.bytes)) != artifact.sizeBytes || DigestBytes(artifact.bytes) != artifact.digest {
		return rootQuotaObjectFact{}, fail(CodeEvidenceRecoveryRequired, "evidence-quota-recovery", "verified recovery artifact is not exact or exceeds 4 MiB", nil)
	}
	return rootQuotaObjectFact{digest: artifact.digest, size: artifact.sizeBytes}, nil
}

func quotaLimit(message string) error {
	return fail(CodeEvidenceJournalLimitExceeded, "evidence-quota", message, nil)
}
func quotaAdd(a, b uint64) (uint64, bool) { value := a + b; return value, value < a }
func quotaMul(a, b uint64) (uint64, bool) {
	if a != 0 && b > ^uint64(0)/a {
		return 0, true
	}
	return a * b, false
}
func quotaAddValue(a, b uint64) (uint64, error) {
	value, overflow := quotaAdd(a, b)
	if overflow {
		return 0, quotaLimit("quota arithmetic overflow")
	}
	return value, nil
}

func canonicalRootObjects(objects []rootQuotaObjectFact) []rootQuotaObjectFact {
	copyObjects := append([]rootQuotaObjectFact(nil), objects...)
	sort.Slice(copyObjects, func(i, j int) bool { return copyObjects[i].digest < copyObjects[j].digest })
	return copyObjects
}
