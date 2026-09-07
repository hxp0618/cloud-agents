// Package opensandbox implements the fixed v0.2.2 lifecycle discovery/cleanup seam.
// It does not own CP authorization, durable claims, Workspace fencing or creation retries.
package opensandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/networkpolicy"
)

var (
	ErrInvalid          = errors.New("opensandbox request is invalid")
	ErrUnavailable      = errors.New("opensandbox authority is unavailable")
	ErrNotFound         = errors.New("opensandbox resource is absent")
	ErrConflict         = errors.New("opensandbox ownership or receipt conflicts")
	ErrRuntimeFailed    = errors.New("opensandbox runtime failed")
	ErrOutputLimit      = errors.New("opensandbox command output limit exceeded")
	ErrFileLimit        = errors.New("opensandbox file limit exceeded")
	ErrPolicyUnenforced = errors.New("opensandbox network policy was not enforced")
	identifier          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	platformID          = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$`)
	digest              = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	runtimeImage        = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$`)
	previewHeaderName   = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9a-z-]+$")
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

const maxExecOutputBytes = 1 << 20

type ExecInput struct {
	Identity  Identity
	RuntimeID string
	Command   string
	Timeout   time.Duration
}

type ExecResult struct {
	Stdout, Stderr      string
	ExitCode            int64
	ExecutionTimeMillis int64
}

type PTYInput struct {
	Identity  Identity
	RuntimeID string
	Command   string
}

type PTYObservation struct {
	SessionID    string
	Running      bool
	OutputOffset int64
}

type FileEntry struct {
	Path, Type, ModifiedAt, FileVersion string
	SizeBytes                           int64
}

type FileRead struct {
	Path, FileVersion  string
	Offset, TotalBytes int64
	Content            []byte
}

func (input PTYInput) valid() bool {
	return input.Identity.valid() && identifier.MatchString(input.RuntimeID) &&
		(input.Command == "" || len(input.Command) <= 8192 && utf8.ValidString(input.Command) && !strings.ContainsRune(input.Command, 0))
}

func (input ExecInput) valid() bool {
	return input.Identity.valid() && identifier.MatchString(input.RuntimeID) && len(input.Command) >= 1 &&
		len(input.Command) <= 8192 && utf8.ValidString(input.Command) && !strings.ContainsRune(input.Command, 0) &&
		input.Timeout >= time.Second && input.Timeout <= time.Minute
}

type CreateInput struct {
	Identity      Identity
	ImageURI      string
	VolumeName    string
	CPUMillis     int64
	MemoryBytes   int64
	NetworkPolicy *NetworkPolicy
}

type NetworkPolicy struct {
	DefaultAction string        `json:"defaultAction"`
	Egress        []NetworkRule `json:"egress"`
}

type NetworkRule struct {
	Action string `json:"action"`
	Target string `json:"target"`
}

func (policy *NetworkPolicy) valid() bool {
	if policy == nil {
		return true
	}
	targets := make([]string, len(policy.Egress))
	for index, rule := range policy.Egress {
		if rule.Action != "allow" {
			return false
		}
		targets[index] = rule.Target
	}
	canonical, err := networkpolicy.CanonicalAllowedEgress(targets)
	return policy.DefaultAction == "deny" && err == nil && slices.Equal(canonical, targets)
}

func (input CreateInput) valid() bool {
	return input.Identity.valid() && runtimeImage.MatchString(input.ImageURI) &&
		identifier.MatchString(input.VolumeName) && len(input.VolumeName) <= 63 &&
		input.CPUMillis >= 100 && input.CPUMillis <= 64000 &&
		input.MemoryBytes >= 134217728 && input.MemoryBytes <= 1099511627776 && input.NetworkPolicy.valid()
}

type Client struct {
	endpoint, key string
	http          *http.Client
}

type PreviewHeader struct{ Name, Value string }

type PreviewResponse struct {
	StatusCode int
	Headers    []PreviewHeader
	Body       []byte
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
	if input.NetworkPolicy != nil {
		body["networkPolicy"] = input.NetworkPolicy
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
	createHTTP := *c.http
	createHTTP.Timeout = 45 * time.Second // candidate sidecar readiness is bounded at 30 seconds
	response, err := createHTTP.Do(request)
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
	target, headers, err := c.execdEndpoint(ctx, runtimeID)
	if err != nil {
		return err
	}
	target.Path = "/ping"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return ErrUnavailable
	}
	request.Header = headers
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

func (c *Client) execdEndpoint(ctx context.Context, runtimeID string) (*url.URL, http.Header, error) {
	var endpoint struct {
		Endpoint string            `json:"endpoint"`
		Headers  map[string]string `json:"headers"`
	}
	if err := c.call(ctx, http.MethodGet, "/v1/sandboxes/"+runtimeID+"/endpoints/44772", &endpoint); err != nil {
		return nil, nil, err
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
		return nil, nil, ErrUnavailable
	}
	headers := make(http.Header, len(endpoint.Headers))
	for name, value := range endpoint.Headers {
		if name == "" || len(name) > 128 || len(value) > 4096 || strings.EqualFold(name, "Host") ||
			strings.ContainsAny(name+value, "\r\n") {
			return nil, nil, ErrUnavailable
		}
		headers.Set(name, value)
	}
	return target, headers, nil
}

func (c *Client) verifyAccessTarget(ctx context.Context, input PTYInput) (*url.URL, http.Header, error) {
	if c == nil || ctx == nil || !input.valid() {
		return nil, nil, ErrInvalid
	}
	if err := c.verifyRuntime(ctx, input); err != nil {
		return nil, nil, err
	}
	return c.execdEndpoint(ctx, input.RuntimeID)
}

func (c *Client) verifyRuntime(ctx context.Context, input PTYInput) error {
	var current sandbox
	if err := c.call(ctx, http.MethodGet, "/v1/sandboxes/"+input.RuntimeID, &current); err != nil {
		return err
	}
	observation, err := current.observation(input.Identity)
	if err != nil {
		return err
	}
	if observation.RuntimeID != input.RuntimeID || observation.RuntimeState != "Running" {
		return ErrRuntimeFailed
	}
	return nil
}

// PreviewHTTPProxyTarget returns only the candidate's fixed server-proxy route for
// the verified Sandbox receipt. The caller remains responsible for Grant lifetime.
func (c *Client) PreviewHTTPProxyTarget(ctx context.Context, input PTYInput, port int32) (*url.URL, http.Header, error) {
	if c == nil || ctx == nil || !input.valid() || port < 1024 || port > 65535 || port == 44772 {
		return nil, nil, ErrInvalid
	}
	if err := c.verifyRuntime(ctx, input); err != nil {
		return nil, nil, err
	}
	return c.serverProxyTarget(ctx, input.RuntimeID, port)
}

func CanonicalPreviewHeaders(source http.Header) ([]PreviewHeader, error) {
	blocked := map[string]bool{"authorization": true, "connection": true, "content-length": true, "cookie": true, "forwarded": true, "host": true, "keep-alive": true, "open-sandbox-api-key": true, "proxy-authenticate": true, "proxy-authorization": true, "proxy-connection": true, "set-cookie": true, "te": true, "trailer": true, "transfer-encoding": true, "upgrade": true, "x-forwarded-for": true, "x-forwarded-host": true, "x-forwarded-proto": true, "x-real-ip": true}
	for _, value := range source.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			blocked[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}
	values := make([]PreviewHeader, 0, len(source))
	for name, entries := range source {
		name = strings.ToLower(name)
		if blocked[name] || len(name) < 1 || len(name) > 128 || !previewHeaderName.MatchString(name) {
			continue
		}
		for _, value := range entries {
			if len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
				return nil, ErrInvalid
			}
			values = append(values, PreviewHeader{Name: name, Value: value})
		}
	}
	if len(values) > 64 {
		return nil, ErrOutputLimit
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Name != values[j].Name {
			return values[i].Name < values[j].Name
		}
		return values[i].Value < values[j].Value
	})
	unique := values[:0]
	for _, value := range values {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique, nil
}

func (c *Client) ProxyPreview(ctx context.Context, input PTYInput, port int32, method, suffix, rawQuery string, headers []PreviewHeader, body []byte) (PreviewResponse, error) {
	if c == nil || ctx == nil || len(body) > 1<<20 || !strings.HasPrefix(suffix, "/") || strings.ContainsAny(suffix, "?#\x00\r\n") || strings.ContainsAny(rawQuery, "#\x00\r\n") || !slices.Contains([]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}, method) {
		return PreviewResponse{}, ErrInvalid
	}
	target, privateHeaders, err := c.PreviewHTTPProxyTarget(ctx, input, port)
	if err != nil {
		return PreviewResponse{}, err
	}
	if suffix != "/" {
		target.Path = strings.TrimSuffix(target.Path, "/") + suffix
	}
	target.RawPath, target.RawQuery = "", rawQuery
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return PreviewResponse{}, ErrInvalid
	}
	publicHeaders := make(http.Header)
	for _, header := range headers {
		publicHeaders.Add(header.Name, header.Value)
	}
	checkedHeaders, err := CanonicalPreviewHeaders(publicHeaders)
	if err != nil {
		return PreviewResponse{}, err
	}
	for _, header := range checkedHeaders {
		request.Header.Add(header.Name, header.Value)
	}
	for name, values := range privateHeaders {
		request.Header.Del(name)
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	request.Host = target.Host
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return PreviewResponse{}, ctx.Err()
		}
		return PreviewResponse{}, ErrUnavailable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil {
		return PreviewResponse{}, ErrUnavailable
	}
	if len(data) > 4<<20 {
		return PreviewResponse{}, ErrOutputLimit
	}
	responseHeaders, err := CanonicalPreviewHeaders(response.Header)
	if err != nil {
		return PreviewResponse{}, err
	}
	return PreviewResponse{StatusCode: response.StatusCode, Headers: responseHeaders, Body: data}, nil
}

func (c *Client) serverProxyTarget(ctx context.Context, runtimeID string, port int32) (*url.URL, http.Header, error) {
	var endpoint struct {
		Endpoint string            `json:"endpoint"`
		Headers  map[string]string `json:"headers"`
	}
	portValue := strconv.FormatInt(int64(port), 10)
	if err := c.call(ctx, http.MethodGet, "/v1/sandboxes/"+runtimeID+"/endpoints/"+portValue+"?use_server_proxy=true", &endpoint); err != nil {
		return nil, nil, err
	}
	base, baseErr := url.Parse(c.endpoint)
	raw := endpoint.Endpoint
	if !strings.Contains(raw, "://") {
		raw = base.Scheme + "://" + raw
	}
	advertised, err := url.Parse(raw)
	expectedPath := "/v1/sandboxes/" + runtimeID + "/proxy/" + portValue
	if err != nil || baseErr != nil || advertised.Scheme != base.Scheme || advertised.Hostname() != base.Hostname() ||
		(advertised.Port() != "" && advertised.Port() != base.Port()) ||
		(advertised.Path != expectedPath && advertised.Path != strings.TrimPrefix(expectedPath, "/v1")) ||
		advertised.User != nil || advertised.RawPath != "" || advertised.RawQuery != "" || advertised.Fragment != "" || advertised.Opaque != "" || len(endpoint.Headers) > 16 {
		return nil, nil, ErrUnavailable
	}
	target := *base
	target.Path = expectedPath
	headers := make(http.Header, len(endpoint.Headers)+1)
	for name, value := range endpoint.Headers {
		if name == "" || len(name) > 128 || len(value) > 4096 || strings.EqualFold(name, "Host") ||
			strings.EqualFold(name, "OPEN-SANDBOX-API-KEY") || strings.ContainsAny(name+value, "\r\n") {
			return nil, nil, ErrUnavailable
		}
		headers.Set(name, value)
	}
	headers.Set("OPEN-SANDBOX-API-KEY", c.key)
	return &target, headers, nil
}

func (c *Client) VerifyNetworkPolicy(ctx context.Context, id Identity, runtimeID string, expected *NetworkPolicy) error {
	if c == nil || ctx == nil || !id.valid() || !identifier.MatchString(runtimeID) || expected == nil || !expected.valid() {
		return ErrInvalid
	}
	if err := c.verifyRuntime(ctx, PTYInput{Identity: id, RuntimeID: runtimeID}); err != nil {
		return err
	}
	target, headers, err := c.serverProxyTarget(ctx, runtimeID, 18080)
	if err != nil {
		return err
	}
	target.Path += "/policy"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return ErrUnavailable
	}
	request.Header = headers
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	defer response.Body.Close()
	var status struct {
		Status          string         `json:"status"`
		EnforcementMode string         `json:"enforcementMode"`
		Policy          *NetworkPolicy `json:"policy"`
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if readErr != nil || len(data) > 64<<10 || response.StatusCode != http.StatusOK ||
		json.Unmarshal(data, &status) != nil || status.Status != "ok" || status.EnforcementMode != "dns+nft" ||
		status.Policy == nil || status.Policy.DefaultAction != expected.DefaultAction || len(status.Policy.Egress) != len(expected.Egress) {
		return ErrPolicyUnenforced
	}
	for index := range expected.Egress {
		if status.Policy.Egress[index] != expected.Egress[index] {
			return ErrPolicyUnenforced
		}
	}
	return nil
}

func execdPath(target *url.URL, path string) {
	target.Path = strings.TrimSuffix(target.Path, "/") + path
}

// CreatePTY creates only a fixed /workspace shell session after re-verifying the
// physical Sandbox receipt. Authorization and grant lifetime remain Gateway concerns.
func (c *Client) CreatePTY(ctx context.Context, input PTYInput) (PTYObservation, error) {
	target, headers, err := c.verifyAccessTarget(ctx, input)
	if err != nil {
		return PTYObservation{}, err
	}
	body, err := json.Marshal(struct {
		CWD     string `json:"cwd"`
		Command string `json:"command,omitempty"`
	}{CWD: "/workspace", Command: input.Command})
	if err != nil {
		return PTYObservation{}, ErrInvalid
	}
	execdPath(target, "/pty")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return PTYObservation{}, ErrUnavailable
	}
	request.Header = headers.Clone()
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return PTYObservation{}, ctx.Err()
		}
		return PTYObservation{}, ErrUnavailable
	}
	defer response.Body.Close()
	var result PTYObservation
	var raw struct {
		SessionID string `json:"session_id"`
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if readErr != nil || len(data) > 64<<10 || response.StatusCode != http.StatusCreated ||
		json.Unmarshal(data, &raw) != nil || !identifier.MatchString(raw.SessionID) {
		return PTYObservation{}, ErrUnavailable
	}
	result.SessionID = raw.SessionID
	return result, nil
}

func (c *Client) GetPTY(ctx context.Context, input PTYInput, sessionID string) (PTYObservation, error) {
	target, headers, err := c.verifyAccessTarget(ctx, input)
	if err != nil || !identifier.MatchString(sessionID) {
		if err != nil {
			return PTYObservation{}, err
		}
		return PTYObservation{}, ErrInvalid
	}
	execdPath(target, "/pty/"+sessionID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return PTYObservation{}, ErrUnavailable
	}
	request.Header = headers.Clone()
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return PTYObservation{}, ctx.Err()
		}
		return PTYObservation{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return PTYObservation{}, ErrNotFound
	}
	var raw struct {
		SessionID    string `json:"session_id"`
		Running      bool   `json:"running"`
		OutputOffset int64  `json:"output_offset"`
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if readErr != nil || len(data) > 64<<10 || response.StatusCode != http.StatusOK ||
		json.Unmarshal(data, &raw) != nil || raw.SessionID != sessionID || raw.OutputOffset < 0 {
		return PTYObservation{}, ErrUnavailable
	}
	return PTYObservation{SessionID: raw.SessionID, Running: raw.Running, OutputOffset: raw.OutputOffset}, nil
}

func (c *Client) DeletePTY(ctx context.Context, input PTYInput, sessionID string) error {
	target, headers, err := c.verifyAccessTarget(ctx, input)
	if err != nil || !identifier.MatchString(sessionID) {
		if err != nil {
			return err
		}
		return ErrInvalid
	}
	execdPath(target, "/pty/"+sessionID)
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, target.String(), http.NoBody)
	if err != nil {
		return ErrUnavailable
	}
	request.Header = headers.Clone()
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		return ErrUnavailable
	}
	return nil
}

func (c *Client) PTYWebSocketTarget(ctx context.Context, input PTYInput, sessionID string) (*url.URL, http.Header, error) {
	target, headers, err := c.verifyAccessTarget(ctx, input)
	if err != nil {
		return nil, nil, err
	}
	if !identifier.MatchString(sessionID) {
		return nil, nil, ErrInvalid
	}
	execdPath(target, "/pty/"+sessionID+"/ws")
	return target, headers, nil
}

type candidateFileInfo struct {
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

func validFilePath(value string, allowRoot bool) bool {
	if len(value) < 1 || len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsAny(value, "\\\x00\r\n") || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	if value == "." {
		return allowRoot
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func workspaceFilePath(value string) string {
	if value == "." {
		return "/workspace"
	}
	return "/workspace/" + value
}

func cloneExecdTarget(target *url.URL, suffix string) *url.URL {
	cloned := *target
	execdPath(&cloned, suffix)
	return &cloned
}

func (c *Client) fileInfo(ctx context.Context, target *url.URL, headers http.Header, absolutePath string) (candidateFileInfo, error) {
	requestTarget := cloneExecdTarget(target, "/files/info")
	query := requestTarget.Query()
	query.Add("path", absolutePath)
	requestTarget.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestTarget.String(), http.NoBody)
	if err != nil {
		return candidateFileInfo{}, ErrUnavailable
	}
	request.Header = headers.Clone()
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return candidateFileInfo{}, ctx.Err()
		}
		return candidateFileInfo{}, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return candidateFileInfo{}, ErrNotFound
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	var values map[string]candidateFileInfo
	if readErr != nil || len(data) > 64<<10 || response.StatusCode != http.StatusOK || json.Unmarshal(data, &values) != nil || len(values) != 1 {
		return candidateFileInfo{}, ErrUnavailable
	}
	info, ok := values[absolutePath]
	if !ok || info.Path != absolutePath || info.Size < 0 || info.ModifiedAt.IsZero() {
		return candidateFileInfo{}, ErrUnavailable
	}
	return info, nil
}

func fileVersion(info candidateFileInfo) string {
	sum := sha256.Sum256([]byte(info.Type + "\x00" + strconv.FormatInt(info.Size, 10) + "\x00" + info.ModifiedAt.UTC().Format(time.RFC3339Nano)))
	return "sfv1_" + base64.RawURLEncoding.EncodeToString(sum[:])
}

func fileEntry(info candidateFileInfo, relativePath string) (FileEntry, error) {
	if !validFilePath(relativePath, false) || info.Path != workspaceFilePath(relativePath) || info.Size < 0 {
		return FileEntry{}, ErrUnavailable
	}
	switch info.Type {
	case "file", "directory", "symlink", "other":
	default:
		return FileEntry{}, ErrUnavailable
	}
	return FileEntry{Path: relativePath, Type: info.Type, SizeBytes: info.Size, ModifiedAt: info.ModifiedAt.UTC().Format(time.RFC3339Nano), FileVersion: fileVersion(info)}, nil
}

func (c *Client) checkedFileInfo(ctx context.Context, target *url.URL, headers http.Header, relativePath, wantedType string, allowMissing bool) (candidateFileInfo, bool, error) {
	current := "/workspace"
	root, err := c.fileInfo(ctx, target, headers, current)
	if err != nil || root.Type != "directory" {
		if err != nil {
			return candidateFileInfo{}, false, err
		}
		return candidateFileInfo{}, false, ErrConflict
	}
	if relativePath == "." {
		if wantedType != "" && root.Type != wantedType {
			return candidateFileInfo{}, false, ErrConflict
		}
		return root, true, nil
	}
	segments := strings.Split(relativePath, "/")
	for index, segment := range segments {
		current += "/" + segment
		info, infoErr := c.fileInfo(ctx, target, headers, current)
		if errors.Is(infoErr, ErrNotFound) && allowMissing {
			return candidateFileInfo{}, false, nil
		}
		if infoErr != nil {
			return candidateFileInfo{}, false, infoErr
		}
		if info.Type == "symlink" || index < len(segments)-1 && info.Type != "directory" {
			return candidateFileInfo{}, false, ErrConflict
		}
		if index == len(segments)-1 {
			if wantedType != "" && info.Type != wantedType {
				return candidateFileInfo{}, false, ErrConflict
			}
			return info, true, nil
		}
	}
	return candidateFileInfo{}, false, ErrUnavailable
}

// ListFiles returns at most the immediate 1000 Workspace children and never follows symlinks.
func (c *Client) ListFiles(ctx context.Context, input PTYInput, relativePath string) ([]FileEntry, error) {
	if !validFilePath(relativePath, true) {
		return nil, ErrInvalid
	}
	target, headers, err := c.verifyAccessTarget(ctx, input)
	if err != nil {
		return nil, err
	}
	if _, _, err := c.checkedFileInfo(ctx, target, headers, relativePath, "directory", false); err != nil {
		return nil, err
	}
	requestTarget := cloneExecdTarget(target, "/directories/list")
	query := requestTarget.Query()
	query.Set("path", workspaceFilePath(relativePath))
	query.Set("depth", "1")
	requestTarget.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestTarget.String(), http.NoBody)
	if err != nil {
		return nil, ErrUnavailable
	}
	request.Header = headers.Clone()
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	var raw []candidateFileInfo
	if readErr != nil || len(data) > 1<<20 || response.StatusCode != http.StatusOK || json.Unmarshal(data, &raw) != nil {
		return nil, ErrUnavailable
	}
	if len(raw) > 1000 {
		return nil, ErrFileLimit
	}
	entries := make([]FileEntry, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, info := range raw {
		relative := strings.TrimPrefix(info.Path, "/workspace/")
		entry, entryErr := fileEntry(info, relative)
		if entryErr != nil || path.Dir(relative) != relativePath {
			return nil, ErrUnavailable
		}
		if _, duplicate := seen[relative]; duplicate {
			return nil, ErrUnavailable
		}
		seen[relative] = struct{}{}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// ReadFile returns one byte page after checking the same metadata version before and after the read.
func (c *Client) ReadFile(ctx context.Context, input PTYInput, relativePath string, offset int64, limit int, expectedVersion string) (FileRead, error) {
	if !validFilePath(relativePath, false) || offset < 0 || offset > 16<<20 || limit < 1 || limit > 1<<20 || offset > 0 && expectedVersion == "" {
		return FileRead{}, ErrInvalid
	}
	target, headers, err := c.verifyAccessTarget(ctx, input)
	if err != nil {
		return FileRead{}, err
	}
	before, _, err := c.checkedFileInfo(ctx, target, headers, relativePath, "file", false)
	if err != nil {
		return FileRead{}, err
	}
	version := fileVersion(before)
	if expectedVersion != "" && version != expectedVersion {
		return FileRead{}, ErrConflict
	}
	if before.Size > 16<<20 {
		return FileRead{}, ErrFileLimit
	}
	if offset > before.Size {
		return FileRead{}, ErrConflict
	}
	result := FileRead{Path: relativePath, FileVersion: version, Offset: offset, TotalBytes: before.Size}
	if offset < before.Size {
		end := offset + int64(limit)
		if end > before.Size {
			end = before.Size
		}
		requestTarget := cloneExecdTarget(target, "/files/download")
		query := requestTarget.Query()
		query.Set("path", workspaceFilePath(relativePath))
		requestTarget.RawQuery = query.Encode()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, requestTarget.String(), http.NoBody)
		if requestErr != nil {
			return FileRead{}, ErrUnavailable
		}
		request.Header = headers.Clone()
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end-1))
		response, responseErr := c.http.Do(request)
		if responseErr != nil {
			if ctx.Err() != nil {
				return FileRead{}, ctx.Err()
			}
			return FileRead{}, ErrUnavailable
		}
		result.Content, requestErr = io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
		response.Body.Close()
		if requestErr != nil || len(result.Content) != int(end-offset) || response.StatusCode != http.StatusPartialContent || response.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", offset, end-1, before.Size) {
			return FileRead{}, ErrUnavailable
		}
	}
	after, _, err := c.checkedFileInfo(ctx, target, headers, relativePath, "file", false)
	if err != nil || fileVersion(after) != version {
		if err != nil {
			return FileRead{}, err
		}
		return FileRead{}, ErrConflict
	}
	return result, nil
}

func (c *Client) WriteFile(ctx context.Context, input PTYInput, relativePath string, content []byte) (FileEntry, error) {
	if !validFilePath(relativePath, false) || len(content) > 16<<20 {
		if len(content) > 16<<20 {
			return FileEntry{}, ErrFileLimit
		}
		return FileEntry{}, ErrInvalid
	}
	target, headers, err := c.verifyAccessTarget(ctx, input)
	if err != nil {
		return FileEntry{}, err
	}
	if _, _, err := c.checkedFileInfo(ctx, target, headers, relativePath, "file", true); err != nil {
		return FileEntry{}, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadataPart, err := writer.CreateFormFile("metadata", "metadata.json")
	if err == nil {
		err = json.NewEncoder(metadataPart).Encode(map[string]any{"path": workspaceFilePath(relativePath), "mode": 600})
	}
	var filePart io.Writer
	if err == nil {
		filePart, err = writer.CreateFormFile("file", path.Base(relativePath))
	}
	if err == nil {
		_, err = filePart.Write(content)
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return FileEntry{}, ErrInvalid
	}
	requestTarget := cloneExecdTarget(target, "/files/upload")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestTarget.String(), &body)
	if err != nil {
		return FileEntry{}, ErrUnavailable
	}
	request.Header = headers.Clone()
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return FileEntry{}, ctx.Err()
		}
		return FileEntry{}, ErrUnavailable
	}
	copied, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, (64<<10)+1))
	response.Body.Close()
	if readErr != nil || copied > 64<<10 || response.StatusCode != http.StatusOK {
		return FileEntry{}, ErrUnavailable
	}
	info, _, err := c.checkedFileInfo(ctx, target, headers, relativePath, "file", false)
	if err != nil || info.Size != int64(len(content)) {
		if err != nil {
			return FileEntry{}, err
		}
		return FileEntry{}, ErrConflict
	}
	return fileEntry(info, relativePath)
}

func (c *Client) DeleteFile(ctx context.Context, input PTYInput, relativePath string) error {
	if !validFilePath(relativePath, false) {
		return ErrInvalid
	}
	target, headers, err := c.verifyAccessTarget(ctx, input)
	if err != nil {
		return err
	}
	if _, _, err := c.checkedFileInfo(ctx, target, headers, relativePath, "file", false); err != nil {
		return err
	}
	requestTarget := cloneExecdTarget(target, "/files")
	query := requestTarget.Query()
	query.Add("path", workspaceFilePath(relativePath))
	requestTarget.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, requestTarget.String(), http.NoBody)
	if err != nil {
		return ErrUnavailable
	}
	request.Header = headers.Clone()
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	copied, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, (64<<10)+1))
	response.Body.Close()
	if readErr != nil || copied > 64<<10 || response.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	if _, err := c.fileInfo(ctx, target, headers, workspaceFilePath(relativePath)); !errors.Is(err, ErrNotFound) {
		if err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

// Exec runs one bounded foreground command in the fixed Workspace directory.
// CP authorization and generation selection remain the caller's responsibility;
// this adapter re-verifies the exact physical receipt before sending content.
func (c *Client) Exec(ctx context.Context, input ExecInput) (ExecResult, error) {
	if c == nil || ctx == nil || !input.valid() {
		return ExecResult{}, ErrInvalid
	}
	execCtx, cancel := context.WithTimeout(ctx, input.Timeout+5*time.Second)
	defer cancel()
	var current sandbox
	if err := c.call(execCtx, http.MethodGet, "/v1/sandboxes/"+input.RuntimeID, &current); err != nil {
		return ExecResult{}, err
	}
	observation, err := current.observation(input.Identity)
	if err != nil {
		return ExecResult{}, err
	}
	if observation.RuntimeID != input.RuntimeID || observation.RuntimeState != "Running" {
		return ExecResult{}, ErrRuntimeFailed
	}
	target, headers, err := c.execdEndpoint(execCtx, input.RuntimeID)
	if err != nil {
		return ExecResult{}, err
	}
	body, err := json.Marshal(map[string]any{"command": input.Command, "cwd": "/workspace", "background": false, "timeout": input.Timeout.Milliseconds()})
	if err != nil {
		return ExecResult{}, ErrInvalid
	}
	target.Path = "/command"
	request, err := http.NewRequestWithContext(execCtx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return ExecResult{}, ErrUnavailable
	}
	request.Header = headers.Clone()
	request.Header.Set("Content-Type", "application/json")
	httpClient := *c.http
	httpClient.Timeout = 0
	response, err := httpClient.Do(request)
	if err != nil {
		if execCtx.Err() != nil {
			return ExecResult{}, execCtx.Err()
		}
		return ExecResult{}, ErrUnavailable
	}
	defer response.Body.Close()
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if response.StatusCode != http.StatusOK || mediaType != "text/event-stream" {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, (1<<20)+1))
		return ExecResult{}, ErrUnavailable
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, (2<<20)+1))
	result := ExecResult{}
	executionID, terminal, commandFailed := "", false, false
	for !terminal {
		var event struct {
			Type          string `json:"type"`
			Text          string `json:"text"`
			ExecutionTime int64  `json:"execution_time"`
		}
		if err := decoder.Decode(&event); err != nil {
			return ExecResult{}, ErrUnavailable
		}
		switch event.Type {
		case "init":
			if executionID != "" || !identifier.MatchString(event.Text) {
				return ExecResult{}, ErrUnavailable
			}
			executionID = event.Text
		case "stdout":
			if len(result.Stdout)+len(result.Stderr)+len(event.Text) > maxExecOutputBytes {
				return ExecResult{}, ErrOutputLimit
			}
			result.Stdout += event.Text
		case "stderr":
			if len(result.Stdout)+len(result.Stderr)+len(event.Text) > maxExecOutputBytes {
				return ExecResult{}, ErrOutputLimit
			}
			result.Stderr += event.Text
		case "error":
			commandFailed, terminal = true, true
		case "execution_complete":
			if event.ExecutionTime < 0 || event.ExecutionTime > 65000 {
				return ExecResult{}, ErrUnavailable
			}
			result.ExecutionTimeMillis, terminal = event.ExecutionTime, true
		case "status", "result", "execution_count", "ping":
		default:
			return ExecResult{}, ErrUnavailable
		}
	}
	if executionID == "" {
		return ExecResult{}, ErrUnavailable
	}
	target.Path = "/command/status/" + executionID
	for {
		statusRequest, err := http.NewRequestWithContext(execCtx, http.MethodGet, target.String(), http.NoBody)
		if err != nil {
			return ExecResult{}, ErrUnavailable
		}
		statusRequest.Header = headers.Clone()
		statusResponse, err := httpClient.Do(statusRequest)
		if err != nil {
			if execCtx.Err() != nil {
				return ExecResult{}, execCtx.Err()
			}
			return ExecResult{}, ErrUnavailable
		}
		var status struct {
			ID       string `json:"id"`
			Running  bool   `json:"running"`
			ExitCode *int64 `json:"exit_code"`
		}
		data, readErr := io.ReadAll(io.LimitReader(statusResponse.Body, (64<<10)+1))
		statusResponse.Body.Close()
		if readErr != nil || len(data) > 64<<10 || statusResponse.StatusCode != http.StatusOK || json.Unmarshal(data, &status) != nil || status.ID != executionID {
			if commandFailed {
				return ExecResult{}, ErrRuntimeFailed
			}
			return ExecResult{}, ErrUnavailable
		}
		if !status.Running {
			if status.ExitCode == nil || *status.ExitCode < -2147483648 || *status.ExitCode > 2147483647 {
				return ExecResult{}, ErrUnavailable
			}
			result.ExitCode = *status.ExitCode
			return result, nil
		}
		select {
		case <-execCtx.Done():
			return ExecResult{}, execCtx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
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
