package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

const (
	cliLoginCallbackPath     = "/callback"
	cliLoginCompletePath     = "/complete"
	cliLoopbackHeaderTimeout = 5 * time.Second
)

var (
	errCLILoginCallbackRejected = errors.New("CLI login callback rejected")
	errCLILoginDenied           = errors.New("CLI login was not approved")
)

type cliLoginCallback struct {
	code string
	err  error
}

type cliLoginLoopback struct {
	listener      net.Listener
	server        *http.Server
	callbackURL   string
	expectedHost  string
	state         string
	codeVerifier  string
	codeChallenge string
	result        chan cliLoginCallback
	serveError    chan error
	consumed      atomic.Bool
	completed     atomic.Bool
}

func newCLILoginLoopback() (*cliLoginLoopback, error) {
	return newCLILoginLoopbackWithRandom(rand.Reader)
}

func newCLILoginLoopbackWithRandom(random io.Reader) (*cliLoginLoopback, error) {
	if random == nil {
		return nil, errCLILoginCallbackRejected
	}
	stateBytes := make([]byte, 32)
	verifierBytes := make([]byte, 32)
	if _, err := io.ReadFull(random, stateBytes); err != nil {
		return nil, errCLILoginCallbackRejected
	}
	if _, err := io.ReadFull(random, verifierBytes); err != nil {
		return nil, errCLILoginCallbackRejected
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errCLILoginCallbackRejected
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	challengeDigest := sha256.Sum256([]byte(verifier))
	loopback := &cliLoginLoopback{
		listener:      listener,
		expectedHost:  listener.Addr().String(),
		state:         state,
		codeVerifier:  verifier,
		codeChallenge: base64.RawURLEncoding.EncodeToString(challengeDigest[:]),
		result:        make(chan cliLoginCallback, 1),
		serveError:    make(chan error, 1),
	}
	loopback.callbackURL = (&url.URL{Scheme: "http", Host: loopback.expectedHost, Path: cliLoginCallbackPath}).String()
	loopback.server = &http.Server{Handler: http.HandlerFunc(loopback.handleCallback), ReadHeaderTimeout: cliLoopbackHeaderTimeout}
	go func() {
		err := loopback.server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			loopback.serveError <- errCLILoginCallbackRejected
		}
	}()
	return loopback, nil
}

func (loopback *cliLoginLoopback) Await(ctx context.Context) (string, error) {
	if loopback == nil || ctx == nil {
		return "", errCLILoginCallbackRejected
	}
	select {
	case result := <-loopback.result:
		return result.code, result.err
	case <-loopback.serveError:
		return "", errCLILoginCallbackRejected
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (loopback *cliLoginLoopback) Close() error {
	if loopback == nil || loopback.server == nil {
		return nil
	}
	return loopback.server.Close()
}

func (loopback *cliLoginLoopback) handleCallback(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	if request.Method == http.MethodGet && request.Host == loopback.expectedHost && request.URL.Path == cliLoginCompletePath && request.URL.RawPath == "" && request.URL.RawQuery == "" {
		if !loopback.completed.Load() {
			http.Error(writer, "CLI login callback rejected", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(writer, "CLI login completed. You can close this window.")
		return
	}
	if request.Method != http.MethodGet || request.Host != loopback.expectedHost || request.URL.Path != cliLoginCallbackPath || request.URL.RawPath != "" {
		http.Error(writer, "CLI login callback rejected", http.StatusBadRequest)
		return
	}
	values, ok := exactCLILoginCallbackQuery(request.URL.Query())
	if !ok || subtle.ConstantTimeCompare([]byte(values.Get("state")), []byte(loopback.state)) != 1 {
		http.Error(writer, "CLI login callback rejected", http.StatusBadRequest)
		return
	}
	if !loopback.consumed.CompareAndSwap(false, true) {
		http.Error(writer, "CLI login callback already used", http.StatusGone)
		return
	}
	loopback.completed.Store(true)
	writer.Header().Set("Location", cliLoginCompletePath)
	writer.WriteHeader(http.StatusSeeOther)
	if code := values.Get("code"); code != "" {
		loopback.result <- cliLoginCallback{code: code}
	} else {
		loopback.result <- cliLoginCallback{err: errCLILoginDenied}
	}
}

func exactCLILoginCallbackQuery(values url.Values) (url.Values, bool) {
	if len(values) != 2 || len(values["state"]) != 1 || !isRawURLToken(values.Get("state"), 32) {
		return nil, false
	}
	if code := values["code"]; len(code) == 1 && isRawURLToken(code[0], 32) {
		if _, hasError := values["error"]; !hasError {
			return values, true
		}
	}
	if denied := values["error"]; len(denied) == 1 && denied[0] == "access_denied" {
		if _, hasCode := values["code"]; !hasCode {
			return values, true
		}
	}
	return nil, false
}

func isRawURLToken(value string, bytes int) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == bytes && base64.RawURLEncoding.EncodeToString(decoded) == value
}
