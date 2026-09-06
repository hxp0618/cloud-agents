package opensandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func identity() Identity {
	return Identity{"tenant", "project", "workspace", "sandbox", "operation", 1, "sha256:" + strings.Repeat("a", 64)}
}

func TestReceiptGuards(t *testing.T) {
	id := identity()
	for _, test := range []struct {
		name      string
		change    func(*sandbox)
		duplicate bool
		want      error
	}{
		{"running is only runtime state", func(*sandbox) {}, false, nil},
		{"tenant", func(s *sandbox) { s.Metadata["cloud-agents-tenant-sha256-a"] = "other" }, false, ErrConflict},
		{"tenant second half", func(s *sandbox) { s.Metadata["cloud-agents-tenant-sha256-b"] = "other" }, false, ErrConflict},
		{"encoding version", func(s *sandbox) { delete(s.Metadata, "cloud-agents-receipt-version") }, false, ErrConflict},
		{"generation", func(s *sandbox) { s.Metadata["cloud-agents-generation"] = "2" }, false, ErrConflict},
		{"digest first half", func(s *sandbox) { s.Metadata["cloud-agents-spec-sha256-a"] = "other" }, false, ErrConflict},
		{"digest second half", func(s *sandbox) { s.Metadata["cloud-agents-spec-sha256-b"] = "other" }, false, ErrConflict},
		{"unknown state", func(s *sandbox) { s.Status.State = "future" }, false, ErrUnavailable},
		{"unsafe ID", func(s *sandbox) { s.ID = "../escape" }, false, ErrUnavailable},
		{"duplicate", func(*sandbox) {}, true, ErrConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := sandbox{ID: "physical-1", Metadata: id.Labels()}
			item.Status.State = "Running"
			test.change(&item)
			deletes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("OPEN-SANDBOX-API-KEY") != "private-key" {
					t.Error("missing credential")
				}
				if r.Method == "DELETE" {
					deletes++
					w.WriteHeader(204)
					return
				}
				if r.URL.Path == "/v1/sandboxes" {
					items := []sandbox{item}
					if test.duplicate {
						items = append(items, item)
					}
					json.NewEncoder(w).Encode(map[string]any{"items": items, "pagination": map[string]any{"page": 1, "hasNextPage": false}})
				} else {
					json.NewEncoder(w).Encode(item)
				}
			}))
			defer server.Close()
			client, err := New(server.URL, "private-key")
			if err != nil {
				t.Fatal(err)
			}
			observation, err := client.Find(context.Background(), id)
			if !errors.Is(err, test.want) {
				t.Fatalf("Find: %v", err)
			}
			if err == nil && observation.RuntimeState != "Running" {
				t.Fatal(observation)
			}
			if !test.duplicate {
				err = client.Delete(context.Background(), id, "physical-1")
				if test.name == "unsafe ID" {
					if !errors.Is(err, ErrConflict) {
						t.Fatal(err)
					}
				} else if !errors.Is(err, test.want) {
					t.Fatal(err)
				}
				if test.want != nil && deletes != 0 {
					t.Fatal("unowned delete")
				}
			}
		})
	}
}

func TestTransportAndPaginationFailClosed(t *testing.T) {
	for _, endpoint := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/base", "https://example.com?q=secret", "file:///tmp/api"} {
		if _, err := New(endpoint, "key"); !errors.Is(err, ErrInvalid) {
			t.Fatal(endpoint, err)
		}
	}
	for _, body := range []string{`{"items":[]}`, `{"items":[],"pagination":{"page":1}}`, strings.Repeat("x", (1<<20)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		client, _ := New(server.URL, "key")
		_, err := client.Find(context.Background(), identity())
		server.Close()
		if !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
	requests := 0
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer sink.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, sink.URL, 302) }))
	defer redirect.Close()
	client, _ := New(redirect.URL, "private-key")
	_, err := client.Find(context.Background(), identity())
	if !errors.Is(err, ErrUnavailable) || requests != 0 {
		t.Fatal("credential redirect", err, requests)
	}
	pageCount := 0
	pages := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCount++
		items := []sandbox{}
		if pageCount == 2 {
			item := sandbox{ID: "physical-1", Metadata: identity().Labels()}
			item.Status.State = "Failed"
			items = append(items, item)
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items, "pagination": map[string]any{"page": pageCount, "hasNextPage": pageCount == 1}})
	}))
	defer pages.Close()
	client, _ = New(pages.URL, "key")
	observation, err := client.Find(context.Background(), identity())
	if err != nil || observation.RuntimeState != "Failed" || pageCount != 2 {
		t.Fatal(observation, err, pageCount)
	}
}

func TestInvalidIdentityAndErrorRedaction(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; http.Error(w, "upstream-secret-bytes", 500) }))
	defer server.Close()
	client, _ := New(server.URL, "private-key")
	invalid := identity()
	invalid.Tenant = strings.Repeat("a", 129)
	if invalid.Labels() != nil {
		t.Fatal("invalid labels produced")
	}
	if _, err := client.Find(context.Background(), invalid); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := client.Delete(context.Background(), identity(), "../escape"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatal("invalid request sent")
	}
	_, err := client.Find(context.Background(), identity())
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret") {
		t.Fatal("upstream error exposed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Find(ctx, identity()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPublicIdentifiersInReceiptLabels(t *testing.T) {
	id := identity()
	id.Tenant = "A" + strings.Repeat("~", 126) + "Z"
	id.Sandbox = id.Tenant
	labels := id.Labels()
	if len(labels) != 14 {
		t.Fatal("missing receipt fields", len(labels))
	}
	for _, value := range labels {
		if len(value) > 63 || strings.Contains(value, "~") {
			t.Fatal("not a candidate label value")
		}
	}
	for _, value := range []string{"", "_a", "a~", strings.Repeat("a", 129), "a/b", "中文", "a\n"} {
		invalid := id
		invalid.Workspace = value
		if invalid.Labels() != nil {
			t.Fatal("accepted invalid public identifier")
		}
	}
	other := id
	other.Tenant = strings.ToLower(id.Tenant)
	if other.Labels()["cloud-agents-tenant-sha256-a"] == labels["cloud-agents-tenant-sha256-a"] {
		t.Fatal("identifier case normalized")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter, err := url.ParseQuery(r.URL.Query().Get("metadata"))
		if err != nil || len(filter) != 2 || filter.Get("cloud-agents-sandbox-sha256-a") != labels["cloud-agents-sandbox-sha256-a"] || filter.Get("cloud-agents-sandbox-sha256-b") != labels["cloud-agents-sandbox-sha256-b"] {
			t.Error("discovery does not bind full sandbox digest")
		}
		item := sandbox{ID: "physical-1", Metadata: labels}
		item.Status.State = "Running"
		json.NewEncoder(w).Encode(map[string]any{"items": []sandbox{item}, "pagination": map[string]any{"page": 1, "hasNextPage": false}})
	}))
	defer server.Close()
	client, _ := New(server.URL, "key")
	if _, err := client.Find(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}

func TestCreateAdoptsOrCreatesRetainedVolume(t *testing.T) {
	for _, existing := range []bool{true, false} {
		t.Run(map[bool]string{true: "adopt", false: "create"}[existing], func(t *testing.T) {
			id := identity()
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet && request.URL.Path == "/v1/sandboxes" {
					items := []sandbox{}
					if existing {
						item := sandbox{ID: "physical-1", Metadata: id.Labels()}
						item.Status.State = "Running"
						items = append(items, item)
					}
					_ = json.NewEncoder(writer).Encode(map[string]any{"items": items, "pagination": map[string]any{"page": 1, "hasNextPage": false}})
					return
				}
				if request.Method != http.MethodPost || request.URL.Path != "/v1/sandboxes" {
					http.NotFound(writer, request)
					return
				}
				posts++
				var body struct {
					Image          map[string]string `json:"image"`
					Entrypoint     []string          `json:"entrypoint"`
					Timeout        *int64            `json:"timeout"`
					ResourceLimits map[string]string `json:"resourceLimits"`
					Volumes        []struct {
						Name      string `json:"name"`
						MountPath string `json:"mountPath"`
						ReadOnly  bool   `json:"readOnly"`
						PVC       struct {
							ClaimName                  string `json:"claimName"`
							CreateIfNotExists          bool   `json:"createIfNotExists"`
							DeleteOnSandboxTermination bool   `json:"deleteOnSandboxTermination"`
						} `json:"pvc"`
					} `json:"volumes"`
				}
				if json.NewDecoder(request.Body).Decode(&body) != nil || body.Image["uri"] != "node@sha256:"+strings.Repeat("b", 64) ||
					strings.Join(body.Entrypoint, " ") != "sleep infinity" || body.Timeout != nil ||
					body.ResourceLimits["cpu"] != "500m" || body.ResourceLimits["memory"] != "536870912" ||
					len(body.Volumes) != 1 || body.Volumes[0].Name != "workspace" || body.Volumes[0].MountPath != "/workspace" ||
					body.Volumes[0].ReadOnly || body.Volumes[0].PVC.ClaimName != "ca-ws-volume" ||
					body.Volumes[0].PVC.CreateIfNotExists || body.Volumes[0].PVC.DeleteOnSandboxTermination {
					t.Error("unsafe create body")
				}
				item := sandbox{ID: "physical-1", Metadata: id.Labels()}
				item.Status.State = "Pending"
				writer.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(writer).Encode(item)
			}))
			defer server.Close()
			client, _ := New(server.URL, "private-key")
			observation, err := client.Create(context.Background(), CreateInput{
				Identity: id, ImageURI: "node@sha256:" + strings.Repeat("b", 64), VolumeName: "ca-ws-volume",
				CPUMillis: 500, MemoryBytes: 512 << 20,
			})
			if err != nil || observation.RuntimeID != "physical-1" || posts != map[bool]int{true: 0, false: 1}[existing] {
				t.Fatalf("observation = %#v, posts = %d, error = %v", observation, posts, err)
			}
		})
	}
}

func TestNetworkPolicyCreateAndVerification(t *testing.T) {
	id := identity()
	policy := &NetworkPolicy{DefaultAction: "deny", Egress: []NetworkRule{
		{Action: "allow", Target: "10.0.0.0/8"},
		{Action: "allow", Target: "api.openai.com"},
	}}
	mode := "dns+nft"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("OPEN-SANDBOX-API-KEY") != "private-key" {
			t.Error("missing candidate credential")
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sandboxes":
			_ = json.NewEncoder(writer).Encode(map[string]any{"items": []sandbox{}, "pagination": map[string]any{"page": 1, "hasNextPage": false}})
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sandboxes":
			var body struct {
				NetworkPolicy *NetworkPolicy `json:"networkPolicy"`
			}
			if json.NewDecoder(request.Body).Decode(&body) != nil || body.NetworkPolicy == nil ||
				body.NetworkPolicy.DefaultAction != policy.DefaultAction || len(body.NetworkPolicy.Egress) != len(policy.Egress) ||
				body.NetworkPolicy.Egress[0] != policy.Egress[0] || body.NetworkPolicy.Egress[1] != policy.Egress[1] {
				t.Errorf("network policy body = %+v", body.NetworkPolicy)
			}
			item := sandbox{ID: "physical-1", Metadata: id.Labels()}
			item.Status.State = "Pending"
			writer.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(writer).Encode(item)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sandboxes/physical-1":
			item := sandbox{ID: "physical-1", Metadata: id.Labels()}
			item.Status.State = "Running"
			_ = json.NewEncoder(writer).Encode(item)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sandboxes/physical-1/endpoints/18080":
			if request.URL.Query().Get("use_server_proxy") != "true" {
				t.Error("policy endpoint did not require the candidate server proxy")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"endpoint": server.URL + "/v1/sandboxes/physical-1/proxy/18080",
				"headers":  map[string]string{"X-Route": "owned"},
			})
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sandboxes/physical-1/proxy/18080/policy":
			if request.Header.Get("X-Route") != "owned" {
				t.Error("missing candidate route credential")
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"status": "ok", "enforcementMode": mode, "policy": policy})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, "private-key")
	created, err := client.Create(context.Background(), CreateInput{
		Identity: id, ImageURI: "node@sha256:" + strings.Repeat("b", 64), VolumeName: "ca-ws-volume",
		CPUMillis: 500, MemoryBytes: 512 << 20, NetworkPolicy: policy,
	})
	if err != nil || created.RuntimeID != "physical-1" {
		t.Fatalf("create = %+v, %v", created, err)
	}
	if err := client.VerifyNetworkPolicy(context.Background(), id, "physical-1", policy); err != nil {
		t.Fatal(err)
	}
	mode = "dns"
	if err := client.VerifyNetworkPolicy(context.Background(), id, "physical-1", policy); !errors.Is(err, ErrPolicyUnenforced) {
		t.Fatalf("partial enforcement error = %v", err)
	}
}

func TestWaitReadyRequiresExecdHealth(t *testing.T) {
	id := identity()
	state := "Running"
	healthy := true
	pings := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/sandboxes/physical-1":
			item := sandbox{ID: "physical-1", Metadata: id.Labels()}
			item.Status.State = state
			_ = json.NewEncoder(writer).Encode(item)
		case "/v1/sandboxes/physical-1/endpoints/44772":
			_ = json.NewEncoder(writer).Encode(map[string]any{"endpoint": server.URL + "/proxy/44772", "headers": map[string]string{"X-Route": "owned"}})
		case "/ping":
			pings++
			if request.Header.Get("X-Route") != "owned" {
				t.Error("missing endpoint header")
			}
			if !healthy {
				writer.WriteHeader(http.StatusServiceUnavailable)
			}
			_, _ = writer.Write([]byte("pong"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, "private-key")
	observation, err := client.WaitReady(context.Background(), id, "physical-1")
	if err != nil || observation.RuntimeState != "Running" || pings != 1 {
		t.Fatalf("observation = %#v, pings = %d, error = %v", observation, pings, err)
	}
	healthy = false
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	observation, err = client.WaitReady(ctx, id, "physical-1")
	if !errors.Is(err, context.DeadlineExceeded) || observation.RuntimeID != "physical-1" || observation.RuntimeState != "Running" {
		t.Fatalf("timed out observation = %#v, error = %v", observation, err)
	}
	state = "Failed"
	if _, err := client.WaitReady(context.Background(), id, "physical-1"); !errors.Is(err, ErrRuntimeFailed) {
		t.Fatal(err)
	}
}

func TestExecUsesExactReceiptAndBoundsOutput(t *testing.T) {
	id := identity()
	overflow, commandFailed, commands, statuses := false, false, 0, 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/sandboxes/physical-1":
			item := sandbox{ID: "physical-1", Metadata: id.Labels()}
			item.Status.State = "Running"
			_ = json.NewEncoder(writer).Encode(item)
		case "/v1/sandboxes/physical-1/endpoints/44772":
			_ = json.NewEncoder(writer).Encode(map[string]any{"endpoint": server.URL, "headers": map[string]string{"X-EXECD-ACCESS-TOKEN": "owned"}})
		case "/command":
			commands++
			if request.Method != http.MethodPost || request.Header.Get("X-EXECD-ACCESS-TOKEN") != "owned" || request.Header.Get("Content-Type") != "application/json" {
				t.Error("unsafe exec request")
			}
			var body struct {
				Command    string `json:"command"`
				Cwd        string `json:"cwd"`
				Background bool   `json:"background"`
				Timeout    int64  `json:"timeout"`
			}
			if json.NewDecoder(request.Body).Decode(&body) != nil || body.Command != "printf bounded" || body.Cwd != "/workspace" || body.Background || body.Timeout != 1000 {
				t.Errorf("exec body = %+v", body)
			}
			writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			_ = json.NewEncoder(writer).Encode(map[string]any{"type": "init", "text": "command-1"})
			text := "stdout"
			if overflow {
				text = strings.Repeat("x", maxExecOutputBytes+1)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"type": "stdout", "text": text})
			if !overflow {
				_ = json.NewEncoder(writer).Encode(map[string]any{"type": "stderr", "text": "stderr"})
				if commandFailed {
					_ = json.NewEncoder(writer).Encode(map[string]any{"type": "error", "error": map[string]any{"ename": "CommandExecError", "evalue": "7"}})
				} else {
					_ = json.NewEncoder(writer).Encode(map[string]any{"type": "execution_complete", "execution_time": 12})
				}
			}
		case "/command/status/command-1":
			statuses++
			if request.Header.Get("X-EXECD-ACCESS-TOKEN") != "owned" {
				t.Error("missing status credential")
			}
			exitCode := 0
			if commandFailed {
				exitCode = 7
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": "command-1", "running": false, "exit_code": exitCode})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, "private-key")
	input := ExecInput{Identity: id, RuntimeID: "physical-1", Command: "printf bounded", Timeout: time.Second}
	result, err := client.Exec(context.Background(), input)
	if err != nil || result.Stdout != "stdout" || result.Stderr != "stderr" || result.ExitCode != 0 || result.ExecutionTimeMillis != 12 || commands != 1 || statuses != 1 {
		t.Fatalf("result=%+v commands=%d statuses=%d err=%v", result, commands, statuses, err)
	}
	commandFailed = true
	result, err = client.Exec(context.Background(), input)
	if err != nil || result.ExitCode != 7 || result.ExecutionTimeMillis != 0 || commands != 2 || statuses != 2 {
		t.Fatalf("non-zero result=%+v commands=%d statuses=%d err=%v", result, commands, statuses, err)
	}
	overflow = true
	if _, err := client.Exec(context.Background(), input); !errors.Is(err, ErrOutputLimit) || commands != 3 || statuses != 2 {
		t.Fatalf("output limit commands=%d statuses=%d err=%v", commands, statuses, err)
	}
	input.Identity.Generation++
	if _, err := client.Exec(context.Background(), input); !errors.Is(err, ErrConflict) || commands != 3 {
		t.Fatalf("stale receipt commands=%d err=%v", commands, err)
	}
}

func TestPreviewUsesOnlyExactCandidateServerProxy(t *testing.T) {
	id := identity()
	unsafeTarget, bareTarget := false, false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/sandboxes/physical-1":
			item := sandbox{ID: "physical-1", Metadata: id.Labels()}
			item.Status.State = "Running"
			_ = json.NewEncoder(writer).Encode(item)
		case "/v1/sandboxes/physical-1/endpoints/3000":
			if request.URL.Query().Get("use_server_proxy") != "true" {
				t.Error("Preview endpoint did not require the candidate server proxy")
			}
			endpoint := server.URL + "/v1/sandboxes/physical-1/proxy/3000"
			if bareTarget {
				base, _ := url.Parse(server.URL)
				endpoint = base.Scheme + "://" + base.Hostname() + "/sandboxes/physical-1/proxy/3000"
			}
			if unsafeTarget {
				endpoint = "https://example.com/proxy/3000"
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"endpoint": endpoint, "headers": map[string]string{"X-Route": "owned"}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, "private-key")
	target, headers, err := client.PreviewHTTPProxyTarget(context.Background(), PTYInput{Identity: id, RuntimeID: "physical-1"}, 3000)
	if err != nil || target.String() != server.URL+"/v1/sandboxes/physical-1/proxy/3000" ||
		headers.Get("X-Route") != "owned" || headers.Get("OPEN-SANDBOX-API-KEY") != "private-key" {
		t.Fatalf("target=%v headers=%v err=%v", target, headers, err)
	}
	bareTarget = true
	if target, _, err = client.PreviewHTTPProxyTarget(context.Background(), PTYInput{Identity: id, RuntimeID: "physical-1"}, 3000); err != nil || target.String() != server.URL+"/v1/sandboxes/physical-1/proxy/3000" {
		t.Fatalf("bare advertised target=%v err=%v", target, err)
	}
	unsafeTarget = true
	if _, _, err := client.PreviewHTTPProxyTarget(context.Background(), PTYInput{Identity: id, RuntimeID: "physical-1"}, 3000); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unsafe target error = %v", err)
	}
	if _, _, err := client.PreviewHTTPProxyTarget(context.Background(), PTYInput{Identity: id, RuntimeID: "physical-1"}, 44772); !errors.Is(err, ErrInvalid) {
		t.Fatalf("internal port error = %v", err)
	}
}

func TestFilesStayInsideWorkspaceAndPreserveVersions(t *testing.T) {
	id := identity()
	modified := time.Date(2026, 9, 6, 1, 2, 3, 4, time.UTC)
	content := []byte("hello")
	deleted := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		info := func(filePath, fileType string, size int64) candidateFileInfo {
			return candidateFileInfo{Path: filePath, Type: fileType, Size: size, ModifiedAt: modified}
		}
		switch request.URL.Path {
		case "/v1/sandboxes/physical-1":
			item := sandbox{ID: "physical-1", Metadata: id.Labels()}
			item.Status.State = "Running"
			_ = json.NewEncoder(writer).Encode(item)
		case "/v1/sandboxes/physical-1/endpoints/44772":
			_ = json.NewEncoder(writer).Encode(map[string]any{"endpoint": server.URL, "headers": map[string]string{"X-EXECD-ACCESS-TOKEN": "owned"}})
		case "/files/info":
			if request.Header.Get("X-EXECD-ACCESS-TOKEN") != "owned" {
				t.Error("missing endpoint credential")
			}
			filePath := request.URL.Query().Get("path")
			var value candidateFileInfo
			switch filePath {
			case "/workspace":
				value = info(filePath, "directory", 0)
			case "/workspace/link":
				value = info(filePath, "symlink", 0)
			case "/workspace/notes.txt":
				if deleted {
					http.NotFound(writer, request)
					return
				}
				value = info(filePath, "file", int64(len(content)))
			default:
				http.NotFound(writer, request)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]candidateFileInfo{filePath: value})
		case "/directories/list":
			if request.URL.Query().Get("path") != "/workspace" || request.URL.Query().Get("depth") != "1" {
				t.Error("unsafe list scope")
			}
			_ = json.NewEncoder(writer).Encode([]candidateFileInfo{
				info("/workspace/notes.txt", "file", int64(len(content))),
				info("/workspace/link", "symlink", 0),
			})
		case "/files/download":
			parts := strings.Split(strings.TrimPrefix(request.Header.Get("Range"), "bytes="), "-")
			if len(parts) != 2 {
				t.Error("invalid Range")
				return
			}
			start, startErr := strconv.Atoi(parts[0])
			end, endErr := strconv.Atoi(parts[1])
			if startErr != nil || endErr != nil || start < 0 || end >= len(content) || start > end {
				t.Error("invalid Range")
				return
			}
			writer.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(content)))
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write(content[start : end+1])
		case "/files/upload":
			if request.Method != http.MethodPost || request.ParseMultipartForm(17<<20) != nil {
				t.Error("invalid upload")
				return
			}
			metadata, _, err := request.FormFile("metadata")
			if err != nil {
				t.Error(err)
				return
			}
			var spec struct {
				Path string `json:"path"`
				Mode int    `json:"mode"`
			}
			decodeErr := json.NewDecoder(metadata).Decode(&spec)
			_ = metadata.Close()
			file, _, err := request.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			uploaded, readErr := io.ReadAll(file)
			_ = file.Close()
			if decodeErr != nil || readErr != nil || spec.Path != "/workspace/notes.txt" || spec.Mode != 600 {
				t.Error("unsafe upload metadata")
				return
			}
			content, deleted = uploaded, false
			_, _ = writer.Write([]byte("{}"))
		case "/files":
			if request.Method != http.MethodDelete || request.URL.Query().Get("path") != "/workspace/notes.txt" {
				t.Error("unsafe delete")
				return
			}
			deleted = true
			_, _ = writer.Write([]byte("{}"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, "private-key")
	input := PTYInput{Identity: id, RuntimeID: "physical-1"}
	entries, err := client.ListFiles(context.Background(), input, ".")
	if err != nil || len(entries) != 2 || entries[0].Path != "link" || entries[0].Type != "symlink" || entries[1].Path != "notes.txt" {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	first, err := client.ReadFile(context.Background(), input, "notes.txt", 0, 2, "")
	if err != nil || string(first.Content) != "he" || first.TotalBytes != 5 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := client.ReadFile(context.Background(), input, "notes.txt", 2, 3, first.FileVersion)
	if err != nil || string(second.Content) != "llo" || second.FileVersion != first.FileVersion {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if _, err := client.ReadFile(context.Background(), input, "link/secret", 0, 1, ""); !errors.Is(err, ErrConflict) {
		t.Fatal("followed symlink", err)
	}
	if _, err := client.ReadFile(context.Background(), input, "../secret", 0, 1, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted traversal", err)
	}
	written, err := client.WriteFile(context.Background(), input, "notes.txt", []byte("updated"))
	if err != nil || written.SizeBytes != 7 || string(content) != "updated" {
		t.Fatalf("written=%+v content=%q err=%v", written, content, err)
	}
	if err := client.DeleteFile(context.Background(), input, "notes.txt"); err != nil || !deleted {
		t.Fatal("delete failed", err)
	}
}

// Invoked by the real Docker candidate harness; no credentials or user content are logged.
func TestLiveDiscovery(t *testing.T) {
	endpoint := os.Getenv("CA_BASE_ENDPOINT")
	if endpoint == "" {
		t.Skip("requires owned candidate harness")
	}
	id := Identity{Tenant: "A" + strings.Repeat("~", 126) + "Z", Project: "project-poc", Workspace: os.Getenv("CA_BASE_VOLUME"), Sandbox: os.Getenv("CA_BASE_RUN"), Operation: os.Getenv("CA_BASE_OPERATION"), Generation: 1, SpecDigest: "sha256:" + strings.Repeat("a", 64)}
	client, err := New(endpoint, os.Getenv("CA_BASE_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	observation, err := client.Find(context.Background(), id)
	if os.Getenv("CA_BASE_DUPLICATE") == "yes" {
		if !errors.Is(err, ErrConflict) {
			t.Fatal("duplicate not rejected", err)
		}
		return
	}
	if err != nil || observation.RuntimeID != os.Getenv("CA_BASE_RUNTIME_ID") || observation.RuntimeState != os.Getenv("CA_BASE_STATE") {
		t.Fatal("receipt mismatch", observation, err)
	}
	stale := id
	stale.Generation++
	if err := client.Delete(context.Background(), stale, observation.RuntimeID); !errors.Is(err, ErrConflict) {
		t.Fatal("stale cleanup accepted", err)
	}
	if os.Getenv("CA_BASE_DELETE") == "yes" {
		for i := 0; i < 2; i++ {
			if err := client.Delete(context.Background(), id, observation.RuntimeID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := client.Find(context.Background(), id); !errors.Is(err, ErrNotFound) {
			t.Fatal("cleanup discovery", err)
		}
	}
}
