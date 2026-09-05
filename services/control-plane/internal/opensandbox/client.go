// Package opensandbox implements the fixed v0.2.2 lifecycle discovery/cleanup seam.
// It does not own CP authorization, durable claims, Workspace fencing or creation retries.
package opensandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	ErrInvalid       = errors.New("opensandbox request is invalid")
	ErrUnavailable   = errors.New("opensandbox authority is unavailable")
	ErrNotFound      = errors.New("opensandbox resource is absent")
	ErrConflict      = errors.New("opensandbox ownership or receipt conflicts")
	ErrRuntimeFailed = errors.New("opensandbox runtime failed")
	identifier       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	platformID       = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$`)
	digest           = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	runtimeImage     = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$`)
)

type Identity struct {
	Tenant, Project, Workspace, Sandbox, Operation string
	Generation                                     int64
	SpecDigest                                     string
}

func (id Identity) valid() bool {
	for _, value := range []string{id.Tenant, id.Project, id.Workspace, id.Sandbox, id.Operation} {
		if !platformID.MatchString(value) {
			return false
		}
	}
	return id.Generation > 0 && digest.MatchString(id.SpecDigest)
}

func (id Identity) Labels() map[string]string {
	if !id.valid() {
		return nil
	}
	// Candidate labels cannot carry all valid public identifiers (128 chars/~).
	// Version the encoding and preserve every SHA-256 bit, without normalizing IDs.
	labels := map[string]string{
		"cloud-agents-receipt-version": "2",
		"cloud-agents-generation":      strconv.FormatInt(id.Generation, 10),
	}
	for name, value := range map[string]string{"tenant": id.Tenant, "project": id.Project, "workspace": id.Workspace, "sandbox": id.Sandbox, "operation": id.Operation} {
		sum := sha256.Sum256([]byte(value))
		encoded := hex.EncodeToString(sum[:])
		labels["cloud-agents-"+name+"-sha256-a"] = encoded[:32]
		labels["cloud-agents-"+name+"-sha256-b"] = encoded[32:]
	}
	encoded := strings.TrimPrefix(id.SpecDigest, "sha256:")
	labels["cloud-agents-spec-sha256-a"] = encoded[:32]
	labels["cloud-agents-spec-sha256-b"] = encoded[32:]
	return labels
}

// Observation contains only execution metadata. Running is not a readiness verdict.
type Observation struct{ RuntimeID, RuntimeState string }

type CreateInput struct {
	Identity    Identity
	ImageURI    string
	VolumeName  string
	CPUMillis   int64
	MemoryBytes int64
}

func (input CreateInput) valid() bool {
	return input.Identity.valid() && runtimeImage.MatchString(input.ImageURI) &&
		identifier.MatchString(input.VolumeName) && len(input.VolumeName) <= 63 &&
		input.CPUMillis >= 100 && input.CPUMillis <= 64000 &&
		input.MemoryBytes >= 134217728 && input.MemoryBytes <= 1099511627776
}

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
	labels := id.Labels()
	filter := url.Values{
		"cloud-agents-sandbox-sha256-a": {labels["cloud-agents-sandbox-sha256-a"]},
		"cloud-agents-sandbox-sha256-b": {labels["cloud-agents-sandbox-sha256-b"]},
	}
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

// Create adopts an exact receipt before creating. Cross-process exclusion is the
// caller's durable claim; a lost POST response is recovered by the next Find.
func (c *Client) Create(ctx context.Context, input CreateInput) (Observation, error) {
	if c == nil || !input.valid() {
		return Observation{}, ErrInvalid
	}
	if found, err := c.Find(ctx, input.Identity); err == nil {
		return found, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Observation{}, err
	}
	body := map[string]any{
		"image":      map[string]string{"uri": input.ImageURI},
		"entrypoint": []string{"sleep", "infinity"},
		"resourceLimits": map[string]string{
			"cpu": strconv.FormatInt(input.CPUMillis, 10) + "m", "memory": strconv.FormatInt(input.MemoryBytes, 10),
		},
		"metadata": input.Identity.Labels(),
		"volumes": []map[string]any{{
			"name": "workspace", "pvc": map[string]any{
				"claimName": input.VolumeName, "createIfNotExists": false, "deleteOnSandboxTermination": false,
			}, "mountPath": "/workspace", "readOnly": false,
		}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return Observation{}, ErrInvalid
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/sandboxes", bytes.NewReader(encoded))
	if err != nil {
		return Observation{}, ErrInvalid
	}
	request.Header.Set("OPEN-SANDBOX-API-KEY", c.key)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Observation{}, ctx.Err()
		}
		return Observation{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusConflict {
			return Observation{}, ErrConflict
		}
		return Observation{}, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	var created sandbox
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, &created) != nil {
		return Observation{}, ErrUnavailable
	}
	return created.observation(input.Identity)
}

// WaitReady verifies the candidate state and its command service; Running alone
// is not readiness because an invalid entrypoint can fail asynchronously.
func (c *Client) WaitReady(ctx context.Context, id Identity, runtimeID string) (Observation, error) {
	if c == nil || !id.valid() || !identifier.MatchString(runtimeID) {
		return Observation{}, ErrInvalid
	}
	last := Observation{RuntimeID: runtimeID}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var current sandbox
		if err := c.call(ctx, http.MethodGet, "/v1/sandboxes/"+runtimeID, &current); err != nil {
			return last, err
		}
		observation, err := current.observation(id)
		if err != nil {
			return last, err
		}
		last = observation
		switch observation.RuntimeState {
		case "Running":
			if err := c.probeExecd(ctx, runtimeID); err == nil {
				return observation, nil
			} else if !errors.Is(err, ErrUnavailable) {
				return observation, err
			}
		case "Failed", "Terminated":
			return observation, ErrRuntimeFailed
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *Client) probeExecd(ctx context.Context, runtimeID string) error {
	var endpoint struct {
		Endpoint string            `json:"endpoint"`
		Headers  map[string]string `json:"headers"`
	}
	if err := c.call(ctx, http.MethodGet, "/v1/sandboxes/"+runtimeID+"/endpoints/44772", &endpoint); err != nil {
		return err
	}
	base, baseErr := url.Parse(c.endpoint)
	raw := endpoint.Endpoint
	if !strings.Contains(raw, "://") {
		raw = base.Scheme + "://" + raw
	}
	target, err := url.Parse(raw)
	if err != nil || baseErr != nil || target.Host == "" || target.Hostname() != base.Hostname() || target.Scheme != base.Scheme || target.User != nil ||
		(target.Path != "" && target.Path != "/" && target.Path != "/proxy/44772") || target.RawPath != "" || target.RawQuery != "" || target.Fragment != "" || target.Opaque != "" ||
		(target.Scheme != "http" && target.Scheme != "https") || len(endpoint.Headers) > 16 {
		return ErrUnavailable
	}
	target.Path = "/ping"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return ErrUnavailable
	}
	for name, value := range endpoint.Headers {
		if name == "" || len(name) > 128 || len(value) > 4096 || strings.EqualFold(name, "Host") ||
			strings.ContainsAny(name+value, "\r\n") {
			return ErrUnavailable
		}
		request.Header.Set(name, value)
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	defer response.Body.Close()
	copied, copyErr := io.Copy(io.Discard, io.LimitReader(response.Body, (1<<20)+1))
	if copyErr != nil || copied > 1<<20 || response.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	return nil
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
