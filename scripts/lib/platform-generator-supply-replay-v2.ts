import {
  createGeneratorSupplyReplay,
  type GeneratorSupplyReplayAuthorityFile,
  type GeneratorSupplyReplayContract,
  type GeneratorSupplyReplayExpected,
  type GeneratorSupplyReplayPlatformMaterial,
  type GeneratorSupplyReplayPreparedReceipt,
  type GeneratorSupplyReplayPreparedReceipts,
  type GeneratorSupplyReplayProjection,
  type GeneratorSupplyReplayValidation,
} from "./platform-generator-supply-replay";
import {
  SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_PROJECTION_EXCLUSIONS,
  SUCCESSOR_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag";

export type GeneratorSupplyReplayV2AuthorityFile = GeneratorSupplyReplayAuthorityFile;
export type GeneratorSupplyReplayV2Contract = GeneratorSupplyReplayContract;
export type GeneratorSupplyReplayV2Projection = GeneratorSupplyReplayProjection;
export type GeneratorSupplyReplayV2PlatformMaterial = GeneratorSupplyReplayPlatformMaterial;
export type GeneratorSupplyReplayV2Expected =
  GeneratorSupplyReplayExpected<GeneratorSupplyReplayV2Contract>;
export type GeneratorSupplyReplayV2Validation = GeneratorSupplyReplayValidation;
export type GeneratorSupplyReplayV2PreparedReceipt = GeneratorSupplyReplayPreparedReceipt;
export type GeneratorSupplyReplayV2PreparedReceipts = GeneratorSupplyReplayPreparedReceipts;

export class GeneratorSupplyReplayV2Error extends Error {
  readonly code = "GENERATOR_SUPPLY_REPLAY_V2_INVALID";

  constructor(
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "GeneratorSupplyReplayV2Error";
  }
}

// v2 replays the immutable v1 supply set; its contract pins no authority paths.
const replay = createGeneratorSupplyReplay<GeneratorSupplyReplayV2Contract>({
  version: "v2",
  predecessor: "v1",
  Error: GeneratorSupplyReplayV2Error,
  receiptPaths: SUCCESSOR_REPLAY_RECEIPT_PATHS,
  projectionExclusions: SUCCESSOR_PROJECTION_EXCLUSIONS,
  coreGeneratorOutputPaths: SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS,
});

export const assertGeneratorSupplyReplayV2ContractCurrent = replay.assertContractCurrent;
/**
 * Reads every one of the exact eight late-bound receipts once, then validates
 * their complete closed semantic graph. Hashes are always computed from those
 * stable-read bytes; validation never reopens a receipt.
 */
export const assertGeneratorSupplyReplayV2Receipts = replay.assertReceipts;
/**
 * Validates the exact seven caller-owned native replay receipts, derives the
 * canonical summary, and returns the complete ordered eight-receipt set.
 */
export const buildGeneratorSupplyReplayV2PreparedReceipts = replay.buildPreparedReceipts;
export const assertGeneratorSupplyReplayV2SnapshotMutationForTest =
  replay.assertSnapshotMutationForTest;
export const assertGeneratorSupplyReplayV2InputSnapshotMutationForTest =
  replay.assertInputSnapshotMutationForTest;
export const buildGeneratorSupplyReplayV2SummaryForTest = replay.buildSummaryForTest;
export const buildGeneratorSupplyReplayV2ExpectedFromImmutableV1 =
  replay.buildExpectedFromPredecessor;
export const assertGeneratorSupplyReplayV2V1DerivedABAMutationForTest =
  replay.assertPredecessorDerivedABAMutationForTest;
export const buildGeneratorSupplyReplayV2TestFixture = replay.buildTestFixture;
