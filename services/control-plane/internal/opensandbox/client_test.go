package opensandbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
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
