package migration

import (
	"bytes"
	"fmt"
)

type SchemaLineage struct {
	Current       *SchemaBundleDocument
	Ancestors     []*SchemaBundleDocument // newest predecessor to oldest
	indexByDigest map[Digest]int          // oldest is zero
}

func validateSchemaLineage(current *SchemaBundleDocument, files map[string][]byte) (*SchemaLineage, error) {
	lineage := &SchemaLineage{Current: current, indexByDigest: make(map[Digest]int)}
	seenDigest := map[Digest]struct{}{current.SchemaBundleDigest: {}}
	seenPath := make(map[string]struct{})
	next := current.SchemaBundle.PredecessorSchemaBundle
	newer := current
	for next != nil {
		if len(lineage.Ancestors) >= 128 {
			return nil, fail(CodeInvalidLineage, "ancestor", "ancestor depth exceeds 128", nil)
		}
		if _, exists := seenDigest[next.SchemaBundleDigest]; exists {
			return nil, fail(CodeInvalidLineage, "ancestor", "ancestor digest cycle or duplicate", nil)
		}
		if _, exists := seenPath[next.Path]; exists {
			return nil, fail(CodeInvalidLineage, "ancestor", "ancestor path cycle or duplicate", nil)
		}
		raw, exists := files[next.Path]
		if !exists {
			return nil, fail(CodeInvalidLineage, next.Path, "ancestor archive is missing", nil)
		}
		if uint64(len(raw)) != next.SizeBytes || DigestBytes(raw) != next.SHA256 {
			return nil, fail(CodeInvalidLineage, next.Path, "ancestor raw size or checksum mismatch", nil)
		}
		document, err := DecodeSchemaBundleDocument(raw)
		if err != nil {
			return nil, err
		}
		if document.SchemaBundleDigest != next.SchemaBundleDigest {
			return nil, fail(CodeInvalidLineage, next.Path, "ancestor schema digest mismatch", nil)
		}
		if err := strictMigrationPrefix(document.SchemaBundle.Migrations, newer.SchemaBundle.Migrations); err != nil {
			return nil, err
		}
		lineage.Ancestors = append(lineage.Ancestors, document)
		seenDigest[next.SchemaBundleDigest] = struct{}{}
		seenPath[next.Path] = struct{}{}
		newer = document
		next = document.SchemaBundle.PredecessorSchemaBundle
	}
	all := make([]*SchemaBundleDocument, 0, len(lineage.Ancestors)+1)
	for index := len(lineage.Ancestors) - 1; index >= 0; index-- {
		all = append(all, lineage.Ancestors[index])
	}
	all = append(all, current)
	for index, document := range all {
		lineage.indexByDigest[document.SchemaBundleDigest] = index
	}
	return lineage, nil
}

func strictMigrationPrefix(older, newer []MigrationEntry) error {
	if len(older) >= len(newer) {
		return fail(CodeInvalidLineage, "ancestor", "predecessor migration list is not a strict prefix", nil)
	}
	for index := range older {
		left, err := canonicalTyped(older[index])
		if err != nil {
			return err
		}
		right, err := canonicalTyped(newer[index])
		if err != nil {
			return err
		}
		if !bytes.Equal(left, right) {
			return fail(CodeInvalidLineage, older[index].ID, "ancestor entry was mutated", nil)
		}
	}
	return nil
}

func (lineage *SchemaLineage) BundleIndex(digest Digest) (int, bool) {
	index, ok := lineage.indexByDigest[digest]
	return index, ok
}

func (lineage *SchemaLineage) EntryForDigest(digest Digest, id string) (*MigrationEntry, error) {
	documents := append([]*SchemaBundleDocument{lineage.Current}, lineage.Ancestors...)
	for _, document := range documents {
		if document.SchemaBundleDigest != digest {
			continue
		}
		for index := range document.SchemaBundle.Migrations {
			if document.SchemaBundle.Migrations[index].ID == id {
				return &document.SchemaBundle.Migrations[index], nil
			}
		}
		return nil, fail(CodeInvalidLedger, id, "bundle does not contain the claimed migration", nil)
	}
	return nil, fail(CodeInvalidLedger, fmt.Sprintf("%s/%s", digest, id), "ledger references an unknown bundle", nil)
}
