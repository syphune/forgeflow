package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Config struct {
	Endpoint    string
	ServiceName string
}

func Setup(ctx context.Context, config Config) (func(context.Context) error, error) {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("OTLP endpoint must be an absolute URL")
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(parsed.String()))
	if err != nil {
		return nil, err
	}
	serviceName := strings.TrimSpace(config.ServiceName)
	if serviceName == "" {
		serviceName = "forgeflow-api"
	}
	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		exporter.Shutdown(ctx)
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return provider.Shutdown, nil
}

func HTTP(next http.Handler) http.Handler {
	return otelhttp.NewMiddleware("forgeflow.http", otelhttp.WithPublicEndpoint())(next)
}
