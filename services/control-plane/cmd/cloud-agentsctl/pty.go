package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
)

func attachPTY(ctx context.Context, options globalOptions, client *openapi.Client, since int64, takeover bool, stdin io.Reader, stdout io.Writer) error {
	session, err := client.GetPTYSession(ctx, options.tenant, options.project, options.grant, options.ptySession, options.requestID)
	if err != nil {
		return err
	}
	base, err := url.Parse(options.endpoint)
	if err != nil || base.Host == "" || !strings.HasPrefix(session.Value.WebSocketPath, "/") {
		return errors.New("invalid PTY WebSocket endpoint")
	}
	if base.Scheme == "https" {
		base.Scheme = "wss"
	} else {
		base.Scheme = "ws"
	}
	base.Path = session.Value.WebSocketPath
	query := url.Values{"since": {strconv.FormatInt(since, 10)}}
	if takeover {
		query.Set("takeover", "1")
	}
	base.RawQuery = query.Encode()
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	if options.caFile != "" {
		contents, err := os.ReadFile(options.caFile)
		if err != nil || len(contents) > maxCAFileBytes {
			return errors.New("cannot read CA file")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(contents) {
			return errors.New("CA file contains no certificates")
		}
		dialer.TLSClientConfig = &tls.Config{RootCAs: roots}
	}
	headers := http.Header{"Authorization": {"Bearer " + options.token}, "X-Request-ID": {options.requestID}}
	if options.tokenFile != "" {
		contents, err := os.ReadFile(options.tokenFile)
		if err != nil || len(contents) > maxBearerTokenFileBytes {
			return errors.New("cannot read bearer token file")
		}
		headers.Set("Authorization", "Bearer "+strings.TrimSuffix(strings.TrimSuffix(string(contents), "\n"), "\r"))
	}
	connection, response, err := dialer.DialContext(ctx, base.String(), headers)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return errors.New("PTY WebSocket connection failed")
	}
	defer connection.Close()
	go func() {
		<-ctx.Done()
		_ = connection.Close()
	}()
	inputErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, 32<<10)
		for {
			count, err := stdin.Read(buffer)
			if count > 0 {
				frame := append([]byte{0}, buffer[:count]...)
				if writeErr := connection.WriteMessage(websocket.BinaryMessage, frame); writeErr != nil {
					inputErr <- writeErr
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					inputErr <- err
				}
				return
			}
		}
	}()
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-inputErr:
				return err
			default:
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					return nil
				}
				return errors.New("PTY WebSocket closed unexpectedly")
			}
		}
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) < 1 {
				return errors.New("invalid PTY binary frame")
			}
			content := payload[1:]
			if payload[0] == 3 {
				if len(payload) < 9 {
					return errors.New("invalid PTY replay frame")
				}
				content = payload[9:]
			} else if payload[0] != 1 && payload[0] != 2 {
				return errors.New("invalid PTY binary frame")
			}
			if _, err := stdout.Write(content); err != nil {
				return err
			}
		case websocket.TextMessage:
			var frame struct {
				Type string `json:"type"`
				Code string `json:"code"`
			}
			if json.Unmarshal(payload, &frame) != nil {
				return errors.New("invalid PTY control frame")
			}
			if frame.Type == "exit" {
				return nil
			}
			if frame.Type == "error" {
				return errors.New("PTY error: " + frame.Code)
			}
		}
	}
}
