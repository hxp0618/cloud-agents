package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/accessgateway"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/opensandbox"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type config struct {
	listen, database, dockerCredentials, tlsCert, tlsKey string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cloud-agents-access-gateway: %v\n", err)
		os.Exit(2)
	}
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	set := flag.NewFlagSet("cloud-agents-access-gateway", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	value := config{}
	set.StringVar(&value.listen, "listen", "127.0.0.1:8090", "Gateway listen address")
	set.StringVar(&value.database, "database-url", "", "PostgreSQL URL")
	set.StringVar(&value.dockerCredentials, "docker-credentials-directory", "", "deployment-owned Docker mTLS credential directory")
	set.StringVar(&value.tlsCert, "tls-cert", "", "optional TLS certificate")
	set.StringVar(&value.tlsKey, "tls-key", "", "optional TLS private key")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return config{}, errors.New("invalid access Gateway configuration")
	}
	fill := func(target *string, name string) {
		if *target == "" && getenv != nil {
			*target = getenv(name)
		}
	}
	fill(&value.database, "CLOUD_AGENTS_PLATFORM_DATABASE_URL")
	fill(&value.dockerCredentials, "CLOUD_AGENTS_PLATFORM_DOCKER_CREDENTIALS_DIRECTORY")
	fill(&value.tlsCert, "CLOUD_AGENTS_ACCESS_GATEWAY_TLS_CERT")
	fill(&value.tlsKey, "CLOUD_AGENTS_ACCESS_GATEWAY_TLS_KEY")
	if value.database == "" || value.dockerCredentials == "" ||
		(value.tlsCert == "") != (value.tlsKey == "") {
		return config{}, errors.New("database, Docker credential, and complete TLS configuration are required")
	}
	for _, item := range []string{value.listen, value.database, value.dockerCredentials, value.tlsCert, value.tlsKey} {
		if strings.TrimSpace(item) != item {
			return config{}, errors.New("invalid access Gateway configuration")
		}
	}
	if value.tlsCert == "" {
		host, _, err := net.SplitHostPort(value.listen)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			return config{}, errors.New("plaintext access Gateway must listen on loopback")
		}
	}
	return value, nil
}

func run(ctx context.Context, args []string, getenv func(string) string) error {
	config, err := parseConfig(args, getenv)
	if err != nil {
		return err
	}
	poolConfig, err := pgxpool.ParseConfig(config.database)
	if err != nil {
		return errors.New("invalid PostgreSQL configuration")
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return errors.New("database pool configuration failed")
	}
	defer pool.Close()
	var safe bool
	if err := pool.QueryRow(ctx, `SELECT NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole
    AND NOT rolreplication AND NOT rolbypassrls
    AND pg_has_role(current_user, 'cloud_agents_runtime', 'MEMBER')
FROM pg_roles WHERE rolname = current_user`).Scan(&safe); err != nil || !safe {
		return errors.New("database runtime authority is unsafe")
	}
	store, err := postgres.NewAccessGatewayStore(pool)
	if err != nil {
		return errors.New("access Gateway store is unavailable")
	}
	credentials, err := opensandbox.NewCredentialDirectory(config.dockerCredentials)
	if err != nil {
		return errors.New("OpenSandbox credential directory is invalid")
	}
	handler, err := accessgateway.New(store, credentials)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/", handler)
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	httpServer := &http.Server{Addr: config.listen, Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx }}
	errorsChannel := make(chan error, 1)
	go func() {
		if config.tlsCert == "" {
			errorsChannel <- httpServer.ListenAndServe()
		} else {
			errorsChannel <- httpServer.ListenAndServeTLS(config.tlsCert, config.tlsKey)
		}
	}()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("access Gateway stopped")
	}
}
