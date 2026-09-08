package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCertificateRotationDue(t *testing.T) {
	now := time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC)
	if certificateRotationDue(now.Add(6*time.Minute), now, 5*time.Minute, false) ||
		!certificateRotationDue(now.Add(5*time.Minute), now, 5*time.Minute, false) ||
		!certificateRotationDue(now.Add(time.Hour), now, 5*time.Minute, true) {
		t.Fatal("certificate rotation deadline mismatch")
	}
}

func TestPendingCertificateRotationSurvivesRestart(t *testing.T) {
	directory := t.TempDir()
	value := config{
		enrollmentID:                   "enrollment-alpha",
		incarnationID:                  "incarnation-alpha",
		certificateResourceVersionFile: filepath.Join(directory, "identity.resource-version"),
	}
	first, err := loadOrCreatePendingCertificateRotation(value, 3)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadOrCreatePendingCertificateRotation(value, 3)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("pending rotation changed across restart: equal=%v err=%v", reflect.DeepEqual(first, second), err)
	}
	info, err := os.Stat(value.certificateResourceVersionFile + ".pending")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("pending rotation mode=%v", info.Mode().Perm())
	}
	if err := writePrivateFile(value.certificateResourceVersionFile, []byte("4\n")); err != nil {
		t.Fatal(err)
	}
	third, err := loadOrCreatePendingCertificateRotation(value, 4)
	if err != nil || third.ExpectedResourceVersion != 4 || reflect.DeepEqual(first, third) {
		t.Fatalf("stale completed rotation was not replaced: value=%+v err=%v", third, err)
	}
}
