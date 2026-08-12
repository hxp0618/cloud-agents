//go:build !linux

package migration

import (
	"context"
	"testing"
)

func TestProductionEvidenceFilesystemUnsupported(t *testing.T) {
	if supportedEvidenceFilesystem(fakeSupportedFilesystem) {
		t.Fatal("non-linux production filesystem became supported")
	}
	if _, err := newProductionEvidenceFSRoot(context.Background(), "/tmp"); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("got %v", err)
	}
}
