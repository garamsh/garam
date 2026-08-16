package cli

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/machine"
)

// The service name every trace, metric and log line carries, so one
// stream tells the servers apart.
const (
	serviceAPI     = "garam-api"
	serviceAuthz   = "garam-authz"
	serviceMachine = "garam-machine"
)

// observability is the telemetry one server constructs at startup:
// the logger its handlers write through, and the registry its metrics
// are collected from. It sits in the composition root because api and
// authz never import each other (ADR-0003).
type observability struct {
	logger   *slog.Logger
	registry *prometheus.Registry
}

// newObservability builds the logger, the metrics registry and the
// tracer for one server. Metrics are defined by the otelhttp and
// otelgrpc instrumentation, which finds the providers set here as
// globals; the registry is the one place they are collected from.
func newObservability(service string) (*observability, error) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With(slog.String("service", service))
	// A line written through the default logger lands in the same
	// stream as the handler lines below.
	slog.SetDefault(logger)

	res := resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(service))

	registry := prometheus.NewRegistry()
	// A registry of its own inherits nothing from the default one; the
	// Go runtime series a scrape is read for come from this collector.
	registry.MustRegister(collectors.NewGoCollector())
	reader, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, errorx.Wrap(err, "prometheus exporter", errorx.KindInternal)
	}
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	))

	// No span exporter: garam has nowhere to ship spans to yet. The
	// provider still mints the trace_id every line below carries, and
	// the propagator keeps that id across the api → authz hop.
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithResource(res)))
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &observability{logger: logger, registry: registry}, nil
}

// metricsHandler serves the registry in the prometheus text format for
// a scraper to pull.
func (o *observability) metricsHandler() http.Handler {
	return promhttp.HandlerFor(o.registry, promhttp.HandlerOpts{})
}

// logRequests emits one line per served request. It runs as the
// generated router's per-operation middleware, so the request already
// carries the route pattern that names the operation — a concrete
// path can hold an invitation token and the query an OAuth code, and
// neither reaches the line, nor does any header, cookie or body.
func (o *observability) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var principal api.Principal
		r = api.WithPrincipal(r, &principal)
		o.logServed(w, r, next, func() string { return principal.GRN })
	})
}

// logMachineRequests is logRequests for the machine listener. Its
// principal is the identity the client certificate authenticated,
// which the listener resolved before the request reached here, so it
// is read rather than written back.
func (o *observability) logMachineRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.logServed(w, r, next, func() string {
			if identity, ok := machine.IdentityOf(r); ok {
				return identity.GRN().String()
			}
			return ""
		})
	})
}

// logServed runs next and emits the line, asking for the principal
// once the handler has had its chance to resolve one.
func (o *observability) logServed(w http.ResponseWriter, r *http.Request, next http.Handler, principal func() string) {
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	next.ServeHTTP(recorder, r)

	o.logger.LogAttrs(r.Context(), levelOf(recorder.status >= http.StatusInternalServerError),
		"http request",
		slog.String("operation", r.Pattern),
		slog.String("trace_id", traceIDOf(r.Context())),
		slog.Int("status", recorder.status),
		principalAttr(principal()),
	)
}

// logRPCs is the gRPC counterpart of logRequests. The engine's
// principal is the subject the request asks about.
func (o *observability) logRPCs(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)

	subject := ""
	if named, ok := req.(interface{ GetSubjectGrn() string }); ok {
		subject = named.GetSubjectGrn()
	}
	o.logger.LogAttrs(ctx, levelOf(err != nil), "rpc",
		slog.String("operation", info.FullMethod),
		slog.String("trace_id", traceIDOf(ctx)),
		slog.String("code", status.Code(err).String()),
		principalAttr(subject),
	)
	return resp, err
}

// levelOf grades a finished call: a failure is worth waking someone
// for, a success is not.
func levelOf(failed bool) slog.Level {
	if failed {
		return slog.LevelError
	}
	return slog.LevelInfo
}

// principalAttr renders a known principal as the user_id field, and
// an unknown one as no field at all: an empty user_id would read as a
// caller identified as nobody.
func principalAttr(subject string) slog.Attr {
	if subject == "" {
		return slog.Attr{}
	}
	return slog.String("user_id", subject)
}

// traceIDOf returns the call's trace id, falling back to a fresh UUID
// when nothing upstream started a span.
func traceIDOf(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return uuid.NewString()
}

// statusRecorder remembers the status an inner handler wrote, which
// net/http otherwise keeps to itself.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
