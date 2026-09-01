package sshtarget

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestCredentialDirectoryProbesPinnedSSHHost(t *testing.T) {
	_, hostPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	_, clientPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serverConfig := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if key.Type() != clientSigner.PublicKey().Type() || !bytes.Equal(key.Marshal(), clientSigner.PublicKey().Marshal()) {
			return nil, errors.New("unknown client key")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(hostSigner)
	go serveSSHProbe(listener, serverConfig)

	directory := t.TempDir()
	privatePKCS8, err := x509.MarshalPKCS8PrivateKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{
		"host-alpha.user":         []byte("agent-user\n"),
		"host-alpha.key":          pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privatePKCS8}),
		"host-alpha.host-key.pub": ssh.MarshalAuthorizedKey(hostSigner.PublicKey()),
	} {
		if err := os.WriteFile(filepath.Join(directory, name), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	result, err := credentials.Probe(context.Background(), "ssh://"+listener.Addr().String(), "host-alpha")
	if err != nil || result.APIVersion != "2.0" || result.OS != "linux" || result.Architecture != "arm64" || result.EngineVersion == "" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	_, wrongPrivate, _ := ed25519.GenerateKey(rand.Reader)
	wrongSigner, _ := ssh.NewSignerFromKey(wrongPrivate)
	if err := os.WriteFile(filepath.Join(directory, "host-alpha.host-key.pub"), ssh.MarshalAuthorizedKey(wrongSigner.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Probe(context.Background(), "ssh://"+listener.Addr().String(), "host-alpha"); !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("host key mismatch error=%v", err)
	}
	if _, err := credentials.Probe(context.Background(), "https://"+listener.Addr().String(), "host-alpha"); !errors.Is(err, ErrInvalidEndpoint) {
		t.Fatalf("HTTPS endpoint error=%v", err)
	}
}

func serveSSHProbe(listener net.Listener, config *ssh.ServerConfig) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer connection.Close()
			_, channels, requests, err := ssh.NewServerConn(connection, config)
			if err != nil {
				return
			}
			go ssh.DiscardRequests(requests)
			for channelRequest := range channels {
				if channelRequest.ChannelType() != "session" {
					_ = channelRequest.Reject(ssh.UnknownChannelType, "session required")
					continue
				}
				channel, requests, err := channelRequest.Accept()
				if err != nil {
					return
				}
				go func() {
					defer channel.Close()
					for request := range requests {
						if request.Type != "exec" || len(request.Payload) < 4 || int(binary.BigEndian.Uint32(request.Payload)) != len(request.Payload)-4 || string(request.Payload[4:]) != "uname -s && uname -m" {
							_ = request.Reply(false, nil)
							continue
						}
						_ = request.Reply(true, nil)
						_, _ = io.WriteString(channel, "Linux\naarch64\n")
						status := make([]byte, 4)
						_, _ = channel.SendRequest("exit-status", false, status)
						return
					}
				}()
			}
		}()
	}
}
