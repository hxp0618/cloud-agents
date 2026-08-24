package migration

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
)

const (
	EvidenceJournalFormat                = "cloud-agents-platform-evidence-journal/v1"
	EvidenceFrameFormat                  = "cloud-agents-platform-evidence-journal-frame/v1"
	EvidenceLimitsProfile                = "cloud-agents-platform-evidence-journal-limits/v1"
	LineageQuotaProfileV2                = "cloud-agents-platform-lineage-quota-profile/v2"
	LineageQuotaProfileV3                = "cloud-agents-platform-lineage-quota-profile/v3"
	LineageQuotaProfileV4                = "cloud-agents-platform-lineage-quota-profile/v4"
	LineageIndexFormat                   = "cloud-agents-platform-lineage-index/v1"
	LineageFrameFormat                   = "cloud-agents-platform-lineage-index-frame/v1"
	LineageLimitsProfile                 = "cloud-agents-platform-lineage-index-limits/v1"
	EvidenceRecordDigestDomain           = "cloud-agents-platform-evidence-journal-record/v1"
	LineageRecordDigestDomain            = "cloud-agents-platform-lineage-index-record/v1"
	JournalIdentityDigestDomain          = "cloud-agents-platform-evidence-journal-identity/v1"
	QuotaReservationDigestDomain         = "cloud-agents-platform-evidence-quota-reservation/v1"
	QuotaReservationDigestDomainV2       = "cloud-agents-platform-evidence-quota-reservation/v2"
	QuotaReservationDigestDomainV3       = "cloud-agents-platform-evidence-quota-reservation/v3"
	QuotaReservationDigestDomainV4       = "cloud-agents-platform-evidence-quota-reservation/v4"
	AmbiguousResolutionDigestDomain      = "cloud-agents-platform-ambiguous-resolution/v1"
	LedgerPrefixDigestDomain             = "cloud-agents-platform-ledger-prefix/v1"
	ExecutionLineageDigestDomain         = "cloud-agents-platform-execution-lineage/v1"
	maxEvidenceFrameBytes                = uint64(1 << 20)
	maxLineageFrameBytes                 = uint64(256 << 10)
	maxEvidenceReservedRecords           = uint64(16 * 4096)
	maxEvidenceReservedBytes             = uint64(16 * 16 << 20)
	maxEvidenceReservedSegments          = uint32(16)
	maxSupportedEvidenceReservedBytes    = uint64(32 * 16 << 20)
	maxSupportedEvidenceReservedSegments = uint32(32)
	maxV4EvidenceReservedBytes           = maxSupportedEvidenceReservedBytes + uint64(16<<20)
	maxDecisionRecoveryArtifactBytes     = uint64(4 << 20)
	v2GenerationCheckpointMaximum        = uint64(4 << 10)
)

type evidenceQuotaProfileLimits struct {
	maxRecords    uint64
	maxBytes      uint64
	maxSegments   uint32
	maxCheckpoint uint64
}

func evidenceQuotaLimitsForProfile(profile string) (evidenceQuotaProfileLimits, error) {
	switch profile {
	case EvidenceLimitsProfile:
		return evidenceQuotaProfileLimits{
			maxRecords: maxEvidenceReservedRecords, maxBytes: maxEvidenceReservedBytes,
			maxSegments: maxEvidenceReservedSegments, maxCheckpoint: lineageRecordFrameLimits[LineageRecordGenerationCheckpoint],
		}, nil
	case LineageQuotaProfileV2:
		return evidenceQuotaProfileLimits{
			maxRecords: maxEvidenceReservedRecords, maxBytes: maxEvidenceReservedBytes,
			maxSegments: maxEvidenceReservedSegments, maxCheckpoint: v2GenerationCheckpointMaximum,
		}, nil
	case LineageQuotaProfileV3:
		return evidenceQuotaProfileLimits{
			maxRecords: maxEvidenceReservedRecords, maxBytes: maxSupportedEvidenceReservedBytes,
			maxSegments: maxSupportedEvidenceReservedSegments, maxCheckpoint: v2GenerationCheckpointMaximum,
		}, nil
	case LineageQuotaProfileV4:
		return evidenceQuotaProfileLimits{
			maxRecords: maxEvidenceReservedRecords, maxBytes: maxV4EvidenceReservedBytes,
			maxSegments: maxSupportedEvidenceReservedSegments, maxCheckpoint: v2GenerationCheckpointMaximum,
		}, nil
	default:
		return evidenceQuotaProfileLimits{}, invalidEvidence("quota-profile", "unknown profile")
	}
}

var evidenceRecordFrameLimits = map[EvidenceRecordKind]uint64{
	EvidenceRecordHeader:              32 << 10,
	EvidenceRecordStatementIntent:     64 << 10,
	EvidenceRecordIntermediate:        256 << 10,
	EvidenceRecordCommitIntent:        64 << 10,
	EvidenceRecordAttemptTerminal:     64 << 10,
	EvidenceRecordAmbiguousResolution: 64 << 10,
}

var lineageRecordFrameLimits = map[LineageRecordKind]uint64{
	LineageRecordHeader:               32 << 10,
	LineageRecordGenerationReserved:   64 << 10,
	LineageRecordGenerationActivated:  64 << 10,
	LineageRecordGenerationCheckpoint: 16 << 10,
	LineageRecordGenerationSuperseded: 128 << 10,
}

type EvidenceRecordKind string

const (
	EvidenceRecordHeader              EvidenceRecordKind = "header"
	EvidenceRecordStatementIntent     EvidenceRecordKind = "statement_intent"
	EvidenceRecordIntermediate        EvidenceRecordKind = "intermediate"
	EvidenceRecordCommitIntent        EvidenceRecordKind = "commit_intent"
	EvidenceRecordAttemptTerminal     EvidenceRecordKind = "attempt_terminal"
	EvidenceRecordAmbiguousResolution EvidenceRecordKind = "ambiguous_resolution"
)

type LineageRecordKind string

const (
	LineageRecordHeader               LineageRecordKind = "header"
	LineageRecordGenerationReserved   LineageRecordKind = "generation_reserved"
	LineageRecordGenerationActivated  LineageRecordKind = "generation_activated"
	LineageRecordGenerationCheckpoint LineageRecordKind = "generation_checkpoint"
	LineageRecordGenerationSuperseded LineageRecordKind = "generation_superseded"
)

type StableFailureEvidence struct {
	Code           ErrorCode `json:"code"`
	ProjectionKind *string   `json:"projection_kind"`
	Phase          string    `json:"phase"`
	Path           string    `json:"path"`
	Major          *uint16   `json:"major"`
	Retryable      bool      `json:"retryable"`
}

type RetryProofEvidence struct {
	ProofKind                       string  `json:"proof_kind"`
	AttemptPredecessorCatalogDigest Digest  `json:"attempt_predecessor_catalog_digest"`
	ObservedCatalogDigest           Digest  `json:"observed_catalog_digest"`
	LedgerPrefixDigest              Digest  `json:"ledger_prefix_digest"`
	AuthorityResultDigest           Digest  `json:"authority_result_digest"`
	CommitRejectedReason            *string `json:"commit_rejected_reason"`
}

type ProjectionResultEvidence struct {
	Digest   Digest             `json:"digest"`
	Metadata ProjectionMetadata `json:"metadata"`
}

type JournalHeader struct {
	FormatVersion                     string  `json:"format_version"`
	JournalIdentityDigest             Digest  `json:"journal_identity_digest"`
	ReleaseTrustDecisionDigest        Digest  `json:"release_trust_decision_digest"`
	RunnerProjectionDecisionDigest    Digest  `json:"runner_projection_decision_digest"`
	ExecutionLineageDigest            Digest  `json:"execution_lineage_digest"`
	OuterArtifactDigest               Digest  `json:"outer_artifact_digest"`
	OuterArtifactSizeBytes            uint64  `json:"outer_artifact_size_bytes"`
	DecisionRecoveryArtifactSHA256    Digest  `json:"decision_recovery_artifact_sha256"`
	DecisionRecoveryArtifactSizeBytes uint64  `json:"decision_recovery_artifact_size_bytes"`
	ManifestDigest                    Digest  `json:"manifest_digest"`
	RunnerReleaseDigest               Digest  `json:"runner_release_digest"`
	SchemaBundleDigest                Digest  `json:"schema_bundle_digest"`
	AuthorityProfileDigest            Digest  `json:"authority_profile_digest"`
	AuthorityBindingDigest            Digest  `json:"authority_binding_digest"`
	SegmentIndex                      uint32  `json:"segment_index"`
	PreviousSegmentRecordDigest       *Digest `json:"previous_segment_record_digest"`
	LimitsProfile                     string  `json:"limits_profile"`
	QuotaReservationDigest            Digest  `json:"quota_reservation_digest"`
	ReservedRecords                   uint64  `json:"reserved_records"`
	ReservedBytes                     uint64  `json:"reserved_bytes"`
	ReservedSegments                  uint32  `json:"reserved_segments"`
}

type StatementIntent struct {
	SchemaBundleDigest              Digest                      `json:"schema_bundle_digest"`
	CatalogContractDigest           Digest                      `json:"catalog_contract_digest"`
	AuthorityProfileDigest          Digest                      `json:"authority_profile_digest"`
	AuthorityBindingDigest          Digest                      `json:"authority_binding_digest"`
	MigrationID                     string                      `json:"migration_id"`
	AttemptIndex                    uint32                      `json:"attempt_index"`
	StatementIndex                  uint32                      `json:"statement_index"`
	SQLPath                         string                      `json:"sql_path"`
	SQLArtifactSHA256               Digest                      `json:"sql_artifact_sha256"`
	SQLArtifactSizeBytes            uint64                      `json:"sql_artifact_size_bytes"`
	StartOffset                     uint64                      `json:"start_offset"`
	EndOffset                       uint64                      `json:"end_offset"`
	StatementSHA256                 Digest                      `json:"statement_sha256"`
	Classification                  SQLClassificationDescriptor `json:"classification"`
	PreviousAttemptTerminalDigest   *Digest                     `json:"previous_attempt_terminal_digest"`
	PreviousIntermediateStateDigest *Digest                     `json:"previous_intermediate_state_digest"`
	ExpectedTransitionDigest        Digest                      `json:"expected_transition_digest"`
	AuthorityBeforeDigest           Digest                      `json:"authority_before_digest"`
	CatalogBeforeDigest             Digest                      `json:"catalog_before_digest"`
	AuthorityBeforeResult           ProjectionResultEvidence    `json:"authority_before_result"`
	CatalogBeforeResult             ProjectionResultEvidence    `json:"catalog_before_result"`
}

type StatementIntermediateEvidence struct {
	State                    StatementIntermediateState `json:"state"`
	AuthorityBeforeResult    ProjectionResultEvidence   `json:"authority_before_result"`
	CatalogBeforeResult      ProjectionResultEvidence   `json:"catalog_before_result"`
	AuthorityAfterResult     ProjectionResultEvidence   `json:"authority_after_result"`
	CatalogAfterResult       ProjectionResultEvidence   `json:"catalog_after_result"`
	PreledgerAuthorityResult *ProjectionResultEvidence  `json:"preledger_authority_result"`
	PreledgerCatalogResult   *ProjectionResultEvidence  `json:"preledger_catalog_result"`
}

type CommitIntentLedgerRow struct {
	MigrationID                   string  `json:"migration_id"`
	MigrationName                 string  `json:"migration_name"`
	PredecessorID                 *string `json:"predecessor_id"`
	Phase                         string  `json:"phase"`
	SchemaFrom                    string  `json:"schema_from"`
	SchemaTo                      string  `json:"schema_to"`
	CompatibleBinaryMin           string  `json:"compatible_binary_min"`
	CompatibleBinaryMax           string  `json:"compatible_binary_max"`
	SQLPath                       string  `json:"sql_path"`
	SQLSizeBytes                  uint64  `json:"sql_size_bytes"`
	SQLSHA256                     Digest  `json:"sql_sha256"`
	BundleDigest                  Digest  `json:"bundle_digest"`
	TransactionMode               string  `json:"transaction_mode"`
	Reentrancy                    string  `json:"reentrancy"`
	RollbackBoundary              string  `json:"rollback_boundary"`
	RequiresLiveInstancePreflight bool    `json:"requires_live_instance_preflight"`
	RequiresPITRPreflight         bool    `json:"requires_pitr_preflight"`
}

type CommitIntent struct {
	SchemaBundleDigest              Digest                `json:"schema_bundle_digest"`
	CatalogContractDigest           Digest                `json:"catalog_contract_digest"`
	AuthorityProfileDigest          Digest                `json:"authority_profile_digest"`
	AuthorityBindingDigest          Digest                `json:"authority_binding_digest"`
	MigrationID                     string                `json:"migration_id"`
	AttemptIndex                    uint32                `json:"attempt_index"`
	PreviousAttemptTerminalDigest   *Digest               `json:"previous_attempt_terminal_digest"`
	AttemptPredecessorCatalogDigest Digest                `json:"attempt_predecessor_catalog_digest"`
	LastIntermediateStateDigest     Digest                `json:"last_intermediate_state_digest"`
	ExpectedLedgerLength            uint32                `json:"expected_ledger_length"`
	ExpectedLedgerHead              string                `json:"expected_ledger_head"`
	LedgerRow                       CommitIntentLedgerRow `json:"ledger_row"`
}

type AmbiguousResolutionState struct {
	SchemaBundleDigest       Digest    `json:"schema_bundle_digest"`
	CatalogContractDigest    Digest    `json:"catalog_contract_digest"`
	AuthorityProfileDigest   Digest    `json:"authority_profile_digest"`
	AuthorityBindingDigest   Digest    `json:"authority_binding_digest"`
	MigrationID              string    `json:"migration_id"`
	AttemptIndex             uint32    `json:"attempt_index"`
	UnresolvedTerminalDigest Digest    `json:"unresolved_terminal_digest"`
	Outcome                  string    `json:"outcome"`
	ReconcileResult          string    `json:"reconcile_result"`
	StableErrorCode          ErrorCode `json:"stable_error_code"`
	ResolutionDigest         Digest    `json:"resolution_digest"`
}

type EvidenceRecord struct {
	Header              *JournalHeader
	StatementIntent     *StatementIntent
	Intermediate        *StatementIntermediateEvidence
	CommitIntent        *CommitIntent
	AttemptTerminal     *AttemptTerminalState
	AmbiguousResolution *AmbiguousResolutionState
}

type EvidenceFrame struct {
	FormatVersion        string             `json:"format_version"`
	Sequence             uint64             `json:"sequence"`
	PreviousRecordDigest *Digest            `json:"previous_record_digest"`
	RecordKind           EvidenceRecordKind `json:"record_kind"`
	Record               EvidenceRecord     `json:"record"`
	RecordDigest         Digest             `json:"record_digest"`
}

type LineageExpectedDatabaseIdentity struct {
	DatabaseName string `json:"database_name"`
}

type LineageIndexHeader struct {
	FormatVersion            string                          `json:"format_version"`
	ExecutionLineageDigest   Digest                          `json:"execution_lineage_digest"`
	DeploymentID             string                          `json:"deployment_id"`
	ExpectedDatabaseIdentity LineageExpectedDatabaseIdentity `json:"expected_database_identity"`
	RepositoryIdentity       string                          `json:"repository_identity"`
	LimitsProfile            string                          `json:"limits_profile"`
}

type LineageContinuationContext struct {
	StartAction                   string  `json:"start_action"`
	MigrationID                   string  `json:"migration_id"`
	AttemptIndex                  uint32  `json:"attempt_index"`
	PreviousAttemptTerminalDigest *Digest `json:"previous_attempt_terminal_digest"`
	SourceJournalIdentityDigest   Digest  `json:"source_journal_identity_digest"`
	SourceCheckpointRecordDigest  Digest  `json:"source_checkpoint_record_digest"`
	SourceTerminalDigest          Digest  `json:"source_terminal_digest"`
}

type GenerationReserved struct {
	ExecutionLineageDigest         Digest                      `json:"execution_lineage_digest"`
	JournalIdentityDigest          Digest                      `json:"journal_identity_digest"`
	RunnerProjectionDecisionDigest Digest                      `json:"runner_projection_decision_digest"`
	SchemaBundleDigest             Digest                      `json:"schema_bundle_digest"`
	QuotaReservationDigest         Digest                      `json:"quota_reservation_digest"`
	ReservedRecords                uint64                      `json:"reserved_records"`
	ReservedBytes                  uint64                      `json:"reserved_bytes"`
	ReservedSegments               uint32                      `json:"reserved_segments"`
	PlannedSegment0Header          JournalHeader               `json:"planned_segment0_header"`
	ExpectedSegment0HeaderDigest   Digest                      `json:"expected_segment0_header_digest"`
	Continuation                   *LineageContinuationContext `json:"continuation"`
}

type GenerationActivated struct {
	ExecutionLineageDigest         Digest `json:"execution_lineage_digest"`
	JournalIdentityDigest          Digest `json:"journal_identity_digest"`
	RunnerProjectionDecisionDigest Digest `json:"runner_projection_decision_digest"`
	SchemaBundleDigest             Digest `json:"schema_bundle_digest"`
	QuotaReservationDigest         Digest `json:"quota_reservation_digest"`
	GenerationReservedRecordDigest Digest `json:"generation_reserved_record_digest"`
	Segment0HeaderDigest           Digest `json:"segment0_header_digest"`
	InitialJournalTailDigest       Digest `json:"initial_journal_tail_digest"`
}

type GenerationCheckpoint struct {
	ExecutionLineageDigest               Digest  `json:"execution_lineage_digest"`
	JournalIdentityDigest                Digest  `json:"journal_identity_digest"`
	RunnerProjectionDecisionDigest       Digest  `json:"runner_projection_decision_digest"`
	SchemaBundleDigest                   Digest  `json:"schema_bundle_digest"`
	JournalNextSequence                  uint64  `json:"journal_next_sequence"`
	JournalTailDigest                    Digest  `json:"journal_tail_digest"`
	RecoveryState                        string  `json:"recovery_state"`
	MigrationID                          *string `json:"migration_id"`
	AttemptIndex                         *uint32 `json:"attempt_index"`
	LastStatementIntentRecordDigest      *Digest `json:"last_statement_intent_record_digest"`
	LastIntermediateEvidenceRecordDigest *Digest `json:"last_intermediate_evidence_record_digest"`
	LastCommitIntentRecordDigest         *Digest `json:"last_commit_intent_record_digest"`
	LastTerminalDigest                   *Digest `json:"last_terminal_digest"`
	LastResolutionDigest                 *Digest `json:"last_resolution_digest"`
	PreviousAttemptTerminalDigest        *Digest `json:"previous_attempt_terminal_digest"`
	LastIntermediateStateDigest          *Digest `json:"last_intermediate_state_digest"`
	PreviousCheckpointRecordDigest       *Digest `json:"previous_checkpoint_record_digest"`
}

type GenerationSuperseded struct {
	ExecutionLineageDigest             Digest              `json:"execution_lineage_digest"`
	OldJournalIdentityDigest           Digest              `json:"old_journal_identity_digest"`
	OldRunnerProjectionDecisionDigest  Digest              `json:"old_runner_projection_decision_digest"`
	OldSchemaBundleDigest              Digest              `json:"old_schema_bundle_digest"`
	OldCheckpointRecordDigest          *Digest             `json:"old_checkpoint_record_digest"`
	OldActivationRecordDigest          *Digest             `json:"old_activation_record_digest"`
	OldInitialJournalTailDigest        *Digest             `json:"old_initial_journal_tail_digest"`
	LineageSupersessionAuthorityDigest Digest              `json:"lineage_supersession_authority_digest"`
	Outcome                            string              `json:"outcome"`
	PlannedGenerationReserved          *GenerationReserved `json:"planned_generation_reserved"`
}

type LineageIndexRecord struct {
	Header     *LineageIndexHeader
	Reserved   *GenerationReserved
	Activated  *GenerationActivated
	Checkpoint *GenerationCheckpoint
	Superseded *GenerationSuperseded
}

type LineageIndexFrame struct {
	FormatVersion        string             `json:"format_version"`
	Sequence             uint64             `json:"sequence"`
	PreviousRecordDigest *Digest            `json:"previous_record_digest"`
	RecordKind           LineageRecordKind  `json:"record_kind"`
	Record               LineageIndexRecord `json:"record"`
	RecordDigest         Digest             `json:"record_digest"`
}

func (record EvidenceRecord) branch() (any, error) {
	branches := []any{record.Header, record.StatementIntent, record.Intermediate, record.CommitIntent, record.AttemptTerminal, record.AmbiguousResolution}
	var selected any
	for _, branch := range branches {
		if !isNilEvidenceBranch(branch) {
			if selected != nil {
				return nil, invalidEvidence("record", "multiple union branches")
			}
			selected = branch
		}
	}
	if selected == nil {
		return nil, invalidEvidence("record", "missing union branch")
	}
	return selected, nil
}

func isNilEvidenceBranch(value any) bool {
	switch v := value.(type) {
	case *JournalHeader:
		return v == nil
	case *StatementIntent:
		return v == nil
	case *StatementIntermediateEvidence:
		return v == nil
	case *CommitIntent:
		return v == nil
	case *AttemptTerminalState:
		return v == nil
	case *AmbiguousResolutionState:
		return v == nil
	case *LineageIndexHeader:
		return v == nil
	case *GenerationReserved:
		return v == nil
	case *GenerationActivated:
		return v == nil
	case *GenerationCheckpoint:
		return v == nil
	case *GenerationSuperseded:
		return v == nil
	default:
		return value == nil
	}
}

func (record EvidenceRecord) MarshalJSON() ([]byte, error) {
	branch, err := record.branch()
	if err != nil {
		return nil, err
	}
	return json.Marshal(branch)
}

func (record LineageIndexRecord) branch() (any, error) {
	branches := []any{record.Header, record.Reserved, record.Activated, record.Checkpoint, record.Superseded}
	var selected any
	for _, branch := range branches {
		if !isNilEvidenceBranch(branch) {
			if selected != nil {
				return nil, invalidEvidence("lineage-record", "multiple union branches")
			}
			selected = branch
		}
	}
	if selected == nil {
		return nil, invalidEvidence("lineage-record", "missing union branch")
	}
	return selected, nil
}
func (record LineageIndexRecord) MarshalJSON() ([]byte, error) {
	branch, err := record.branch()
	if err != nil {
		return nil, err
	}
	return json.Marshal(branch)
}

type rawEvidenceFrame struct {
	FormatVersion        string             `json:"format_version"`
	Sequence             uint64             `json:"sequence"`
	PreviousRecordDigest *Digest            `json:"previous_record_digest"`
	RecordKind           EvidenceRecordKind `json:"record_kind"`
	Record               json.RawMessage    `json:"record"`
	RecordDigest         Digest             `json:"record_digest"`
}

func (frame *EvidenceFrame) UnmarshalJSON(data []byte) error {
	var raw rawEvidenceFrame
	if _, err := decodeStrictShape(data, &raw); err != nil {
		return err
	}
	record, err := decodeEvidenceRecord(raw.RecordKind, raw.Record)
	if err != nil {
		return err
	}
	*frame = EvidenceFrame{raw.FormatVersion, raw.Sequence, raw.PreviousRecordDigest, raw.RecordKind, record, raw.RecordDigest}
	return nil
}

func (frame EvidenceFrame) MarshalJSON() ([]byte, error) {
	branch, err := frame.Record.branch()
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		FormatVersion        string             `json:"format_version"`
		Sequence             uint64             `json:"sequence"`
		PreviousRecordDigest *Digest            `json:"previous_record_digest"`
		RecordKind           EvidenceRecordKind `json:"record_kind"`
		Record               any                `json:"record"`
		RecordDigest         Digest             `json:"record_digest"`
	}{frame.FormatVersion, frame.Sequence, frame.PreviousRecordDigest, frame.RecordKind, branch, frame.RecordDigest})
}

func decodeEvidenceRecord(kind EvidenceRecordKind, raw []byte) (EvidenceRecord, error) {
	switch kind {
	case EvidenceRecordHeader:
		var v JournalHeader
		_, e := decodeStrictShape(raw, &v)
		return EvidenceRecord{Header: &v}, e
	case EvidenceRecordStatementIntent:
		var v StatementIntent
		_, e := decodeStrictShape(raw, &v)
		return EvidenceRecord{StatementIntent: &v}, e
	case EvidenceRecordIntermediate:
		var v StatementIntermediateEvidence
		_, e := decodeStrictShape(raw, &v)
		return EvidenceRecord{Intermediate: &v}, e
	case EvidenceRecordCommitIntent:
		var v CommitIntent
		_, e := decodeStrictShape(raw, &v)
		return EvidenceRecord{CommitIntent: &v}, e
	case EvidenceRecordAttemptTerminal:
		var v AttemptTerminalState
		_, e := decodeStrictShape(raw, &v)
		return EvidenceRecord{AttemptTerminal: &v}, e
	case EvidenceRecordAmbiguousResolution:
		var v AmbiguousResolutionState
		_, e := decodeStrictShape(raw, &v)
		return EvidenceRecord{AmbiguousResolution: &v}, e
	default:
		return EvidenceRecord{}, invalidEvidence("record-kind", "unknown evidence record kind")
	}
}

type rawLineageFrame struct {
	FormatVersion        string            `json:"format_version"`
	Sequence             uint64            `json:"sequence"`
	PreviousRecordDigest *Digest           `json:"previous_record_digest"`
	RecordKind           LineageRecordKind `json:"record_kind"`
	Record               json.RawMessage   `json:"record"`
	RecordDigest         Digest            `json:"record_digest"`
}

func (frame *LineageIndexFrame) UnmarshalJSON(data []byte) error {
	var raw rawLineageFrame
	if _, e := decodeStrictShape(data, &raw); e != nil {
		return e
	}
	record, e := decodeLineageRecord(raw.RecordKind, raw.Record)
	if e != nil {
		return e
	}
	*frame = LineageIndexFrame{raw.FormatVersion, raw.Sequence, raw.PreviousRecordDigest, raw.RecordKind, record, raw.RecordDigest}
	return nil
}
func (frame LineageIndexFrame) MarshalJSON() ([]byte, error) {
	branch, e := frame.Record.branch()
	if e != nil {
		return nil, e
	}
	return json.Marshal(struct {
		FormatVersion        string            `json:"format_version"`
		Sequence             uint64            `json:"sequence"`
		PreviousRecordDigest *Digest           `json:"previous_record_digest"`
		RecordKind           LineageRecordKind `json:"record_kind"`
		Record               any               `json:"record"`
		RecordDigest         Digest            `json:"record_digest"`
	}{frame.FormatVersion, frame.Sequence, frame.PreviousRecordDigest, frame.RecordKind, branch, frame.RecordDigest})
}
func decodeLineageRecord(kind LineageRecordKind, raw []byte) (LineageIndexRecord, error) {
	switch kind {
	case LineageRecordHeader:
		var v LineageIndexHeader
		_, e := decodeStrictShape(raw, &v)
		return LineageIndexRecord{Header: &v}, e
	case LineageRecordGenerationReserved:
		var v GenerationReserved
		_, e := decodeStrictShape(raw, &v)
		return LineageIndexRecord{Reserved: &v}, e
	case LineageRecordGenerationActivated:
		var v GenerationActivated
		_, e := decodeStrictShape(raw, &v)
		return LineageIndexRecord{Activated: &v}, e
	case LineageRecordGenerationCheckpoint:
		var v GenerationCheckpoint
		_, e := decodeStrictShape(raw, &v)
		return LineageIndexRecord{Checkpoint: &v}, e
	case LineageRecordGenerationSuperseded:
		var v GenerationSuperseded
		_, e := decodeStrictShape(raw, &v)
		return LineageIndexRecord{Superseded: &v}, e
	default:
		return LineageIndexRecord{}, invalidEvidence("lineage-record-kind", "unknown lineage record kind")
	}
}

func DecodeCanonicalEvidenceFrame(data []byte) (*EvidenceFrame, error) {
	var f EvidenceFrame
	if err := decodeCanonicalFramed(data, maxEvidenceFrameBytes, &f); err != nil {
		return nil, err
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}
func DecodeCanonicalLineageFrame(data []byte) (*LineageIndexFrame, error) {
	var f LineageIndexFrame
	if err := decodeCanonicalFramed(data, maxLineageFrameBytes, &f); err != nil {
		return nil, err
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}
func decodeCanonicalFramed(data []byte, max uint64, target any) error {
	if len(data) < 8 {
		return invalidEvidence("frame", "short length prefix")
	}
	n := binary.BigEndian.Uint64(data[:8])
	if n > max-8 || n > maxJSONInteger || n != uint64(len(data)-8) {
		return invalidEvidence("frame", "invalid frame length")
	}
	payload := data[8:]
	value, e := ParseStrictJSON(payload)
	if e != nil {
		return e
	}
	canonical, e := CanonicalJSON(value)
	if e != nil {
		return e
	}
	if !bytes.Equal(payload, canonical) {
		return invalidEvidence("frame", "payload is not canonical RFC8785")
	}
	_, e = DecodeStrict(payload, target)
	return e
}

func (frame EvidenceFrame) ComputeDigest() (Digest, error) {
	return digestFlatDomain(EvidenceRecordDigestDomain, frame, "record_digest")
}
func (frame LineageIndexFrame) ComputeDigest() (Digest, error) {
	return digestFlatDomain(LineageRecordDigestDomain, frame, "record_digest")
}
func (state AmbiguousResolutionState) ComputeDigest() (Digest, error) {
	return digestFlatDomain(AmbiguousResolutionDigestDomain, state, "resolution_digest")
}

func (frame EvidenceFrame) Validate() error {
	if frame.FormatVersion != EvidenceFrameFormat || frame.Sequence > maxJSONInteger {
		return invalidEvidence("frame", "format or sequence")
	}
	if frame.Sequence == 0 && frame.PreviousRecordDigest != nil {
		return invalidEvidence("frame", "initial previous digest")
	}
	if frame.PreviousRecordDigest != nil {
		if e := frame.PreviousRecordDigest.Validate(); e != nil {
			return e
		}
	}
	branch, e := frame.Record.branch()
	if e != nil {
		return e
	}
	_ = branch
	if !evidenceKindMatches(frame.RecordKind, frame.Record) {
		return invalidEvidence("frame", "record kind/body mismatch")
	}
	if e := validateEvidenceRecord(frame.Record); e != nil {
		return e
	}
	want, e := frame.ComputeDigest()
	if e != nil || want != frame.RecordDigest {
		return invalidEvidence("frame", "record digest mismatch")
	}
	if e := validateCanonicalFrameSize(frame, maxEvidenceFrameBytes, evidenceRecordFrameLimits[frame.RecordKind]); e != nil {
		return e
	}
	return nil
}
func (frame LineageIndexFrame) Validate() error {
	if frame.FormatVersion != LineageFrameFormat || frame.Sequence > maxJSONInteger {
		return invalidEvidence("lineage-frame", "format or sequence")
	}
	if frame.Sequence == 0 && frame.PreviousRecordDigest != nil {
		return invalidEvidence("lineage-frame", "initial previous digest")
	}
	if frame.PreviousRecordDigest != nil {
		if e := frame.PreviousRecordDigest.Validate(); e != nil {
			return e
		}
	}
	if !lineageKindMatches(frame.RecordKind, frame.Record) {
		return invalidEvidence("lineage-frame", "record kind/body mismatch")
	}
	if e := validateLineageRecord(frame.Record); e != nil {
		return e
	}
	want, e := frame.ComputeDigest()
	if e != nil || want != frame.RecordDigest {
		return invalidEvidence("lineage-frame", "record digest mismatch")
	}
	if e := validateCanonicalFrameSize(frame, maxLineageFrameBytes, lineageRecordFrameLimits[frame.RecordKind]); e != nil {
		return e
	}
	return nil
}

func validateCanonicalFrameSize(frame any, maximum, recordMaximum uint64) error {
	canonical, err := canonicalContractKey(frame)
	if err != nil {
		return err
	}
	size := uint64(len(canonical)) + 8
	if err := validateFramedSizeLimit(size, maximum, recordMaximum); err != nil {
		return invalidEvidence("frame-limit", "canonical framed record exceeds limit")
	}
	return nil
}

func validateFramedSizeLimit(size, maximum, recordMaximum uint64) error {
	if recordMaximum == 0 || size > maximum || size > recordMaximum {
		return invalidEvidence("frame-limit", "framed record exceeds limit")
	}
	return nil
}

func validateEvidenceLimitBoundary(name string, value uint64) error {
	maxima := map[string]uint64{
		"uint16": 65535, "uint32": 4294967295, "uint64_json_safe": maxJSONInteger,
		"quota_reserved_records": 65536, "quota_reserved_bytes": 268435456,
		"evidence_frame_bytes": maxEvidenceFrameBytes, "evidence_segment_bytes": 16 << 20,
		"evidence_segment_records": 4096, "evidence_journal_segments": 16,
		"lineage_frame_bytes": maxLineageFrameBytes, "lineage_index_bytes": 16 << 20,
		"lineage_index_records": 16384, "decision_recovery_identity_bytes": 1024,
		"decision_recovery_encoded_bytes": 1 << 20, "decision_recovery_input_count": 4099,
	}
	maximum, ok := maxima[name]
	if !ok || value > maximum {
		return invalidEvidence("limit-boundary", name)
	}
	return nil
}

func validateEvidenceSegmentUsage(records uint64, bytes uint64) error {
	if records == 0 || validateEvidenceLimitBoundary("evidence_segment_records", records) != nil || validateEvidenceLimitBoundary("evidence_segment_bytes", bytes) != nil {
		return invalidEvidence("segment-usage", "limit")
	}
	return nil
}
func validateLineageIndexUsage(records uint64, bytes uint64) error {
	if records == 0 || validateEvidenceLimitBoundary("lineage_index_records", records) != nil || validateEvidenceLimitBoundary("lineage_index_bytes", bytes) != nil {
		return invalidEvidence("lineage-usage", "limit")
	}
	return nil
}

func evidenceKindMatches(k EvidenceRecordKind, r EvidenceRecord) bool {
	return k == EvidenceRecordHeader && r.Header != nil || k == EvidenceRecordStatementIntent && r.StatementIntent != nil || k == EvidenceRecordIntermediate && r.Intermediate != nil || k == EvidenceRecordCommitIntent && r.CommitIntent != nil || k == EvidenceRecordAttemptTerminal && r.AttemptTerminal != nil || k == EvidenceRecordAmbiguousResolution && r.AmbiguousResolution != nil
}
func lineageKindMatches(k LineageRecordKind, r LineageIndexRecord) bool {
	return k == LineageRecordHeader && r.Header != nil || k == LineageRecordGenerationReserved && r.Reserved != nil || k == LineageRecordGenerationActivated && r.Activated != nil || k == LineageRecordGenerationCheckpoint && r.Checkpoint != nil || k == LineageRecordGenerationSuperseded && r.Superseded != nil
}

func invalidEvidence(op, msg string) error { return fail(CodeInvalidJSON, "evidence-"+op, msg, nil) }
func requireEvidenceDigests(values ...Digest) error {
	for _, d := range values {
		if e := d.Validate(); e != nil {
			return e
		}
	}
	return nil
}
func requireSafe64(v uint64, field string) error {
	if v > maxJSONInteger {
		return invalidEvidence(field, "integer exceeds JSON safe range")
	}
	return nil
}

func (v StableFailureEvidence) Validate() error {
	projectionKinds := map[ErrorCode][]string{
		CodeAuthorityDrift: {"authority"}, CodeCatalogDrift: {"catalog"}, CodeIntermediateStateMismatch: {"catalog"},
		CodeProjectionUnsupportedMajor: {"snapshot"}, CodeProjectionCapabilityMismatch: {"snapshot"},
		CodeProjectionCatalogQueryFailed: {"authority", "catalog"}, CodeProjectionLimitExceeded: {"authority", "catalog"},
		CodeProjectionNonCanonicalWitness: {"authority", "catalog"}, CodeProjectionUnknownObject: {"authority", "catalog"},
		CodeProjectionInvalidScope: {"authority", "catalog"}, CodeProjectionInvalidExpression: {"catalog"},
		CodeProjectionMetadataMismatch: {"authority", "catalog", "snapshot"}, CodeProjectionSnapshotInvalid: {"snapshot"},
	}
	if kinds, ok := projectionKinds[v.Code]; ok {
		if v.ProjectionKind == nil || !stringIn(*v.ProjectionKind, kinds...) {
			return invalidEvidence("failure", "projection kind")
		}
		path := "transaction"
		phases := []string{"connected_session", "migration_role", "migration_transaction", "reconcile"}
		if *v.ProjectionKind == "authority" {
			path = "authority"
		} else if *v.ProjectionKind == "catalog" {
			path = "catalog"
			phases = []string{"migration_role", "migration_transaction", "reconcile"}
		}
		if v.Path != path || !stringIn(v.Phase, phases...) {
			return invalidEvidence("failure", "projection path or phase")
		}
		if (v.Code == CodeProjectionUnsupportedMajor || v.Code == CodeProjectionCapabilityMismatch) && v.Phase != "connected_session" {
			return invalidEvidence("failure", "snapshot phase")
		}
	} else {
		if v.ProjectionKind != nil {
			return invalidEvidence("failure", "non-projection kind")
		}
		rules := map[ErrorCode]struct {
			path   string
			phases []string
		}{
			CodeEvidenceJournalFailed: {"journal", []string{"journal_open", "journal_replay", "reconcile", "journal_close"}}, CodeEvidenceRecoveryRequired: {"journal", []string{"journal_replay", "reconcile"}},
			CodeContextCanceled: {"context", failurePhases()}, CodeDeadlineExceeded: {"context", failurePhases()}, CodeInvalidSQL: {"sql", []string{"preconnect", "migration_transaction"}},
			CodeInvalidLedger: {"ledger", []string{"migration_role", "migration_transaction", "reconcile"}}, CodeLockLost: {"transaction", []string{"migration_role", "migration_transaction", "reconcile"}},
			CodeTransactionBoundary: {"transaction", []string{"migration_transaction", "commit", "reconcile"}}, CodeAmbiguousCommit: {"transaction", []string{"commit", "reconcile"}},
			CodeUntrusted: {"trust", []string{"preconnect", "connected_session", "migration_role", "reconcile"}},
		}
		rule, ok := rules[v.Code]
		if !ok || v.Path != rule.path || !stringIn(v.Phase, rule.phases...) {
			return invalidEvidence("failure", "non-projection tuple")
		}
	}
	if stringIn(v.Phase, "preconnect", "journal_open", "journal_replay", "journal_close") {
		if v.Major != nil {
			return invalidEvidence("failure", "major must be null")
		}
	} else if v.Code == CodeProjectionUnsupportedMajor {
		if v.Major == nil || *v.Major == 0 {
			return invalidEvidence("failure", "unsupported major")
		}
	} else if stringIn(v.Phase, "connected_session", "migration_role", "migration_transaction", "commit") {
		if v.Major == nil || *v.Major < 15 || *v.Major > 17 {
			return invalidEvidence("failure", "supported major")
		}
	} else if v.Phase == "reconcile" && v.Major != nil && (*v.Major < 15 || *v.Major > 17) {
		return invalidEvidence("failure", "reconcile major")
	}
	return nil
}

func (v RetryProofEvidence) Validate() error {
	if !stringIn(v.ProofKind, "projection_transient_exact_predecessor", "precommit_rollback_exact_predecessor", "precommit_connection_terminated_exact_predecessor", "commit_rejected_exact_predecessor") {
		return invalidEvidence("retry-proof", "kind")
	}
	if err := requireEvidenceDigests(v.AttemptPredecessorCatalogDigest, v.ObservedCatalogDigest, v.LedgerPrefixDigest, v.AuthorityResultDigest); err != nil {
		return err
	}
	if v.AttemptPredecessorCatalogDigest != v.ObservedCatalogDigest {
		return invalidEvidence("retry-proof", "predecessor")
	}
	if (v.ProofKind == "commit_rejected_exact_predecessor") != (v.CommitRejectedReason != nil) {
		return invalidEvidence("retry-proof", "reason nullability")
	}
	if v.CommitRejectedReason != nil && !stringIn(*v.CommitRejectedReason, "serialization_failure", "deadlock_detected", "other_confirmed_postgres_error") {
		return invalidEvidence("retry-proof", "reason")
	}
	return nil
}

func stringIn(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
func failurePhases() []string {
	return []string{"preconnect", "journal_open", "journal_replay", "connected_session", "migration_role", "migration_transaction", "commit", "reconcile", "journal_close"}
}

func validateEvidenceRecord(r EvidenceRecord) error {
	switch {
	case r.Header != nil:
		return r.Header.Validate()
	case r.StatementIntent != nil:
		return r.StatementIntent.Validate()
	case r.Intermediate != nil:
		return r.Intermediate.Validate()
	case r.CommitIntent != nil:
		return r.CommitIntent.Validate()
	case r.AttemptTerminal != nil:
		return r.AttemptTerminal.Validate()
	case r.AmbiguousResolution != nil:
		return r.AmbiguousResolution.Validate()
	default:
		return invalidEvidence("record", "empty")
	}
}
func validateLineageRecord(r LineageIndexRecord) error {
	switch {
	case r.Header != nil:
		return r.Header.Validate()
	case r.Reserved != nil:
		return r.Reserved.Validate()
	case r.Activated != nil:
		return r.Activated.Validate()
	case r.Checkpoint != nil:
		return r.Checkpoint.Validate()
	case r.Superseded != nil:
		return r.Superseded.Validate()
	default:
		return invalidEvidence("lineage-record", "empty")
	}
}

func (h JournalHeader) Validate() error {
	if h.FormatVersion != EvidenceJournalFormat || !validEvidenceLimitsProfile(h.LimitsProfile) {
		return invalidEvidence("header", "format or limits")
	}
	limits, limitsErr := evidenceQuotaLimitsForProfile(h.LimitsProfile)
	if limitsErr != nil {
		return limitsErr
	}
	if e := requireEvidenceDigests(h.JournalIdentityDigest, h.ReleaseTrustDecisionDigest, h.RunnerProjectionDecisionDigest, h.ExecutionLineageDigest, h.OuterArtifactDigest, h.DecisionRecoveryArtifactSHA256, h.ManifestDigest, h.RunnerReleaseDigest, h.SchemaBundleDigest, h.AuthorityProfileDigest, h.AuthorityBindingDigest, h.QuotaReservationDigest); e != nil {
		return e
	}
	if e := requireSafe64(h.OuterArtifactSizeBytes, "outer-size"); e != nil {
		return e
	}
	if e := requireSafe64(h.DecisionRecoveryArtifactSizeBytes, "recovery-size"); e != nil {
		return e
	}
	if h.DecisionRecoveryArtifactSizeBytes > maxDecisionRecoveryArtifactBytes {
		return invalidEvidence("header", "artifact size")
	}
	if (h.SegmentIndex == 0) != (h.PreviousSegmentRecordDigest == nil) || h.ReservedRecords == 0 || h.ReservedRecords > limits.maxRecords || h.ReservedBytes == 0 || h.ReservedBytes > limits.maxBytes || h.ReservedSegments == 0 || h.ReservedSegments > limits.maxSegments || h.SegmentIndex >= h.ReservedSegments {
		return invalidEvidence("header", "segment or reservation")
	}
	if e := requireSafe64(h.ReservedRecords, "reserved-records"); e != nil {
		return e
	}
	if e := requireSafe64(h.ReservedBytes, "reserved-bytes"); e != nil {
		return e
	}
	wantIdentity, e := JournalIdentityDigest(h)
	if e != nil || wantIdentity != h.JournalIdentityDigest {
		return invalidEvidence("header", "journal identity digest mismatch")
	}
	return nil
}

func validEvidenceLimitsProfile(profile string) bool {
	return profile == EvidenceLimitsProfile || profile == LineageQuotaProfileV2 || profile == LineageQuotaProfileV3 || profile == LineageQuotaProfileV4
}

func checkpointMaximumForProfile(profile string) (uint64, error) {
	switch profile {
	case EvidenceLimitsProfile:
		return lineageRecordFrameLimits[LineageRecordGenerationCheckpoint], nil
	case LineageQuotaProfileV2:
		return v2GenerationCheckpointMaximum, nil
	case LineageQuotaProfileV3:
		return v2GenerationCheckpointMaximum, nil
	case LineageQuotaProfileV4:
		return v2GenerationCheckpointMaximum, nil
	default:
		return 0, invalidEvidence("checkpoint", "unknown lineage quota profile")
	}
}

// lineageFrameMaximumForProfile returns the inclusive framed-byte ceiling for
// one stored lineage record. The physical index container keeps its
// historical v1 profile, while the selected generation profile narrows only
// the checkpoint record maximum.
func lineageFrameMaximumForProfile(kind LineageRecordKind, profile string) (uint64, error) {
	if !validEvidenceLimitsProfile(profile) {
		return 0, invalidEvidence("lineage-frame", "unknown quota profile")
	}
	maximum, ok := lineageRecordFrameLimits[kind]
	if !ok || maximum == 0 {
		return 0, invalidEvidence("lineage-frame", "unknown record kind")
	}
	if kind == LineageRecordGenerationCheckpoint {
		return checkpointMaximumForProfile(profile)
	}
	return maximum, nil
}
func (v ProjectionResultEvidence) Validate() error {
	if e := v.Digest.Validate(); e != nil {
		return e
	}
	return v.Metadata.validate()
}
func (v StatementIntent) Validate() error {
	if !migrationIDPattern.MatchString(v.MigrationID) || v.AttemptIndex == 0 || v.SQLPath == "" || v.EndOffset <= v.StartOffset || v.EndOffset > v.SQLArtifactSizeBytes {
		return invalidEvidence("statement-intent", "identity or range")
	}
	if (v.AttemptIndex == 1) != (v.PreviousAttemptTerminalDigest == nil) || (v.StatementIndex == 0) != (v.PreviousIntermediateStateDigest == nil) {
		return invalidEvidence("statement-intent", "attempt/statement link")
	}
	if e := requireSafe64(v.SQLArtifactSizeBytes, "sql-size"); e != nil {
		return e
	}
	if e := requireSafe64(v.StartOffset, "start"); e != nil {
		return e
	}
	if e := requireSafe64(v.EndOffset, "end"); e != nil {
		return e
	}
	if e := requireEvidenceDigests(v.SchemaBundleDigest, v.CatalogContractDigest, v.AuthorityProfileDigest, v.AuthorityBindingDigest, v.SQLArtifactSHA256, v.StatementSHA256, v.ExpectedTransitionDigest, v.AuthorityBeforeDigest, v.CatalogBeforeDigest); e != nil {
		return e
	}
	if e := v.AuthorityBeforeResult.Validate(); e != nil {
		return e
	}
	if e := v.CatalogBeforeResult.Validate(); e != nil {
		return e
	}
	if v.AuthorityBeforeResult.Digest != v.AuthorityBeforeDigest || v.CatalogBeforeResult.Digest != v.CatalogBeforeDigest {
		return invalidEvidence("statement-intent", "before evidence digest")
	}
	return nil
}
func (v StatementIntermediateEvidence) Validate() error {
	if e := v.State.Validate(); e != nil {
		return e
	}
	for _, x := range []ProjectionResultEvidence{v.AuthorityBeforeResult, v.CatalogBeforeResult, v.AuthorityAfterResult, v.CatalogAfterResult} {
		if e := x.Validate(); e != nil {
			return e
		}
	}
	if (v.PreledgerAuthorityResult == nil) != (v.PreledgerCatalogResult == nil) {
		return invalidEvidence("intermediate", "preledger pair")
	}
	if v.PreledgerAuthorityResult != nil {
		if e := v.PreledgerAuthorityResult.Validate(); e != nil {
			return e
		}
		if e := v.PreledgerCatalogResult.Validate(); e != nil {
			return e
		}
	}
	if v.AuthorityBeforeResult.Digest != v.State.AuthorityBeforeDigest || v.CatalogBeforeResult.Digest != v.State.CatalogBeforeDigest || v.AuthorityAfterResult.Digest != v.State.AuthorityAfterDigest || v.CatalogAfterResult.Digest != v.State.CatalogAfterDigest {
		return invalidEvidence("intermediate", "result digest mapping")
	}
	return nil
}
func (v CommitIntentLedgerRow) Validate() error {
	if !migrationIDPattern.MatchString(v.MigrationID) || v.MigrationName == "" || v.SQLPath == "" {
		return invalidEvidence("ledger-row", "identity")
	}
	if e := requireSafe64(v.SQLSizeBytes, "ledger-sql-size"); e != nil {
		return e
	}
	return requireEvidenceDigests(v.SQLSHA256, v.BundleDigest)
}
func (v CommitIntent) Validate() error {
	if !migrationIDPattern.MatchString(v.MigrationID) || v.AttemptIndex == 0 || v.ExpectedLedgerLength == 0 || v.ExpectedLedgerHead != v.MigrationID || v.LedgerRow.MigrationID != v.MigrationID {
		return invalidEvidence("commit-intent", "identity")
	}
	if (v.AttemptIndex == 1) != (v.PreviousAttemptTerminalDigest == nil) {
		return invalidEvidence("commit-intent", "attempt link")
	}
	if e := requireEvidenceDigests(v.SchemaBundleDigest, v.CatalogContractDigest, v.AuthorityProfileDigest, v.AuthorityBindingDigest, v.AttemptPredecessorCatalogDigest, v.LastIntermediateStateDigest); e != nil {
		return e
	}
	return v.LedgerRow.Validate()
}
func (v AmbiguousResolutionState) Validate() error {
	if !migrationIDPattern.MatchString(v.MigrationID) || v.AttemptIndex == 0 {
		return invalidEvidence("resolution", "identity")
	}
	pairs := map[string]string{"resolved_committed": "exact_committed", "resolved_pending": "exact_pending", "resolved_divergent": "divergent"}
	if pairs[v.Outcome] != v.ReconcileResult {
		return invalidEvidence("resolution", "outcome")
	}
	if e := requireEvidenceDigests(v.SchemaBundleDigest, v.CatalogContractDigest, v.AuthorityProfileDigest, v.AuthorityBindingDigest, v.UnresolvedTerminalDigest, v.ResolutionDigest); e != nil {
		return e
	}
	want, e := v.ComputeDigest()
	if e != nil || want != v.ResolutionDigest {
		return invalidEvidence("resolution", "digest")
	}
	return nil
}

func (v LineageIndexHeader) Validate() error {
	if v.FormatVersion != LineageIndexFormat || v.LimitsProfile != LineageLimitsProfile || v.DeploymentID == "" || v.ExpectedDatabaseIdentity.DatabaseName == "" || v.RepositoryIdentity == "" {
		return invalidEvidence("lineage-header", "shape")
	}
	return v.ExecutionLineageDigest.Validate()
}
func (v LineageContinuationContext) Validate() error {
	if !migrationIDPattern.MatchString(v.MigrationID) || v.AttemptIndex == 0 {
		return invalidEvidence("continuation", "identity")
	}
	if e := requireEvidenceDigests(v.SourceJournalIdentityDigest, v.SourceCheckpointRecordDigest, v.SourceTerminalDigest); e != nil {
		return e
	}
	switch v.StartAction {
	case "begin_first_attempt_next_entry":
		if v.AttemptIndex != 1 || v.PreviousAttemptTerminalDigest != nil {
			return invalidEvidence("continuation", "next entry")
		}
	case "begin_next_attempt":
		if v.AttemptIndex < 2 || v.PreviousAttemptTerminalDigest == nil || *v.PreviousAttemptTerminalDigest != v.SourceTerminalDigest {
			return invalidEvidence("continuation", "next attempt")
		}
	default:
		return invalidEvidence("continuation", "action")
	}
	return nil
}
func (v GenerationReserved) Validate() error {
	if e := requireEvidenceDigests(v.ExecutionLineageDigest, v.JournalIdentityDigest, v.RunnerProjectionDecisionDigest, v.SchemaBundleDigest, v.QuotaReservationDigest, v.ExpectedSegment0HeaderDigest); e != nil {
		return e
	}
	limits, limitsErr := evidenceQuotaLimitsForProfile(v.PlannedSegment0Header.LimitsProfile)
	if limitsErr != nil {
		return limitsErr
	}
	if v.ReservedRecords == 0 || v.ReservedRecords > limits.maxRecords || v.ReservedBytes == 0 || v.ReservedBytes > limits.maxBytes || v.ReservedSegments == 0 || v.ReservedSegments > limits.maxSegments {
		return invalidEvidence("reserved", "limits")
	}
	if v.Continuation != nil {
		if e := v.Continuation.Validate(); e != nil {
			return e
		}
	}
	wantQuota, e := QuotaReservationDigest(v)
	if e != nil || wantQuota != v.QuotaReservationDigest {
		return invalidEvidence("reserved", "quota reservation digest mismatch")
	}
	if e := v.PlannedSegment0Header.Validate(); e != nil {
		return e
	}
	h := v.PlannedSegment0Header
	if h.SegmentIndex != 0 || h.PreviousSegmentRecordDigest != nil || h.ExecutionLineageDigest != v.ExecutionLineageDigest || h.JournalIdentityDigest != v.JournalIdentityDigest || h.RunnerProjectionDecisionDigest != v.RunnerProjectionDecisionDigest || h.SchemaBundleDigest != v.SchemaBundleDigest || h.QuotaReservationDigest != v.QuotaReservationDigest || h.ReservedRecords != v.ReservedRecords || h.ReservedBytes != v.ReservedBytes || h.ReservedSegments != v.ReservedSegments {
		return invalidEvidence("reserved", "planned header")
	}
	f := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &h}}
	want, e := f.ComputeDigest()
	if e != nil || want != v.ExpectedSegment0HeaderDigest {
		return invalidEvidence("reserved", "header digest")
	}
	return nil
}
func (v GenerationActivated) Validate() error {
	if e := requireEvidenceDigests(v.ExecutionLineageDigest, v.JournalIdentityDigest, v.RunnerProjectionDecisionDigest, v.SchemaBundleDigest, v.QuotaReservationDigest, v.GenerationReservedRecordDigest, v.Segment0HeaderDigest, v.InitialJournalTailDigest); e != nil {
		return e
	}
	if v.Segment0HeaderDigest != v.InitialJournalTailDigest {
		return invalidEvidence("activated", "initial tail")
	}
	return nil
}
func (v GenerationCheckpoint) Validate() error {
	if e := requireEvidenceDigests(v.ExecutionLineageDigest, v.JournalIdentityDigest, v.RunnerProjectionDecisionDigest, v.SchemaBundleDigest, v.JournalTailDigest); e != nil {
		return e
	}
	if v.JournalNextSequence == 0 || v.JournalNextSequence > maxJSONInteger || (v.MigrationID == nil) != (v.AttemptIndex == nil) {
		return invalidEvidence("checkpoint", "identity")
	}
	if v.MigrationID != nil && !migrationIDPattern.MatchString(*v.MigrationID) {
		return invalidEvidence("checkpoint", "migration identity")
	}
	if v.AttemptIndex != nil && *v.AttemptIndex == 0 {
		return invalidEvidence("checkpoint", "attempt identity")
	}
	if e := validateOptionalDigests(v.LastStatementIntentRecordDigest, v.LastIntermediateEvidenceRecordDigest, v.LastCommitIntentRecordDigest, v.LastTerminalDigest, v.LastResolutionDigest, v.PreviousAttemptTerminalDigest, v.LastIntermediateStateDigest, v.PreviousCheckpointRecordDigest); e != nil {
		return e
	}
	return nil
}
func (v GenerationSuperseded) Validate() error {
	if e := requireEvidenceDigests(v.ExecutionLineageDigest, v.OldJournalIdentityDigest, v.OldRunnerProjectionDecisionDigest, v.OldSchemaBundleDigest, v.LineageSupersessionAuthorityDigest); e != nil {
		return e
	}
	if e := validateOptionalDigests(v.OldCheckpointRecordDigest, v.OldActivationRecordDigest, v.OldInitialJournalTailDigest); e != nil {
		return e
	}
	planned := map[string]bool{"exact_committed_continue_successor": true, "precommit_aborted_retryable": true, "exact_pending": true, "resolved_pending": true, "activated_no_migration_progress": true}
	known := map[string]bool{"exact_committed_bundle_complete": true, "confirmed_abort_terminal": true, "terminal_failure": true, "divergent_terminal": true}
	if !planned[v.Outcome] && !known[v.Outcome] {
		return invalidEvidence("superseded", "outcome")
	}
	if planned[v.Outcome] != (v.PlannedGenerationReserved != nil) {
		return invalidEvidence("superseded", "planned")
	}
	if v.Outcome == "activated_no_migration_progress" {
		if v.OldCheckpointRecordDigest != nil || v.OldActivationRecordDigest == nil || v.OldInitialJournalTailDigest == nil {
			return invalidEvidence("superseded", "header boundary")
		}
	} else if v.OldCheckpointRecordDigest == nil || v.OldActivationRecordDigest != nil || v.OldInitialJournalTailDigest != nil {
		return invalidEvidence("superseded", "checkpoint boundary")
	}
	if v.PlannedGenerationReserved != nil {
		return v.PlannedGenerationReserved.Validate()
	}
	return nil
}

func JournalIdentityDigest(header JournalHeader) (Digest, error) {
	subject := struct {
		ReleaseTrustDecisionDigest        Digest `json:"release_trust_decision_digest"`
		RunnerProjectionDecisionDigest    Digest `json:"runner_projection_decision_digest"`
		OuterArtifactDigest               Digest `json:"outer_artifact_digest"`
		OuterArtifactSizeBytes            uint64 `json:"outer_artifact_size_bytes"`
		DecisionRecoveryArtifactSHA256    Digest `json:"decision_recovery_artifact_sha256"`
		DecisionRecoveryArtifactSizeBytes uint64 `json:"decision_recovery_artifact_size_bytes"`
		SchemaBundleDigest                Digest `json:"schema_bundle_digest"`
		AuthorityProfileDigest            Digest `json:"authority_profile_digest"`
		AuthorityBindingDigest            Digest `json:"authority_binding_digest"`
	}{header.ReleaseTrustDecisionDigest, header.RunnerProjectionDecisionDigest, header.OuterArtifactDigest, header.OuterArtifactSizeBytes, header.DecisionRecoveryArtifactSHA256, header.DecisionRecoveryArtifactSizeBytes, header.SchemaBundleDigest, header.AuthorityProfileDigest, header.AuthorityBindingDigest}
	return digestFlatDomain(JournalIdentityDigestDomain, subject, "")
}
func ExecutionLineageDigest(header LineageIndexHeader) (Digest, error) {
	subject := struct {
		DeploymentID             string                          `json:"deployment_id"`
		ExpectedDatabaseIdentity LineageExpectedDatabaseIdentity `json:"expected_database_identity"`
		RepositoryIdentity       string                          `json:"repository_identity"`
	}{header.DeploymentID, header.ExpectedDatabaseIdentity, header.RepositoryIdentity}
	return digestFlatDomain(ExecutionLineageDigestDomain, subject, "")
}
func QuotaReservationDigest(reserved GenerationReserved) (Digest, error) {
	profile := reserved.PlannedSegment0Header.LimitsProfile
	if !validEvidenceLimitsProfile(profile) {
		return "", invalidEvidence("reserved", "unknown lineage quota profile")
	}
	subject := struct {
		LimitsProfile                  string                      `json:"limits_profile"`
		ExecutionLineageDigest         Digest                      `json:"execution_lineage_digest"`
		JournalIdentityDigest          Digest                      `json:"journal_identity_digest"`
		RunnerProjectionDecisionDigest Digest                      `json:"runner_projection_decision_digest"`
		SchemaBundleDigest             Digest                      `json:"schema_bundle_digest"`
		ReservedRecords                uint64                      `json:"reserved_records"`
		ReservedBytes                  uint64                      `json:"reserved_bytes"`
		ReservedSegments               uint32                      `json:"reserved_segments"`
		Continuation                   *LineageContinuationContext `json:"continuation"`
	}{
		LimitsProfile:                  profile,
		ExecutionLineageDigest:         reserved.ExecutionLineageDigest,
		JournalIdentityDigest:          reserved.JournalIdentityDigest,
		RunnerProjectionDecisionDigest: reserved.RunnerProjectionDecisionDigest,
		SchemaBundleDigest:             reserved.SchemaBundleDigest,
		ReservedRecords:                reserved.ReservedRecords,
		ReservedBytes:                  reserved.ReservedBytes,
		ReservedSegments:               reserved.ReservedSegments,
		Continuation:                   reserved.Continuation,
	}
	domain := QuotaReservationDigestDomain
	if profile == LineageQuotaProfileV2 {
		domain = QuotaReservationDigestDomainV2
	} else if profile == LineageQuotaProfileV3 {
		domain = QuotaReservationDigestDomainV3
	} else if profile == LineageQuotaProfileV4 {
		domain = QuotaReservationDigestDomainV4
	}
	return digestFlatDomain(domain, subject, "")
}
func LedgerPrefixDigest(rows []CommitIntentLedgerRow) (Digest, error) {
	subject := struct {
		Rows []CommitIntentLedgerRow `json:"rows"`
	}{rows}
	return digestFlatDomain(LedgerPrefixDigestDomain, subject, "")
}

func validateOptionalDigests(values ...*Digest) error {
	for _, value := range values {
		if value != nil {
			if err := value.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}
