package cloudagents

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	platformadapterv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/platformadapter/v1alpha1"
	platformadapterv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/platformadapter/v1alpha1/platformadapterv1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

const fixtureDeniedMessage = "fixture denied: no production side effect"

type fixtureServices struct {
	mu    sync.Mutex
	calls []string
	deny  bool
}

func (s *fixtureServices) record(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.calls = append(s.calls, name)
	deny := s.deny
	s.mu.Unlock()
	if deny {
		return connect.NewError(connect.CodePermissionDenied, errors.New(fixtureDeniedMessage))
	}
	return nil
}

func (s *fixtureServices) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *fixtureServices) Negotiate(ctx context.Context, _ *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	if err := s.record(ctx, "WorkerExecutionService/Negotiate"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&workerv1alpha1.NegotiationResponse{}), nil
}

func (s *fixtureServices) CheckHealth(ctx context.Context, _ *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
	if err := s.record(ctx, "WorkerExecutionService/CheckHealth"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&workerv1alpha1.HealthResponse{}), nil
}

func (s *fixtureServices) ExecuteOperation(ctx context.Context, _ *connect.Request[workerv1alpha1.OperationAttemptEnvelope]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := s.record(ctx, "WorkerExecutionService/ExecuteOperation"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&workerv1alpha1.DurableReceipt{}), nil
}

func (s *fixtureServices) GetOperationReceipt(ctx context.Context, _ *connect.Request[workerv1alpha1.ReceiptRequest]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := s.record(ctx, "WorkerExecutionService/GetOperationReceipt"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&workerv1alpha1.DurableReceipt{}), nil
}

func (s *fixtureServices) registryNegotiate(ctx context.Context) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	if err := s.record(ctx, "PlatformAdapterRegistryService/Negotiate"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&workerv1alpha1.NegotiationResponse{}), nil
}

func (s *fixtureServices) registerAdapter(ctx context.Context) (*connect.Response[platformadapterv1alpha1.AdapterRegistrationReceipt], error) {
	if err := s.record(ctx, "PlatformAdapterRegistryService/RegisterAdapter"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&platformadapterv1alpha1.AdapterRegistrationReceipt{}), nil
}

func (s *fixtureServices) getAdapterRegistrationReceipt(ctx context.Context) (*connect.Response[platformadapterv1alpha1.AdapterRegistrationReceipt], error) {
	if err := s.record(ctx, "PlatformAdapterRegistryService/GetAdapterRegistrationReceipt"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&platformadapterv1alpha1.AdapterRegistrationReceipt{}), nil
}

func (s *fixtureServices) executionNegotiate(ctx context.Context) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	if err := s.record(ctx, "PlatformAdapterExecutionService/Negotiate"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&workerv1alpha1.NegotiationResponse{}), nil
}

func (s *fixtureServices) getCapabilities(ctx context.Context) (*connect.Response[platformadapterv1alpha1.AdapterCapabilitiesResponse], error) {
	if err := s.record(ctx, "PlatformAdapterExecutionService/GetCapabilities"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&platformadapterv1alpha1.AdapterCapabilitiesResponse{}), nil
}

func (s *fixtureServices) adapterCheckHealth(ctx context.Context) (*connect.Response[platformadapterv1alpha1.AdapterHealthResponse], error) {
	if err := s.record(ctx, "PlatformAdapterExecutionService/CheckHealth"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&platformadapterv1alpha1.AdapterHealthResponse{}), nil
}

func (s *fixtureServices) adapterExecuteOperation(ctx context.Context) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := s.record(ctx, "PlatformAdapterExecutionService/ExecuteOperation"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&workerv1alpha1.DurableReceipt{}), nil
}

func (s *fixtureServices) adapterGetOperationReceipt(ctx context.Context) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := s.record(ctx, "PlatformAdapterExecutionService/GetOperationReceipt"); err != nil {
		return nil, err
	}
	return connect.NewResponse(&workerv1alpha1.DurableReceipt{}), nil
}

type registryHandlerAdapter struct{ svc *fixtureServices }

func (a registryHandlerAdapter) Negotiate(ctx context.Context, _ *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	return a.svc.registryNegotiate(ctx)
}
func (a registryHandlerAdapter) RegisterAdapter(ctx context.Context, _ *connect.Request[platformadapterv1alpha1.AdapterRegistrationRequest]) (*connect.Response[platformadapterv1alpha1.AdapterRegistrationReceipt], error) {
	return a.svc.registerAdapter(ctx)
}
func (a registryHandlerAdapter) GetAdapterRegistrationReceipt(ctx context.Context, _ *connect.Request[platformadapterv1alpha1.AdapterRegistrationReceiptRequest]) (*connect.Response[platformadapterv1alpha1.AdapterRegistrationReceipt], error) {
	return a.svc.getAdapterRegistrationReceipt(ctx)
}

type executionHandlerAdapter struct{ svc *fixtureServices }

func (a executionHandlerAdapter) Negotiate(ctx context.Context, _ *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	return a.svc.executionNegotiate(ctx)
}
func (a executionHandlerAdapter) GetCapabilities(ctx context.Context, _ *connect.Request[platformadapterv1alpha1.AdapterCapabilitiesRequest]) (*connect.Response[platformadapterv1alpha1.AdapterCapabilitiesResponse], error) {
	return a.svc.getCapabilities(ctx)
}
func (a executionHandlerAdapter) CheckHealth(ctx context.Context, _ *connect.Request[platformadapterv1alpha1.AdapterHealthRequest]) (*connect.Response[platformadapterv1alpha1.AdapterHealthResponse], error) {
	return a.svc.adapterCheckHealth(ctx)
}
func (a executionHandlerAdapter) ExecuteOperation(ctx context.Context, _ *connect.Request[platformadapterv1alpha1.AdapterOperationRequest]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	return a.svc.adapterExecuteOperation(ctx)
}
func (a executionHandlerAdapter) GetOperationReceipt(ctx context.Context, _ *connect.Request[platformadapterv1alpha1.AdapterReceiptRequest]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	return a.svc.adapterGetOperationReceipt(ctx)
}

func fixtureServer(t *testing.T, svc *fixtureServices) *httptest.Server {
	return fixtureServerWithClientAuth(t, svc, tls.NoClientCert)
}

func fixtureServerWithClientAuth(t *testing.T, svc *fixtureServices, clientAuth tls.ClientAuthType) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	workerPath, workerHandler := workerv1alpha1connect.NewWorkerExecutionServiceHandler(svc)
	registryPath, registryHandler := platformadapterv1alpha1connect.NewPlatformAdapterRegistryServiceHandler(registryHandlerAdapter{svc})
	executionPath, executionHandler := platformadapterv1alpha1connect.NewPlatformAdapterExecutionServiceHandler(executionHandlerAdapter{svc})
	mux.Handle(workerPath, workerHandler)
	mux.Handle(registryPath, registryHandler)
	mux.Handle(executionPath, executionHandler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{ClientAuth: clientAuth, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func TestGeneratedSDKConnectAndGRPCConformance(t *testing.T) {
	svc := new(fixtureServices)
	server := fixtureServer(t, svc)
	client := server.Client()
	worker := workerv1alpha1connect.NewWorkerExecutionServiceClient(client, server.URL)
	registry := platformadapterv1alpha1connect.NewPlatformAdapterRegistryServiceClient(client, server.URL)
	execution := platformadapterv1alpha1connect.NewPlatformAdapterExecutionServiceClient(client, server.URL)
	ctx := context.Background()
	connectCalls := []func() error{
		func() error {
			_, err := worker.Negotiate(ctx, connect.NewRequest(&workerv1alpha1.NegotiationRequest{}))
			return err
		},
		func() error {
			_, err := worker.CheckHealth(ctx, connect.NewRequest(&workerv1alpha1.HealthRequest{}))
			return err
		},
		func() error {
			_, err := worker.ExecuteOperation(ctx, connect.NewRequest(&workerv1alpha1.OperationAttemptEnvelope{}))
			return err
		},
		func() error {
			_, err := worker.GetOperationReceipt(ctx, connect.NewRequest(&workerv1alpha1.ReceiptRequest{}))
			return err
		},
		func() error {
			_, err := registry.Negotiate(ctx, connect.NewRequest(&workerv1alpha1.NegotiationRequest{}))
			return err
		},
		func() error {
			_, err := registry.RegisterAdapter(ctx, connect.NewRequest(&platformadapterv1alpha1.AdapterRegistrationRequest{}))
			return err
		},
		func() error {
			_, err := registry.GetAdapterRegistrationReceipt(ctx, connect.NewRequest(&platformadapterv1alpha1.AdapterRegistrationReceiptRequest{}))
			return err
		},
		func() error {
			_, err := execution.Negotiate(ctx, connect.NewRequest(&workerv1alpha1.NegotiationRequest{}))
			return err
		},
		func() error {
			_, err := execution.GetCapabilities(ctx, connect.NewRequest(&platformadapterv1alpha1.AdapterCapabilitiesRequest{}))
			return err
		},
		func() error {
			_, err := execution.CheckHealth(ctx, connect.NewRequest(&platformadapterv1alpha1.AdapterHealthRequest{}))
			return err
		},
		func() error {
			_, err := execution.ExecuteOperation(ctx, connect.NewRequest(&platformadapterv1alpha1.AdapterOperationRequest{}))
			return err
		},
		func() error {
			_, err := execution.GetOperationReceipt(ctx, connect.NewRequest(&platformadapterv1alpha1.AdapterReceiptRequest{}))
			return err
		},
	}
	for index, call := range connectCalls {
		if err := call(); err != nil {
			t.Fatalf("connect method %d: %v", index, err)
		}
	}

	tlsConfig := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	grpcConn, err := grpc.NewClient(strings.TrimPrefix(server.URL, "https://"), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = grpcConn.Close() })
	grpcCalls := []struct {
		procedure string
		input     proto.Message
		output    proto.Message
	}{
		{workerv1alpha1connect.WorkerExecutionServiceNegotiateProcedure, &workerv1alpha1.NegotiationRequest{}, &workerv1alpha1.NegotiationResponse{}},
		{workerv1alpha1connect.WorkerExecutionServiceCheckHealthProcedure, &workerv1alpha1.HealthRequest{}, &workerv1alpha1.HealthResponse{}},
		{workerv1alpha1connect.WorkerExecutionServiceExecuteOperationProcedure, &workerv1alpha1.OperationAttemptEnvelope{}, &workerv1alpha1.DurableReceipt{}},
		{workerv1alpha1connect.WorkerExecutionServiceGetOperationReceiptProcedure, &workerv1alpha1.ReceiptRequest{}, &workerv1alpha1.DurableReceipt{}},
		{platformadapterv1alpha1connect.PlatformAdapterRegistryServiceNegotiateProcedure, &workerv1alpha1.NegotiationRequest{}, &workerv1alpha1.NegotiationResponse{}},
		{platformadapterv1alpha1connect.PlatformAdapterRegistryServiceRegisterAdapterProcedure, &platformadapterv1alpha1.AdapterRegistrationRequest{}, &platformadapterv1alpha1.AdapterRegistrationReceipt{}},
		{platformadapterv1alpha1connect.PlatformAdapterRegistryServiceGetAdapterRegistrationReceiptProcedure, &platformadapterv1alpha1.AdapterRegistrationReceiptRequest{}, &platformadapterv1alpha1.AdapterRegistrationReceipt{}},
		{platformadapterv1alpha1connect.PlatformAdapterExecutionServiceNegotiateProcedure, &workerv1alpha1.NegotiationRequest{}, &workerv1alpha1.NegotiationResponse{}},
		{platformadapterv1alpha1connect.PlatformAdapterExecutionServiceGetCapabilitiesProcedure, &platformadapterv1alpha1.AdapterCapabilitiesRequest{}, &platformadapterv1alpha1.AdapterCapabilitiesResponse{}},
		{platformadapterv1alpha1connect.PlatformAdapterExecutionServiceCheckHealthProcedure, &platformadapterv1alpha1.AdapterHealthRequest{}, &platformadapterv1alpha1.AdapterHealthResponse{}},
		{platformadapterv1alpha1connect.PlatformAdapterExecutionServiceExecuteOperationProcedure, &platformadapterv1alpha1.AdapterOperationRequest{}, &workerv1alpha1.DurableReceipt{}},
		{platformadapterv1alpha1connect.PlatformAdapterExecutionServiceGetOperationReceiptProcedure, &platformadapterv1alpha1.AdapterReceiptRequest{}, &workerv1alpha1.DurableReceipt{}},
	}
	for _, call := range grpcCalls {
		if err := grpcConn.Invoke(ctx, call.procedure, call.input, call.output); err != nil {
			t.Fatalf("grpc %s: %v", call.procedure, err)
		}
	}
	if got := svc.callCount(); got != len(connectCalls)+len(grpcCalls) {
		t.Fatalf("fixture call count = %d, want %d", got, len(connectCalls)+len(grpcCalls))
	}
}

func TestGeneratedSDKContextErrorAndStableError(t *testing.T) {
	svc := new(fixtureServices)
	server := fixtureServer(t, svc)
	client := workerv1alpha1connect.NewWorkerExecutionServiceClient(server.Client(), server.URL)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Negotiate(canceled, connect.NewRequest(&workerv1alpha1.NegotiationRequest{})); connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("canceled code = %v, err = %v", connect.CodeOf(err), err)
	}
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if _, err := client.CheckHealth(deadline, connect.NewRequest(&workerv1alpha1.HealthRequest{})); connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatalf("deadline code = %v, err = %v", connect.CodeOf(err), err)
	}
	svc.mu.Lock()
	svc.deny = true
	svc.mu.Unlock()
	_, err := client.GetOperationReceipt(context.Background(), connect.NewRequest(&workerv1alpha1.ReceiptRequest{}))
	if connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), fixtureDeniedMessage) {
		t.Fatalf("stable error = %v / %v", connect.CodeOf(err), err)
	}
}

func TestGeneratedSDKmTLSRejectsMissingClientCertificate(t *testing.T) {
	svc := new(fixtureServices)
	server := fixtureServerWithClientAuth(t, svc, tls.RequireAnyClientCert)
	clientTLS := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	conn, err := grpc.NewClient(strings.TrimPrefix(server.URL, "https://"), grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer func() { _ = conn.Close() }()
	err = conn.Invoke(context.Background(), workerv1alpha1connect.WorkerExecutionServiceNegotiateProcedure, &workerv1alpha1.NegotiationRequest{}, &workerv1alpha1.NegotiationResponse{})
	if err == nil {
		t.Fatal("mTLS request without client certificate unexpectedly succeeded")
	}
	if got := svc.callCount(); got != 0 {
		t.Fatalf("mTLS rejected request reached fixture service %d time(s)", got)
	}
}

func TestGeneratedSDKPreservesProtoUnknownFields(t *testing.T) {
	encoded := []byte{0xd8, 0x07, 0x01} // field 123, wire type 0, value 1
	request := new(workerv1alpha1.NegotiationRequest)
	if err := proto.Unmarshal(encoded, request); err != nil {
		t.Fatalf("proto.Unmarshal unknown field: %v", err)
	}
	unknown := request.ProtoReflect().GetUnknown()
	if !bytes.Equal(unknown, encoded) {
		t.Fatalf("unknown bytes = %x, want %x", unknown, encoded)
	}
	if got := proto.Size(request); got != len(encoded) {
		t.Fatalf("proto.Size = %d, want %d", got, len(encoded))
	}
	roundTrip, err := proto.Marshal(request)
	if err != nil {
		t.Fatalf("proto.Marshal unknown field: %v", err)
	}
	if !bytes.Equal(roundTrip, encoded) {
		t.Fatalf("round-trip bytes = %x, want %x", roundTrip, encoded)
	}
}
