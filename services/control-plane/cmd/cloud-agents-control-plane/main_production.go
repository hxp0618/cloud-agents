//go:build !localdev

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/server"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	productionDatabaseEnvironment   = "CLOUD_AGENTS_PLATFORM_DATABASE_URL"
	productionAuthConfigEnvironment = "CLOUD_AGENTS_PLATFORM_AUTH_CONFIG"
	maxAuthConfigBytes              = 1 << 20
)

type productionConfig struct {
	listen   string
	database string
	authPath string
	tlsCert  string
	tlsKey   string
}

type authConfigFile struct {
	Issuer        string          `json:"issuer"`
	Audience      string          `json:"audience"`
	Generation    int64           `json:"generation"`
	SecurityEpoch int64           `json:"securityEpoch"`
	NotBefore     int64           `json:"notBefore"`
	ExpiresAt     int64           `json:"expiresAt"`
	Keys          []authConfigKey `json:"keys"`
}

type authConfigKey struct {
	JWK       json.RawMessage `json:"jwk"`
	Enabled   bool            `json:"enabled"`
	NotBefore int64           `json:"notBefore"`
	NotAfter  int64           `json:"notAfter"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runProduction(ctx, os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cloud-agents-control-plane:", err)
		os.Exit(2)
	}
}

func runProduction(ctx context.Context, args []string, getenv func(string) string) error {
	config, err := parseProductionConfig(args, getenv)
	if err != nil {
		return err
	}
	verifier, err := loadConfiguredVerifier(config.authPath)
	if err != nil {
		return err
	}
	defer verifier.Invalidate()
	pool, err := pgxpool.New(ctx, config.database)
	if err != nil {
		return errors.New("database pool configuration failed")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("database is unavailable")
	}
	coordinationService, err := postgres.NewDurableCoordinationService(pool)
	if err != nil {
		return errors.New("control-plane store is unavailable")
	}
	projectServer, err := server.NewProjectHTTPServer(verifier, coordinationService)
	if err != nil {
		return errors.New("project HTTP server is unavailable")
	}
	mux := http.NewServeMux()
	mux.Handle(server.ProjectRoutePrefix, projectServer)
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := pool.Ping(request.Context()); err != nil {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
	httpServer := &http.Server{Addr: config.listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	errorChannel := make(chan error, 1)
	go func() {
		if config.tlsCert != "" {
			errorChannel <- httpServer.ListenAndServeTLS(config.tlsCert, config.tlsKey)
			return
		}
		errorChannel <- httpServer.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownContext)
	case err := <-errorChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server stopped")
	}
}

func parseProductionConfig(args []string, getenv func(string) string) (productionConfig, error) {
	set := flag.NewFlagSet("cloud-agents-control-plane", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	listen := set.String("listen", ":8080", "listen address")
	database := set.String("database-url", "", "PostgreSQL URL")
	authPath := set.String("auth-config", "", "JSON trust configuration path")
	tlsCert := set.String("tls-cert", "", "TLS certificate path")
	tlsKey := set.String("tls-key", "", "TLS private key path")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return productionConfig{}, errors.New("invalid control-plane configuration")
	}
	if *database == "" && getenv != nil {
		*database = getenv(productionDatabaseEnvironment)
	}
	if *authPath == "" && getenv != nil {
		*authPath = getenv(productionAuthConfigEnvironment)
	}
	if strings.TrimSpace(*database) != *database || *database == "" || strings.TrimSpace(*authPath) != *authPath || *authPath == "" || *tlsCert == "" || *tlsKey == "" {
		return productionConfig{}, errors.New("database, auth config, and TLS configuration are required")
	}
	return productionConfig{listen: *listen, database: *database, authPath: *authPath, tlsCert: *tlsCert, tlsKey: *tlsKey}, nil
}

func loadConfiguredVerifier(path string) (*authn.ConfiguredVerifier, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("auth configuration cannot be opened")
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxAuthConfigBytes+1))
	if err != nil || len(contents) > maxAuthConfigBytes {
		return nil, errors.New("auth configuration is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var input authConfigFile
	if err := decoder.Decode(&input); err != nil {
		return nil, errors.New("auth configuration is invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("auth configuration has trailing data")
	}
	keys := make([]authn.ConfiguredVerifierKey, len(input.Keys))
	for index, key := range input.Keys {
		keys[index] = authn.ConfiguredVerifierKey{JWK: key.JWK, Enabled: key.Enabled, NotBefore: key.NotBefore, NotAfter: key.NotAfter}
	}
	verifier, err := authn.NewConfiguredVerifier(authn.ConfiguredVerifierConfig{Issuer: input.Issuer, Audience: input.Audience, Generation: input.Generation, SecurityEpoch: input.SecurityEpoch, NotBefore: input.NotBefore, ExpiresAt: input.ExpiresAt, Keys: keys, Clock: time.Now})
	if err != nil {
		return nil, errors.New("auth configuration is invalid")
	}
	return verifier, nil
}
