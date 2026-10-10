package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCLILoginLoopbackBindsExactCallbackAndConsumesOnce(t *testing.T) {
	loopback, err := newCLILoginLoopback()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loopback.Close() })
	if !strings.HasPrefix(loopback.callbackURL, "http://127.0.0.1:") || !strings.HasSuffix(loopback.callbackURL, cliLoginCallbackPath) {
		t.Fatalf("callback URL = %q", loopback.callbackURL)
	}
	if len(loopback.state) != 43 || len(loopback.codeVerifier) != 43 {
		t.Fatalf("state/verifier lengths = %d/%d", len(loopback.state), len(loopback.codeVerifier))
	}
	digest := sha256.Sum256([]byte(loopback.codeVerifier))
	if want := base64.RawURLEncoding.EncodeToString(digest[:]); loopback.codeChallenge != want {
		t.Fatalf("challenge = %q, want %q", loopback.codeChallenge, want)
	}

	client := loopbackHTTPClient()
	noRedirectClient := loopbackHTTPClient()
	noRedirectClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	codeA := base64.RawURLEncoding.EncodeToString(bytesOf('a', 32))
	codeB := base64.RawURLEncoding.EncodeToString(bytesOf('b', 32))
	codeC := base64.RawURLEncoding.EncodeToString(bytesOf('c', 32))
	codeD := base64.RawURLEncoding.EncodeToString(bytesOf('d', 32))
	wrongHost, err := http.NewRequest(http.MethodGet, loopback.callbackURL+"?code="+codeA+"&state="+loopback.state, nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongHost.Host = "localhost:1"
	assertLoopbackStatus(t, client, wrongHost, http.StatusBadRequest)
	wrongState, err := http.NewRequest(http.MethodGet, loopback.callbackURL+"?code="+codeA+"&state="+codeB, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertLoopbackStatus(t, client, wrongState, http.StatusBadRequest)

	code := codeC
	valid, err := http.NewRequest(http.MethodGet, loopback.callbackURL+"?code="+code+"&state="+loopback.state, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := assertLoopbackStatus(t, noRedirectClient, valid, http.StatusSeeOther)
	for name, want := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer"} {
		if got := response.Header.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if location := response.Header.Get("Location"); location != cliLoginCompletePath || strings.Contains(location, "code") || strings.Contains(location, "state") {
		t.Fatalf("completion location = %q", location)
	}
	complete, err := http.NewRequest(http.MethodGet, "http://"+loopback.expectedHost+cliLoginCompletePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	completion := assertLoopbackStatus(t, client, complete, http.StatusOK)
	for name, want := range map[string]string{
		"Cache-Control": "no-store", "Referrer-Policy": "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	} {
		if got := completion.Header.Get(name); got != want {
			t.Fatalf("completion %s = %q, want %q", name, got, want)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got, err := loopback.Await(ctx); err != nil || got != code {
		t.Fatalf("Await() = %q, %v", got, err)
	}
	replay, err := http.NewRequest(http.MethodGet, loopback.callbackURL+"?code="+codeD+"&state="+loopback.state, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertLoopbackStatus(t, client, replay, http.StatusGone)
}

func TestCLILoginLoopbackRejectsUnexpectedQueryWithoutConsuming(t *testing.T) {
	loopback, err := newCLILoginLoopback()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loopback.Close() })
	client := loopbackHTTPClient()
	complete, err := http.NewRequest(http.MethodGet, "http://"+loopback.expectedHost+cliLoginCompletePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertLoopbackStatus(t, client, complete, http.StatusBadRequest)
	codeA := base64.RawURLEncoding.EncodeToString(bytesOf('a', 32))
	codeB := base64.RawURLEncoding.EncodeToString(bytesOf('b', 32))
	for _, query := range []string{
		"code=" + codeA + "&state=" + loopback.state + "&extra=x",
		"code=short&state=" + loopback.state,
		"code=" + codeA + "&code=" + codeB + "&state=" + loopback.state,
		"error=server_error&state=" + loopback.state,
	} {
		request, requestErr := http.NewRequest(http.MethodGet, loopback.callbackURL+"?"+query, nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		assertLoopbackStatus(t, client, request, http.StatusBadRequest)
	}
	denied, err := http.NewRequest(http.MethodGet, loopback.callbackURL+"?error=access_denied&state="+loopback.state, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertLoopbackStatus(t, client, denied, http.StatusOK)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if code, err := loopback.Await(ctx); code != "" || !errors.Is(err, errCLILoginDenied) {
		t.Fatalf("Await() = %q, %v", code, err)
	}
}

func TestCLILoginLoopbackAwaitHonorsCancellation(t *testing.T) {
	loopback, err := newCLILoginLoopback()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loopback.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loopback.Await(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Await() error = %v", err)
	}
}

func loopbackHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second}
}

func assertLoopbackStatus(t *testing.T, client *http.Client, request *http.Request, want int) *http.Response {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		t.Fatalf("status = %d, want %d; body=%q", response.StatusCode, want, body)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return response
}

func bytesOf(value byte, count int) []byte {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return result
}
