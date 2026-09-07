package dockertarget

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSnapshotFoundationWorkspaceCopiesAndVerifiesArchiveWithoutStartingHelper(t *testing.T) {
	input := FoundationWorkspaceSnapshot{
		TenantID: "tenant", ProjectID: "project", TargetID: "target", WorkspaceID: "workspace",
		SnapshotID: "snapshot", ImageURI: "registry.test/runtime@sha256:" + strings.Repeat("a", 64),
	}
	input.SourceVolumeName = (FoundationWorkspaceVolume{input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID}).Name()
	archive := snapshotTestArchive(t, time.Unix(100, 0))
	verifiedArchive := snapshotTestArchive(t, time.Unix(200, 0))
	volumes := map[string]map[string]string{input.SourceVolumeName: (FoundationWorkspaceVolume{input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID}).labels()}
	helper := false
	starts := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/volumes/"):
			name := strings.TrimPrefix(request.URL.Path, "/volumes/")
			labels, ok := volumes[name]
			if !ok {
				http.NotFound(writer, request)
				return
			}
			_ = json.NewEncoder(writer).Encode(volumeInspect{Name: name, Labels: labels})
		case request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/volumes/"):
			delete(volumes, strings.TrimPrefix(request.URL.Path, "/volumes/"))
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPost && request.URL.Path == "/volumes/create":
			var body struct {
				Name   string            `json:"Name"`
				Labels map[string]string `json:"Labels"`
			}
			_ = json.NewDecoder(request.Body).Decode(&body)
			volumes[body.Name] = body.Labels
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(volumeInspect{Name: body.Name, Labels: body.Labels})
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/containers/") && strings.HasSuffix(request.URL.Path, "/json"):
			if !helper {
				http.NotFound(writer, request)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"Config": map[string]any{"Labels": input.labels()}})
		case request.Method == http.MethodPost && request.URL.Path == "/containers/create":
			helper = true
			writer.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(writer, `{"Id":"helper-id"}`)
		case request.Method == http.MethodDelete && strings.HasPrefix(request.URL.Path, "/containers/"):
			helper = false
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/archive"):
			writer.WriteHeader(http.StatusOK)
			if request.URL.Query().Get("path") == "/source/." {
				_, _ = writer.Write(archive)
			} else {
				_, _ = writer.Write(verifiedArchive)
			}
		case request.Method == http.MethodPut && strings.HasSuffix(request.URL.Path, "/archive"):
			body, _ := io.ReadAll(request.Body)
			if !bytes.Equal(body, archive) {
				t.Errorf("archive mismatch: %q", body)
			}
			writer.WriteHeader(http.StatusOK)
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/start"):
			starts++
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	directory := credentialDirectoryForIsolationTest(t, server)
	result, err := directory.SnapshotFoundationWorkspace(context.Background(), server.URL, "docker", input)
	if err != nil || result.VolumeName != input.Name() || result.SizeBytes != int64(len(archive)) || result.ContentDigest == "" || !result.CleanupComplete {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if starts != 0 || helper || volumes[input.Name()] == nil {
		t.Fatalf("starts=%d helper=%t snapshot=%v", starts, helper, volumes[input.Name()])
	}
}

func snapshotTestArchive(t *testing.T, modified time.Time) []byte {
	t.Helper()
	var result bytes.Buffer
	writer := tar.NewWriter(&result)
	content := []byte("exact-offline-data")
	if err := writer.WriteHeader(&tar.Header{Name: "proof.txt", Mode: 0o640, Size: int64(len(content)), ModTime: modified}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}
