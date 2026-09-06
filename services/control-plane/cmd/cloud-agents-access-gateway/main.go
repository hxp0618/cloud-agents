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
	"golang.org/x/crypto/ssh"
)

type config struct {
	listen, database, dockerCredentials, tlsCert, tlsKey, sshListen, sshHostKey string
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
	set.StringVar(&value.sshListen, "ssh-listen", "", "optional SSH Gateway listen address")
	set.StringVar(&value.sshHostKey, "ssh-host-key", "", "SSH Gateway private host key")
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
	fill(&value.sshListen, "CLOUD_AGENTS_ACCESS_GATEWAY_SSH_LISTEN")
	fill(&value.sshHostKey, "CLOUD_AGENTS_ACCESS_GATEWAY_SSH_HOST_KEY")
	if value.database == "" || value.dockerCredentials == "" ||
		(value.tlsCert == "") != (value.tlsKey == "") || (value.sshListen == "") != (value.sshHostKey == "") {
		return config{}, errors.New("database, Docker credential, and complete TLS/SSH configuration are required")
	}
	for _, item := range []string{value.listen, value.database, value.dockerCredentials, value.tlsCert, value.tlsKey, value.sshListen, value.sshHostKey} {
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
	if value.sshListen != "" {
		if _, _, err := net.SplitHostPort(value.sshListen); err != nil {
			return config{}, errors.New("invalid SSH access Gateway listener")
		}
	}
	return value, nil
}

func loadSSHSigner(path string) (ssh.Signer, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() < 1 || info.Size() > 64<<10 {
		return nil, errors.New("SSH host key is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("SSH host key is invalid")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return nil, errors.New("SSH host key is invalid")
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, errors.New("SSH host key is invalid")
	}
	return signer, nil
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
	runContext, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	var sshListener net.Listener
	var sshSigner ssh.Signer
	if config.sshListen != "" {
		sshSigner, err = loadSSHSigner(config.sshHostKey)
		if err != nil {
			return err
		}
		sshListener, err = net.Listen("tcp", config.sshListen)
		if err != nil {
			return errors.New("SSH access Gateway listener is unavailable")
		}
		defer sshListener.Close()
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
		BaseContext: func(net.Listener) context.Context { return runContext }}
	errorsChannel := make(chan error, 2)
	go func() {
		if config.tlsCert == "" {
			errorsChannel <- httpServer.ListenAndServe()
		} else {
			errorsChannel <- httpServer.ListenAndServeTLS(config.tlsCert, config.tlsKey)
		}
	}()
	if sshListener != nil {
		go func() { errorsChannel <- handler.ServeSSH(runContext, sshListener, sshSigner) }()
	}
	select {
	case <-ctx.Done():
		cancelRun()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	case err := <-errorsChannel:
		cleanShutdown := ctx.Err() != nil || errors.Is(err, http.ErrServerClosed)
		cancelRun()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
		if cleanShutdown {
			return nil
		}
		return errors.New("access Gateway stopped")
	}
}
