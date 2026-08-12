//go:build !linux

package migration

import "context"

func newProductionEvidenceFSRoot(context.Context, string) (*evidenceFSRoot, error) {
	return nil, filesystemFailure("platform", "production evidence filesystem is unsupported on this platform")
}

func supportedEvidenceFilesystem(int64) bool { return false }
