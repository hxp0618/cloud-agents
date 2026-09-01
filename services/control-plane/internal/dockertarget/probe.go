package dockertarget

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	maxSocketPathBytes = 4096
	maxResponseBytes   = 64 << 10
)

var (
	ErrInvalidSocket   = errors.New("docker target socket is invalid")
	ErrUnavailable     = errors.New("docker target is unavailable")
	ErrInvalidResponse = errors.New("docker target returned an invalid response")
	apiVersionPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
)

type ProbeResult struct {
	APIVersion    string `json:"apiVersion"`
	EngineVersion string `json:"engineVersion"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
}

func ProbeUnixSocket(ctx context.Context, socketPath string) (ProbeResult, error) {
	if ctx == nil || !validSocketPath(socketPath) {
		return ProbeResult{}, ErrInvalidSocket
	}
	transport := &http.Transport{
		DisableCompression:    true,
		ResponseHeaderTimeout: 10 * time.Second,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrInvalidResponse
		},
	}
	if body, err := get(ctx, client, "http://docker/_ping", 16); err != nil {
		return ProbeResult{}, err
	} else if string(body) != "OK" {
		return ProbeResult{}, ErrInvalidResponse
	}
	body, err := get(ctx, client, "http://docker/version", maxResponseBytes)
	if err != nil {
		return ProbeResult{}, err
	}
	var response struct {
		APIVersion    string `json:"ApiVersion"`
		EngineVersion string `json:"Version"`
		OS            string `json:"Os"`
		Architecture  string `json:"Arch"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ProbeResult{}, ErrInvalidResponse
	}
	result := ProbeResult(response)
	if !apiVersionPattern.MatchString(result.APIVersion) || !validFact(result.EngineVersion) || !validFact(result.OS) || !validFact(result.Architecture) {
		return ProbeResult{}, ErrInvalidResponse
	}
	return result, nil
}

func validSocketPath(value string) bool {
	return value != "" && len(value) <= maxSocketPathBytes && !strings.ContainsRune(value, 0) &&
		filepath.IsAbs(value) && filepath.Clean(value) == value && value != string(filepath.Separator)
}

func validFact(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func get(ctx context.Context, client *http.Client, target string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, ErrInvalidResponse
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, ErrInvalidResponse
	}
	if int64(len(body)) > limit {
		return nil, ErrInvalidResponse
	}
	return body, nil
}
