// Package opensandbox implements the fixed v0.2.2 lifecycle discovery/cleanup seam.
// It does not own CP authorization, durable claims, Workspace fencing or creation retries.
package opensandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid     = errors.New("opensandbox request is invalid")
	ErrUnavailable = errors.New("opensandbox authority is unavailable")
	ErrNotFound    = errors.New("opensandbox resource is absent")
	ErrConflict    = errors.New("opensandbox ownership or receipt conflicts")
	identifier     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	labelValue     = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9_.-]{0,61}[A-Za-z0-9])?$`)
	digest         = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type Identity struct {
	Tenant, Project, Workspace, Sandbox, Operation string
	Generation                                     int64
	SpecDigest                                     string
}

func (id Identity) valid() bool {
	for _, value := range []string{id.Tenant, id.Project, id.Workspace, id.Sandbox, id.Operation} {
		if !labelValue.MatchString(value) {
			return false
		}
	}
	return id.Generation > 0 && digest.MatchString(id.SpecDigest)
}

func (id Identity) Labels() map[string]string {
	if !id.valid() {
		return nil
	}
	// Candidate metadata uses Kubernetes label-value rules (63 chars, no colon).
	// Preserve all SHA-256 bits as two hex halves; never truncate the digest.
	hex := strings.TrimPrefix(id.SpecDigest, "sha256:")
	return map[string]string{
		"cloud-agents-tenant": id.Tenant, "cloud-agents-project": id.Project,
		"cloud-agents-workspace": id.Workspace, "cloud-agents-sandbox": id.Sandbox,
		"cloud-agents-operation": id.Operation, "cloud-agents-generation": strconv.FormatInt(id.Generation, 10),
		"cloud-agents-spec-sha256-a": hex[:32],
		"cloud-agents-spec-sha256-b": hex[32:],
	}
}

// Observation contains only execution metadata. Running is not a readiness verdict.
type Observation struct{ RuntimeID, RuntimeState string }

type Client struct {
	endpoint, key string
	http          *http.Client
}

func New(endpoint, key string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return nil, ErrInvalid
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, ErrInvalid
	}
	if len(key) == 0 || len(key) > 4096 {
		return nil, ErrInvalid
	}
	for _, character := range key {
		if character < 33 || character > 126 {
			return nil, ErrInvalid
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Client{endpoint: strings.TrimSuffix(endpoint, "/"), key: key, http: &http.Client{
		Transport: transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

type sandbox struct {
	ID       string            `json:"id"`
	Metadata map[string]string `json:"metadata"`
	Status   struct {
		State string `json:"state"`
	} `json:"status"`
}

func (s sandbox) observation(id Identity) (Observation, error) {
	if !identifier.MatchString(s.ID) {
		return Observation{}, ErrUnavailable
	}
	for key, value := range id.Labels() {
		if s.Metadata[key] != value {
			return Observation{}, ErrConflict
		}
	}
	switch s.Status.State {
	case "Pending", "Running", "Pausing", "Paused", "Resuming", "Stopping", "Terminated", "Failed":
		return Observation{RuntimeID: s.ID, RuntimeState: s.Status.State}, nil
	default:
		return Observation{}, ErrUnavailable
	}
}

func (c *Client) call(ctx context.Context, method, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, nil)
	if err != nil {
		return ErrInvalid
	}
	req.Header.Set("OPEN-SANDBOX-API-KEY", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if result == nil && resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode != http.StatusOK || result == nil {
		return ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, result) != nil {
		return ErrUnavailable
	}
	return nil
}

// Find discovers an exact receipt after a lost response. A caller must hold its durable
// CP claim before deciding to create. Discovery cannot make a subsequent POST atomic.
func (c *Client) Find(ctx context.Context, id Identity) (Observation, error) {
	if !id.valid() {
		return Observation{}, ErrInvalid
	}
	var found *Observation
	filter := url.Values{"cloud-agents-sandbox": {id.Sandbox}}
	// ponytail: bounded sequential scan; fail closed above 100 pages, never infer absence.
	for page := 1; page <= 100; page++ {
		query := url.Values{"metadata": {filter.Encode()}, "page": {strconv.Itoa(page)}, "pageSize": {"100"}}
		var response struct {
			Items      []sandbox `json:"items"`
			Pagination *struct {
				Page        int   `json:"page"`
				HasNextPage *bool `json:"hasNextPage"`
			} `json:"pagination"`
		}
		if err := c.call(ctx, "GET", "/v1/sandboxes?"+query.Encode(), &response); err != nil {
			return Observation{}, err
		}
		if response.Items == nil || response.Pagination == nil || response.Pagination.Page != page || response.Pagination.HasNextPage == nil || len(response.Items) > 100 {
			return Observation{}, ErrUnavailable
		}
		for _, item := range response.Items {
			observation, err := item.observation(id)
			if err != nil {
				return Observation{}, err
			}
			if found != nil {
				return Observation{}, ErrConflict
			}
			found = &observation
		}
		if !*response.Pagination.HasNextPage {
			if found == nil {
				return Observation{}, ErrNotFound
			}
			return *found, nil
		}
	}
	return Observation{}, ErrUnavailable
}

// Delete terminates the exact receipt. The caller must hold the durable claim and
// have created it with retain-volume policy: upstream termination honors that policy.
// This is not cross-process compare-and-delete and does not change volume policy.
func (c *Client) Delete(ctx context.Context, id Identity, runtimeID string) error {
	if !id.valid() || !identifier.MatchString(runtimeID) {
		return ErrInvalid
	}
	path := "/v1/sandboxes/" + runtimeID
	var current sandbox
	if err := c.call(ctx, "GET", path, &current); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if current.ID != runtimeID {
		return ErrConflict
	}
	if _, err := current.observation(id); err != nil {
		return err
	}
	if err := c.call(ctx, "DELETE", path, nil); !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}
