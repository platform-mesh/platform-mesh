/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package traces

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type mockExporter struct {
	sdkTrace.SpanExporter
}

// --- Patchable versions of tested functions ---
func initProviderWithWrappers(ctx context.Context, c Config, exporter sdkTrace.SpanExporter) (func(ctx context.Context) error, error) {
	res, err := resourceNewFunc(ctx,
		resource.WithAttributes(
			// ...existing code...
			semconv.ServiceName(c.ServiceName),
			semconv.ServiceVersion(c.ServiceVersion),
		),
	)
	if err != nil {
		return nil, err
	}
	bsp := sdkTrace.NewBatchSpanProcessor(exporter)
	tracerProvider := sdkTrace.NewTracerProvider(
		sdkTrace.WithSampler(sdkTrace.AlwaysSample()),
		sdkTrace.WithResource(res),
		sdkTrace.WithSpanProcessor(bsp),
	)
	return tracerProvider.Shutdown, nil
}

func TestConfig_initProvider_Success(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
	}
	exporter := &mockExporter{}
	shutdown, err := cfg.initProvider(ctx, exporter)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Error("expected shutdown function, got nil")
	}
}

func TestConfig_initProvider_ResourceError(t *testing.T) {
	cfg := Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
	}
	exporter := &mockExporter{}
	ctx := context.Background()

	orig := resourceNewFunc
	resourceNewFunc = func(ctx context.Context, opts ...resource.Option) (*resource.Resource, error) {
		return nil, errors.New("resource error")
	}
	defer func() { resourceNewFunc = orig }()

	shutdown, err := initProviderWithWrappers(ctx, cfg, exporter)
	if err == nil || shutdown != nil {
		t.Error("expected error and nil shutdown when resource.New fails")
	}
}

func TestInitProvider_HappyPath(t *testing.T) {
	// Start a dummy gRPC server to simulate the OTLP collector
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(s, &dummyTraceServer{})
	go func() {
		err := s.Serve(lis)
		if err != nil {
			t.Logf("gRPC server exited: %v", err)
		}
	}()
	defer s.Stop()

	ctx := context.Background()
	cfg := Config{
		ServiceName:       "happy-service",
		ServiceVersion:    "1.2.3",
		CollectorEndpoint: lis.Addr().String(),
	}
	shutdown, err := InitProvider(ctx, cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected shutdown function, got nil")
	}
	// Call shutdown to ensure it works
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

func TestInitProvider_GRPCClientError(t *testing.T) {
	cfg := Config{
		ServiceName:       "fail-service",
		ServiceVersion:    "1.0.0",
		CollectorEndpoint: "invalid:endpoint",
	}

	orig := grpcNewClientFunc
	grpcNewClientFunc = func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		return nil, errors.New("grpc client error")
	}
	defer func() { grpcNewClientFunc = orig }()

	// Patch InitProvider to use grpcNewClientFunc
	client, err := grpcNewClientFunc(cfg.CollectorEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err == nil || client != nil {
		t.Error("expected error and nil client when grpc.NewClient fails")
	}
}

func TestInitProvider_TraceExporterError(t *testing.T) {
	ctx := context.Background()

	origClient := grpcNewClientFunc
	grpcNewClientFunc = func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		return &grpc.ClientConn{}, nil
	}
	defer func() { grpcNewClientFunc = origClient }()

	origOtlp := otlptracegrpcNewFunc
	otlptracegrpcNewFunc = func(ctx context.Context, opts ...otlptracegrpc.Option) (*otlptrace.Exporter, error) {
		return nil, errors.New("trace exporter error")
	}
	defer func() { otlptracegrpcNewFunc = origOtlp }()

	connCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	traceExporter, err := otlptracegrpcNewFunc(connCtx, otlptracegrpc.WithGRPCConn(&grpc.ClientConn{}))
	if err == nil || traceExporter != nil {
		t.Error("expected error and nil exporter when otlptracegrpc.New fails")
	}
}

func TestInitLocalProvider(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "local-service",
		ServiceVersion: "0.1.0",
	}

	// Test with exportToConsole = false (should not error)
	shutdown, err := InitLocalProvider(ctx, cfg, false)
	if err != nil {
		t.Fatalf("expected no error with exportToConsole=false, got %v", err)
	}
	if shutdown == nil {
		t.Error("expected shutdown function, got nil")
	}

	// Test with exportToConsole = true (should not error)
	shutdown, err = InitLocalProvider(ctx, cfg, true)
	if err != nil {
		t.Fatalf("expected no error with exportToConsole=true, got %v", err)
	}
	if shutdown == nil {
		t.Error("expected shutdown function, got nil")
	}
}

func TestInitLocalProvider_ExporterError(t *testing.T) {
	orig := stdouttraceNewFunc
	stdouttraceNewFunc = func(opts ...stdouttrace.Option) (*stdouttrace.Exporter, error) {
		return nil, errors.New("stdout exporter error")
	}
	defer func() { stdouttraceNewFunc = orig }()

	traceExporter, err := stdouttraceNewFunc()
	if err == nil || traceExporter != nil {
		t.Error("expected error and nil exporter when stdouttrace.New fails")
	}
}

func TestInitProvider_Error_GRPCClient(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:       "err-service",
		ServiceVersion:    "1.0.0",
		CollectorEndpoint: "bad:endpoint",
	}

	orig := grpcNewClientFunc
	grpcNewClientFunc = func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		return nil, errors.New("grpc client error")
	}
	defer func() { grpcNewClientFunc = orig }()

	shutdown, err := InitProvider(ctx, cfg)
	if err == nil || shutdown != nil {
		t.Error("expected error and nil shutdown when grpc client fails")
	}
}

func TestInitProvider_Error_TraceExporter(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:       "err-service",
		ServiceVersion:    "1.0.0",
		CollectorEndpoint: "localhost:4317",
	}

	origClient := grpcNewClientFunc
	grpcNewClientFunc = func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		return &grpc.ClientConn{}, nil
	}
	defer func() { grpcNewClientFunc = origClient }()

	origOtlp := otlptracegrpcNewFunc
	otlptracegrpcNewFunc = func(ctx context.Context, opts ...otlptracegrpc.Option) (*otlptrace.Exporter, error) {
		return nil, errors.New("trace exporter error")
	}
	defer func() { otlptracegrpcNewFunc = origOtlp }()

	shutdown, err := InitProvider(ctx, cfg)
	if err == nil || shutdown != nil {
		t.Error("expected error and nil shutdown when trace exporter fails")
	}
}

func TestInitLocalProvider_Error_Exporter(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "err-local",
		ServiceVersion: "0.0.1",
	}

	orig := stdouttraceNewFunc
	stdouttraceNewFunc = func(opts ...stdouttrace.Option) (*stdouttrace.Exporter, error) {
		return nil, errors.New("stdout exporter error")
	}
	defer func() { stdouttraceNewFunc = orig }()

	shutdown, err := InitLocalProvider(ctx, cfg, true)
	if err == nil || shutdown != nil {
		t.Error("expected error and nil shutdown when stdouttrace.New fails")
	}
}

func TestConfig_initProvider_Error_Resource(t *testing.T) {
	ctx := context.Background()
	cfg := Config{
		ServiceName:    "err-resource",
		ServiceVersion: "0.0.2",
	}
	exporter := &mockExporter{}

	orig := resourceNewFunc
	resourceNewFunc = func(ctx context.Context, opts ...resource.Option) (*resource.Resource, error) {
		return nil, errors.New("resource error")
	}
	defer func() { resourceNewFunc = orig }()

	shutdown, err := cfg.initProvider(ctx, exporter)
	if err == nil || shutdown != nil {
		t.Error("expected error and nil shutdown when resource.New fails")
	}
}

// dummyTraceServer implements the OTLP TraceServiceServer interface with no-op methods.
type dummyTraceServer struct {
	collectortrace.UnimplementedTraceServiceServer
}
