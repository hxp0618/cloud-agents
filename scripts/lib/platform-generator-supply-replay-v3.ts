import {
  createGeneratorSupplyReplay,
  type GeneratorSupplyReplayAuthorityFile,
  type GeneratorSupplyReplayExpected,
  type GeneratorSupplyReplayPinnedContract,
  type GeneratorSupplyReplayPlatformMaterial,
  type GeneratorSupplyReplayPreparedReceipt,
  type GeneratorSupplyReplayPreparedReceipts,
  type GeneratorSupplyReplayProjection,
  type GeneratorSupplyReplayValidation,
} from "./platform-generator-supply-replay";
import {
  SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_V3_REPLAY_AUTHORITY_FILES,
  SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
  SUCCESSOR_V3_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag-v3";

export type GeneratorSupplyReplayV3AuthorityFile = GeneratorSupplyReplayAuthorityFile;
export type GeneratorSupplyReplayV3Contract = GeneratorSupplyReplayPinnedContract;
export type GeneratorSupplyReplayV3Projection = GeneratorSupplyReplayProjection;
export type GeneratorSupplyReplayV3PlatformMaterial = GeneratorSupplyReplayPlatformMaterial;
export type GeneratorSupplyReplayV3Expected =
  GeneratorSupplyReplayExpected<GeneratorSupplyReplayV3Contract>;
export type GeneratorSupplyReplayV3Validation = GeneratorSupplyReplayValidation;
export type GeneratorSupplyReplayV3PreparedReceipt = GeneratorSupplyReplayPreparedReceipt;
export type GeneratorSupplyReplayV3PreparedReceipts = GeneratorSupplyReplayPreparedReceipts;

export const GENERATOR_SUPPLY_V3_REPLAY_AUTHORITY_PATHS = SUCCESSOR_V3_REPLAY_AUTHORITY_FILES;

export class GeneratorSupplyReplayV3Error extends Error {
  readonly code = "GENERATOR_SUPPLY_REPLAY_V3_INVALID";

  constructor(
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "GeneratorSupplyReplayV3Error";
  }
}

// v3 replays the immutable v2 supply set and pins its own authority paths,
// wrapper, replay scope and the exact 49 core generator outputs.
const replay = createGeneratorSupplyReplay<GeneratorSupplyReplayV3Contract>({
  version: "v3",
  predecessor: "v2",
  Error: GeneratorSupplyReplayV3Error,
  receiptPaths: SUCCESSOR_V3_REPLAY_RECEIPT_PATHS,
  projectionExclusions: SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
  coreGeneratorOutputPaths: SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
  pinnedContract: {
    authorityFiles: SUCCESSOR_V3_REPLAY_AUTHORITY_FILES,
    preReplayExclusionPolicy: "EXACT17_ONLY_NO_WILDCARD_ALL_OTHER_TRACKED_BYTES_INCLUDED",
    wrapperPolicy: "VERSIONED_ISOLATION_WRAPPER_V3",
    authoritativeReplayScope: "EXACT49_CORE_OUTPUTS_SUPPLY_PROFILE_AND_LOCK_POST_ASSEMBLY",
  },
});

export const assertGeneratorSupplyReplayV3ContractCurrent = replay.assertContractCurrent;
/**
 * Reads every one of the exact eight late-bound receipts once, then validates
 * their complete closed semantic graph. Hashes are always computed from those
 * stable-read bytes; validation never reopens a receipt.
 */
export const assertGeneratorSupplyReplayV3Receipts = replay.assertReceipts;
/**
 * Validates the exact seven caller-owned native replay receipts, derives the
 * canonical summary, and returns the complete ordered eight-receipt set.
 */
export const buildGeneratorSupplyReplayV3PreparedReceipts = replay.buildPreparedReceipts;
export const assertGeneratorSupplyReplayV3SnapshotMutationForTest =
  replay.assertSnapshotMutationForTest;
export const assertGeneratorSupplyReplayV3InputSnapshotMutationForTest =
  replay.assertInputSnapshotMutationForTest;
export const buildGeneratorSupplyReplayV3SummaryForTest = replay.buildSummaryForTest;
export const buildGeneratorSupplyReplayV3ExpectedFromImmutableV2 =
  replay.buildExpectedFromPredecessor;
export const assertGeneratorSupplyReplayV3V2DerivedABAMutationForTest =
  replay.assertPredecessorDerivedABAMutationForTest;
export const buildGeneratorSupplyReplayV3TestFixture = replay.buildTestFixture;
