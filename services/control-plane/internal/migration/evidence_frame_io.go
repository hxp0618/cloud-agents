package migration

import (
	"encoding/binary"
	"errors"
	"io"
)

// FrameReplayState is the closed classification returned by the bounded frame
// readers. Final-torn values are reserved for a later journal layer that owns
// durable registration authority. This frame layer never emits them and never
// truncates or writes while replaying.
type FrameReplayState uint8

const (
	FrameReplayCleanEOF FrameReplayState = iota
	FrameReplayValid
	FrameReplayFinalTornPrefix
	FrameReplayFinalTornPayload
	FrameReplayCorrupt
	FrameReplayLimitExceeded
	FrameReplayJournalFailed
)

// FrameReplayProfile supplies the already-observed container and global usage
// at the reader boundary. Maxima are inclusive. The frame reader accounts for
// each complete frame against both scopes with checked arithmetic; filesystem
// discovery and policy remain the caller's responsibility.
type FrameReplayProfile struct {
	ContainerBytes          uint64
	ContainerRecords        uint64
	ContainerMaximumBytes   uint64
	ContainerMaximumRecords uint64
	GlobalBytes             uint64
	GlobalRecords           uint64
	GlobalMaximumBytes      uint64
	GlobalMaximumRecords    uint64
}

// EvidenceFrameReplayResult and LineageFrameReplayResult deliberately carry
// different typed frames. They are not an open JSON or interface{} result.
type EvidenceFrameReplayResult struct {
	State       FrameReplayState
	Frame       *EvidenceFrame
	StartOffset uint64
	BytesRead   uint64
	Err         error
}

type LineageFrameReplayResult struct {
	State       FrameReplayState
	Frame       *LineageIndexFrame
	StartOffset uint64
	BytesRead   uint64
	Err         error
}

type frameContainerKind uint8

const (
	evidenceSegmentContainer frameContainerKind = iota + 1
	lineageIndexContainer
)

type finalContainerBoundary struct {
	kind                  frameContainerKind
	identity              string
	containerOrdinal      uint32
	replayStartOffset     uint64
	observedPhysicalBytes uint64
}

func (boundary finalContainerBoundary) validFor(kind frameContainerKind) bool {
	return boundary.kind == kind && boundary.identity != "" && boundary.replayStartOffset == 0
}

type EvidenceFrameReplayReader struct {
	replay   framedReplayReader
	terminal *EvidenceFrameReplayResult
	source   *evidenceVerifiedReplaySource
	closed   bool
}
type LineageFrameReplayReader struct {
	replay   framedReplayReader
	terminal *LineageFrameReplayResult
	source   *evidenceVerifiedReplaySource
	closed   bool
}

type framedReplayReader struct {
	reader       io.Reader
	profile      FrameReplayProfile
	maximumFrame uint64
	boundary     finalContainerBoundary
	offset       uint64
	terminal     bool
}

type rawFrameReplayState uint8

const (
	rawFrameValid rawFrameReplayState = iota
	rawFrameIncompletePrefix
	rawFrameIncompletePayload
	rawFrameCorrupt
	rawFrameLimitExceeded
	rawFrameJournalFailed
)

type rawFrameReplayResult struct {
	state       rawFrameReplayState
	framed      []byte
	startOffset uint64
	bytesRead   uint64
	err         error
}

// newEvidenceFrameReplayReader consumes a strict physical inventory boundary
// for one evidence segment. This layer does not own durable registration or
// final-tail healing authority; every partial frame remains corrupt.
func newEvidenceFrameReplayReader(source *evidenceVerifiedReplaySource, profile FrameReplayProfile) (*EvidenceFrameReplayReader, error) {
	replay, err := newFramedReplayReader(source, profile, maxEvidenceFrameBytes, evidenceSegmentContainer)
	if err != nil {
		return nil, closeReplaySourceAfterConstructorFailure(source, err)
	}
	return &EvidenceFrameReplayReader{replay: replay, source: source}, nil
}

// newLineageFrameReplayReader consumes a strict physical index inventory fact.
func newLineageFrameReplayReader(source *evidenceVerifiedReplaySource, profile FrameReplayProfile) (*LineageFrameReplayReader, error) {
	replay, err := newFramedReplayReader(source, profile, maxLineageFrameBytes, lineageIndexContainer)
	if err != nil {
		return nil, closeReplaySourceAfterConstructorFailure(source, err)
	}
	return &LineageFrameReplayReader{replay: replay, source: source}, nil
}

func closeReplaySourceAfterConstructorFailure(source *evidenceVerifiedReplaySource, primary error) error {
	if source == nil {
		return primary
	}
	if closeErr := source.Close(); closeErr != nil {
		return fail(errorCodeOr(primary, CodeEvidenceJournalFailed), "replay-constructor-close", "replay source validation and close failed", errors.Join(primary, closeErr))
	}
	return primary
}

func errorCodeOr(err error, fallback ErrorCode) ErrorCode {
	for _, code := range []ErrorCode{CodeEvidenceJournalLimitExceeded, CodeEvidenceJournalCorrupt, CodeEvidenceJournalFailed} {
		if IsCode(err, code) {
			return code
		}
	}
	return fallback
}

// Close releases the verified source owned by the typed evidence reader. It
// is explicit so terminal replay classification remains observable before the
// caller chooses the close boundary.
func (reader *EvidenceFrameReplayReader) Close() error {
	if reader == nil || reader.closed || reader.source == nil {
		return frameIOFailure("evidence-replay-close", "reader is already closed", nil)
	}
	reader.closed = true
	err := reader.source.Close()
	reader.source = nil
	return err
}

// Close releases the verified source owned by the typed lineage reader.
func (reader *LineageFrameReplayReader) Close() error {
	if reader == nil || reader.closed || reader.source == nil {
		return frameIOFailure("lineage-replay-close", "reader is already closed", nil)
	}
	reader.closed = true
	err := reader.source.Close()
	reader.source = nil
	return err
}

func newFramedReplayReader(source *evidenceVerifiedReplaySource, profile FrameReplayProfile, maximumFrame uint64, expectedKind frameContainerKind) (framedReplayReader, error) {
	if source == nil {
		return framedReplayReader{}, frameIOFailure("replay-source", "verified source is unavailable", nil)
	}
	boundary, err := source.replayBoundary(expectedKind)
	if err != nil {
		return framedReplayReader{}, err
	}
	if !boundary.validFor(expectedKind) {
		return framedReplayReader{}, frameIOCorrupt("replay-boundary", nil)
	}
	if profile.ContainerMaximumBytes == 0 || profile.ContainerMaximumRecords == 0 || profile.GlobalMaximumBytes == 0 || profile.GlobalMaximumRecords == 0 {
		return framedReplayReader{}, frameIOLimit("replay-profile", "maximum is zero")
	}
	if profile.ContainerBytes > profile.ContainerMaximumBytes || profile.ContainerRecords > profile.ContainerMaximumRecords || profile.GlobalBytes > profile.GlobalMaximumBytes || profile.GlobalRecords > profile.GlobalMaximumRecords {
		return framedReplayReader{}, frameIOLimit("replay-profile", "observed usage exceeds maximum")
	}
	if boundary.observedPhysicalBytes > profile.ContainerMaximumBytes {
		return framedReplayReader{}, frameIOLimit("replay-profile", "observed container size exceeds maximum")
	}
	return framedReplayReader{reader: source, profile: profile, maximumFrame: maximumFrame, boundary: boundary}, nil
}

// Next returns exactly one evidence frame classification. After any terminal
// state, subsequent calls return the same class without touching the reader.
func (reader *EvidenceFrameReplayReader) Next() EvidenceFrameReplayResult {
	if reader == nil || reader.closed || reader.source == nil {
		return EvidenceFrameReplayResult{State: FrameReplayJournalFailed, Err: frameIOFailure("evidence-replay", "reader is unavailable", nil)}
	}
	if reader.terminal != nil {
		return *reader.terminal
	}
	raw := reader.replay.next()
	result := EvidenceFrameReplayResult{StartOffset: raw.startOffset, BytesRead: raw.bytesRead, Err: raw.err}
	if raw.state != rawFrameValid {
		result.State, result.Err = reader.replay.classifyTerminal(raw, "evidence-replay")
		reader.terminal = &result
		return result
	}
	frame, err := decodeReplayEvidenceFrame(raw.framed)
	if err != nil {
		reader.replay.terminal = true
		if IsCode(err, CodeEvidenceJournalLimitExceeded) {
			result.State = FrameReplayLimitExceeded
			result.Err = err
			reader.terminal = &result
			return result
		}
		result.State = FrameReplayCorrupt
		result.Err = frameIOCorrupt("evidence-replay", err)
		reader.terminal = &result
		return result
	}
	result.Frame = frame
	result.State = FrameReplayValid
	return result
}

// Next returns exactly one lineage frame classification.
func (reader *LineageFrameReplayReader) Next() LineageFrameReplayResult {
	if reader == nil || reader.closed || reader.source == nil {
		return LineageFrameReplayResult{State: FrameReplayJournalFailed, Err: frameIOFailure("lineage-replay", "reader is unavailable", nil)}
	}
	if reader.terminal != nil {
		return *reader.terminal
	}
	raw := reader.replay.next()
	result := LineageFrameReplayResult{StartOffset: raw.startOffset, BytesRead: raw.bytesRead, Err: raw.err}
	if raw.state != rawFrameValid {
		result.State, result.Err = reader.replay.classifyTerminal(raw, "lineage-replay")
		reader.terminal = &result
		return result
	}
	frame, err := decodeReplayLineageFrame(raw.framed)
	if err != nil {
		reader.replay.terminal = true
		if IsCode(err, CodeEvidenceJournalLimitExceeded) {
			result.State = FrameReplayLimitExceeded
			result.Err = err
			reader.terminal = &result
			return result
		}
		result.State = FrameReplayCorrupt
		result.Err = frameIOCorrupt("lineage-replay", err)
		reader.terminal = &result
		return result
	}
	result.Frame = frame
	result.State = FrameReplayValid
	return result
}

func decodeReplayEvidenceFrame(framed []byte) (*EvidenceFrame, error) {
	var frame EvidenceFrame
	if err := decodeCanonicalFramed(framed, maxEvidenceFrameBytes, &frame); err != nil {
		return nil, err
	}
	if maximum := evidenceRecordFrameLimits[frame.RecordKind]; maximum == 0 || uint64(len(framed)) > maximum {
		return nil, frameIOLimit("evidence-replay", "record-kind frame maximum exceeded")
	}
	if err := frame.Validate(); err != nil {
		return nil, err
	}
	return &frame, nil
}

func decodeReplayLineageFrame(framed []byte) (*LineageIndexFrame, error) {
	var frame LineageIndexFrame
	if err := decodeCanonicalFramed(framed, maxLineageFrameBytes, &frame); err != nil {
		return nil, err
	}
	if maximum := lineageRecordFrameLimits[frame.RecordKind]; maximum == 0 || uint64(len(framed)) > maximum {
		return nil, frameIOLimit("lineage-replay", "record-kind frame maximum exceeded")
	}
	if err := frame.Validate(); err != nil {
		return nil, err
	}
	return &frame, nil
}

func (reader *framedReplayReader) next() rawFrameReplayResult {
	start := reader.offset
	if reader.terminal {
		return rawFrameReplayResult{state: rawFrameJournalFailed, startOffset: start, err: frameIOFailure("replay", "reader is terminal", nil)}
	}
	if reader.offset > reader.boundary.observedPhysicalBytes {
		reader.terminal = true
		return rawFrameReplayResult{state: rawFrameCorrupt, startOffset: start, err: frameIOCorrupt("replay-boundary", nil)}
	}
	remainingPhysical := reader.boundary.observedPhysicalBytes - reader.offset
	if remainingPhysical == 0 {
		reader.terminal = true
		if err := requireReaderEOF(reader.reader); err != nil {
			if errors.Is(err, errFrameTrailingBytes) || IsCode(err, CodeEvidenceJournalCorrupt) {
				return rawFrameReplayResult{state: rawFrameCorrupt, startOffset: start, err: frameIOCorrupt("replay-boundary", err)}
			}
			return rawFrameReplayResult{state: rawFrameJournalFailed, startOffset: start, err: frameIOFailure("read-boundary", "journal end probe failed", err)}
		}
		return rawFrameReplayResult{state: rawFrameIncompletePrefix, startOffset: start}
	}

	prefix := make([]byte, 8)
	prefixTarget := uint64(len(prefix))
	if remainingPhysical < prefixTarget {
		prefixTarget = remainingPhysical
	}
	n, readErr := readFramePart(reader.reader, prefix[:int(prefixTarget)])
	reader.offset += uint64(n)
	if readErr != nil {
		reader.terminal = true
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			return rawFrameReplayResult{state: rawFrameIncompletePrefix, startOffset: start, bytesRead: uint64(n)}
		}
		return rawFrameReplayResult{state: rawFrameJournalFailed, startOffset: start, bytesRead: uint64(n), err: frameIOFailure("read-prefix", "journal read failed", readErr)}
	}
	if prefixTarget < 8 {
		reader.terminal = true
		if err := requireReaderEOF(reader.reader); err != nil {
			if errors.Is(err, errFrameTrailingBytes) || IsCode(err, CodeEvidenceJournalCorrupt) {
				return rawFrameReplayResult{state: rawFrameCorrupt, startOffset: start, bytesRead: uint64(n), err: frameIOCorrupt("replay-boundary", err)}
			}
			return rawFrameReplayResult{state: rawFrameJournalFailed, startOffset: start, bytesRead: uint64(n), err: frameIOFailure("read-boundary", "journal end probe failed", err)}
		}
		return rawFrameReplayResult{state: rawFrameIncompletePrefix, startOffset: start, bytesRead: uint64(n)}
	}

	payloadBytes := binary.BigEndian.Uint64(prefix)
	framedBytes, overflow := checkedFrameAdd(payloadBytes, 8)
	if overflow || payloadBytes > maxJSONInteger || framedBytes > reader.maximumFrame {
		reader.terminal = true
		return rawFrameReplayResult{state: rawFrameLimitExceeded, startOffset: start, bytesRead: 8, err: frameIOLimit("read-prefix", "declared frame length exceeds maximum")}
	}
	if err := reader.checkBudget(framedBytes); err != nil {
		reader.terminal = true
		return rawFrameReplayResult{state: rawFrameLimitExceeded, startOffset: start, bytesRead: 8, err: err}
	}

	// The maximum is checked before uint64-to-int conversion or allocation.
	framed := make([]byte, int(framedBytes))
	copy(framed, prefix)
	payloadTarget := payloadBytes
	remainingPhysical = reader.boundary.observedPhysicalBytes - reader.offset
	if remainingPhysical < payloadTarget {
		payloadTarget = remainingPhysical
	}
	n, readErr = readFramePart(reader.reader, framed[8:8+int(payloadTarget)])
	reader.offset += uint64(n)
	if readErr != nil {
		reader.terminal = true
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			return rawFrameReplayResult{state: rawFrameIncompletePayload, startOffset: start, bytesRead: 8 + uint64(n)}
		}
		return rawFrameReplayResult{state: rawFrameJournalFailed, startOffset: start, bytesRead: 8 + uint64(n), err: frameIOFailure("read-payload", "journal read failed", readErr)}
	}
	if payloadTarget < payloadBytes {
		reader.terminal = true
		if err := requireReaderEOF(reader.reader); err != nil {
			if errors.Is(err, errFrameTrailingBytes) || IsCode(err, CodeEvidenceJournalCorrupt) {
				return rawFrameReplayResult{state: rawFrameCorrupt, startOffset: start, bytesRead: 8 + uint64(n), err: frameIOCorrupt("replay-boundary", err)}
			}
			return rawFrameReplayResult{state: rawFrameJournalFailed, startOffset: start, bytesRead: 8 + uint64(n), err: frameIOFailure("read-boundary", "journal end probe failed", err)}
		}
		return rawFrameReplayResult{state: rawFrameIncompletePayload, startOffset: start, bytesRead: 8 + uint64(n)}
	}

	reader.profile.ContainerBytes += framedBytes
	reader.profile.ContainerRecords++
	reader.profile.GlobalBytes += framedBytes
	reader.profile.GlobalRecords++
	return rawFrameReplayResult{state: rawFrameValid, framed: framed, startOffset: start, bytesRead: framedBytes}
}

var errFrameTrailingBytes = errors.New("reader contains bytes beyond inventoried physical boundary")

func requireReaderEOF(reader io.Reader) error {
	var probe [1]byte
	n, err := reader.Read(probe[:])
	if n > 0 {
		return errFrameTrailingBytes
	}
	if err == nil {
		return io.ErrNoProgress
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (reader *framedReplayReader) classifyTerminal(raw rawFrameReplayResult, op string) (FrameReplayState, error) {
	switch raw.state {
	case rawFrameIncompletePrefix, rawFrameIncompletePayload:
		exactTail := raw.startOffset+raw.bytesRead == reader.boundary.observedPhysicalBytes
		if exactTail && raw.state == rawFrameIncompletePrefix && raw.bytesRead == 0 {
			return FrameReplayCleanEOF, nil
		}
		return FrameReplayCorrupt, frameIOCorrupt(op, nil)
	case rawFrameCorrupt:
		return FrameReplayCorrupt, raw.err
	case rawFrameLimitExceeded:
		return FrameReplayLimitExceeded, raw.err
	default:
		return FrameReplayJournalFailed, raw.err
	}
}

func (reader *framedReplayReader) checkBudget(frameBytes uint64) error {
	containerBytes, overflow := checkedFrameAdd(reader.profile.ContainerBytes, frameBytes)
	if overflow || containerBytes > reader.profile.ContainerMaximumBytes {
		return frameIOLimit("replay-budget", "container byte maximum exceeded")
	}
	containerRecords, overflow := checkedFrameAdd(reader.profile.ContainerRecords, 1)
	if overflow || containerRecords > reader.profile.ContainerMaximumRecords {
		return frameIOLimit("replay-budget", "container record maximum exceeded")
	}
	globalBytes, overflow := checkedFrameAdd(reader.profile.GlobalBytes, frameBytes)
	if overflow || globalBytes > reader.profile.GlobalMaximumBytes {
		return frameIOLimit("replay-budget", "global byte maximum exceeded")
	}
	globalRecords, overflow := checkedFrameAdd(reader.profile.GlobalRecords, 1)
	if overflow || globalRecords > reader.profile.GlobalMaximumRecords {
		return frameIOLimit("replay-budget", "global record maximum exceeded")
	}
	return nil
}

func checkedFrameAdd(left, right uint64) (uint64, bool) {
	result := left + right
	return result, result < left
}

// readFramePart fills destination without read-ahead. A non-EOF error is
// retained even when the same Read also returns bytes. Repeated zero progress
// is treated as an I/O failure instead of spinning.
func readFramePart(reader io.Reader, destination []byte) (int, error) {
	total := 0
	for total < len(destination) {
		n, err := reader.Read(destination[total:])
		if n < 0 || n > len(destination)-total {
			return total, errors.New("invalid Reader count")
		}
		total += n
		if err != nil {
			if err == io.EOF && total == len(destination) {
				return total, nil
			}
			if err == io.EOF {
				return total, io.ErrUnexpectedEOF
			}
			return total, err
		}
		if n == 0 {
			return total, io.ErrNoProgress
		}
	}
	return total, nil
}

// EncodeCanonicalEvidenceFrame and EncodeCanonicalLineageFrame produce the
// exact 8-byte big-endian length prefix followed by canonical RFC8785 bytes.
func EncodeCanonicalEvidenceFrame(frame EvidenceFrame) ([]byte, error) {
	if err := checkEncodedFrameLimit(frame, maxEvidenceFrameBytes, evidenceRecordFrameLimits[frame.RecordKind]); err != nil {
		return nil, err
	}
	if err := frame.Validate(); err != nil {
		return nil, err
	}
	return encodeCanonicalFrame(frame, maxEvidenceFrameBytes)
}

func EncodeCanonicalLineageFrame(frame LineageIndexFrame) ([]byte, error) {
	return encodeCanonicalLineageFrameForProfile(frame, EvidenceLimitsProfile)
}

func encodeCanonicalLineageFrameForProfile(frame LineageIndexFrame, profile string) ([]byte, error) {
	if !validEvidenceLimitsProfile(profile) {
		return nil, frameIOLimit("encode-frame", "lineage quota profile is unavailable")
	}
	recordMaximum := lineageRecordFrameLimits[frame.RecordKind]
	if frame.RecordKind == LineageRecordGenerationCheckpoint {
		var err error
		recordMaximum, err = checkpointMaximumForProfile(profile)
		if err != nil {
			return nil, err
		}
	}
	if err := checkEncodedFrameLimit(frame, maxLineageFrameBytes, recordMaximum); err != nil {
		return nil, err
	}
	if err := frame.Validate(); err != nil {
		return nil, err
	}
	return encodeCanonicalFrame(frame, maxLineageFrameBytes)
}

func checkEncodedFrameLimit(frame any, overallMaximum, recordMaximum uint64) error {
	if recordMaximum == 0 {
		return frameIOLimit("encode-frame", "record kind has no frame maximum")
	}
	canonical, err := canonicalTyped(frame)
	if err != nil {
		return err
	}
	framedBytes, overflow := checkedFrameAdd(uint64(len(canonical)), 8)
	if overflow || framedBytes > overallMaximum || framedBytes > recordMaximum {
		return frameIOLimit("encode-frame", "canonical frame exceeds maximum")
	}
	return nil
}

func encodeCanonicalFrame(frame any, maximum uint64) ([]byte, error) {
	canonical, err := canonicalTyped(frame)
	if err != nil {
		return nil, err
	}
	framedBytes, overflow := checkedFrameAdd(uint64(len(canonical)), 8)
	if overflow || framedBytes > maximum {
		return nil, frameIOLimit("encode-frame", "canonical frame exceeds maximum")
	}
	framed := make([]byte, int(framedBytes))
	binary.BigEndian.PutUint64(framed[:8], uint64(len(canonical)))
	copy(framed[8:], canonical)
	return framed, nil
}

func WriteCanonicalEvidenceFrame(writer io.Writer, frame EvidenceFrame) (int, error) {
	framed, err := EncodeCanonicalEvidenceFrame(frame)
	if err != nil {
		return 0, err
	}
	return writeCanonicalFrame(writer, framed)
}

func WriteCanonicalLineageFrame(writer io.Writer, frame LineageIndexFrame) (int, error) {
	framed, err := EncodeCanonicalLineageFrame(frame)
	if err != nil {
		return 0, err
	}
	return writeCanonicalFrame(writer, framed)
}

func writeCanonicalFrame(writer io.Writer, framed []byte) (int, error) {
	if writer == nil {
		return 0, frameIOFailure("write-frame", "writer is unavailable", nil)
	}
	written, err := writeAll(writer, framed)
	if err != nil {
		return written, frameIOFailure("write-frame", "journal write failed", err)
	}
	return written, nil
}

func writeAll(writer io.Writer, data []byte) (int, error) {
	total := 0
	for total < len(data) {
		n, err := writer.Write(data[total:])
		if n < 0 || n > len(data)-total {
			return total, io.ErrShortWrite
		}
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func frameIOFailure(op, message string, err error) error {
	return fail(CodeEvidenceJournalFailed, op, message, err)
}

func frameIOCorrupt(op string, err error) error {
	return fail(CodeEvidenceJournalCorrupt, op, "complete frame is invalid", err)
}

func frameIOLimit(op, message string) error {
	return fail(CodeEvidenceJournalLimitExceeded, op, message, nil)
}
