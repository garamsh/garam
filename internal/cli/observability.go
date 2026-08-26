package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"

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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
)

// The service name every trace, metric and log line carries, so one
// stream tells the servers apart.
const (
	serviceAPI     = "garam-api"
	serviceAuthz   = "garam-authz"
	serviceMachine = "garam-machine"
	serviceAdapter = "garam-adapter"
)

// The field a served call names its principal with: a person under
// user_id (stack-go.md §Logging & observability), a machine identity
// under machine_id, so one stream tells an agent from a person.
const (
	fieldUserID    = "user_id"
	fieldMachineID = "machine_id"
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
		o.logServed(w, r, next, fieldUserID, func() string { return principal.GRN })
	})
}

// logMachineRequests is logRequests for the machine listener. Its
// principal is the identity the client certificate authenticated,
// which the listener resolved before the request reached here, so it
// is read rather than written back — and it is a machine, which the
// line says rather than leaving to whoever reads it.
func (o *observability) logMachineRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.logServed(w, r, next, fieldMachineID, func() string {
			if identity, ok := machine.IdentityOf(r); ok {
				return identity.GRN().String()
			}
			return ""
		})
	})
}

// logServed runs next and emits the line, asking for the principal
// once the handler has had its chance to resolve one. A 5xx's cause,
// which the surface's error writer recorded on the recorder, rides
// the same line: the client body withholds it and the rule in
// runtime-safety.md §Errors reach a handler puts it here instead.
func (o *observability) logServed(w http.ResponseWriter, r *http.Request, next http.Handler, field string, principal func() string) {
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	next.ServeHTTP(recorder, r)

	attrs := []slog.Attr{
		slog.String("operation", r.Pattern),
		slog.String("trace_id", traceIDOf(r.Context())),
		slog.Int("status", recorder.status),
		principalAttr(field, principal()),
	}
	if recorder.cause != nil {
		attrs = append(attrs, slog.String("error", recorder.cause.Error()))
	}
	o.logger.LogAttrs(r.Context(), levelOf(recorder.status >= http.StatusInternalServerError),
		"http request", attrs...)
}

// recoverPanics answers a panicking handler on behalf of the server
// rather than of a route: every net/http server a serve command builds
// wraps its outermost handler in it, so a route added to any of those
// routers is covered by having been added (stack-go.md §4, whose
// subject is the server).
//
// It logs as well as answers, which the surfaces' own middleware does
// not. That one runs where a request line is already being emitted and
// where the answer owes a specification's Error; this one runs where
// neither holds — the probes, `/metrics`, an unmatched path, the TLS
// listener's identity check — so the line it writes is the only record
// the panic leaves, and the body is plain because no specification
// describes those paths.
func (o *observability) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			o.logger.LogAttrs(r.Context(), slog.LevelError, "http panic",
				slog.String("operation", r.Pattern),
				slog.String("trace_id", traceIDOf(r.Context())),
				slog.Int("status", http.StatusInternalServerError),
				slog.String("error", fmt.Sprintf("panic: %v\n%s", v, debug.Stack())))
			http.Error(w, "internal error", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// logRPCs is the gRPC counterpart of logRequests. The engine's
// principal is the subject the request asks about. A refusal already
// carries its reason to the caller, so the line takes the cause only
// when the code is this process's fault — the 5xx counterpart of the
// HTTP surfaces (runtime-safety.md §Errors reach a handler).
func (o *observability) logRPCs(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)

	subject := ""
	if named, ok := req.(interface{ GetSubjectGrn() string }); ok {
		subject = named.GetSubjectGrn()
	}
	attrs := []slog.Attr{
		slog.String("operation", info.FullMethod),
		slog.String("trace_id", traceIDOf(ctx)),
		slog.String("code", status.Code(err).String()),
		principalAttr(principalField(subject), subject),
	}
	if err != nil && serverFault(status.Code(err)) {
		// The status message is the errorx chain the engine rendered;
		// Convert reads a plain error as its text, so either form
		// reaches the line whole.
		attrs = append(attrs, slog.String("error", status.Convert(err).Message()))
	}
	o.logger.LogAttrs(ctx, levelOf(err != nil), "rpc", attrs...)
	return resp, err
}

// serverFault reports whether a gRPC code is what the HTTP surfaces
// answer a 5xx for: the kinds that mean a fault of this process, and
// the transient ones a caller retries. Kinds are gRPC-aligned
// (ADR-0003), so the set mirrors statusOf's 5xx half by name.
func serverFault(code codes.Code) bool {
	switch code {
	case codes.Unknown, codes.Internal, codes.DataLoss,
		codes.Unimplemented, codes.Unavailable, codes.Canceled, codes.DeadlineExceeded:
		return true
	}
	return false
}

// principalField names the field a subject GRN is logged under. The
// engine matches subject strings and does not validate that one names a
// user or a group (ADR-0008), so an agent or the operator that runs it
// (ADR-0014, ADR-0015) reaches it as readily as a person does, and only
// the GRN tells them apart. A subject that is no GRN at all names no
// machine.
func principalField(subject string) string {
	g, err := grn.Parse(subject)
	if err != nil {
		return fieldUserID
	}
	switch g.ServiceType {
	case agent.ServiceType, agent.ServiceTypeOperator:
		return fieldMachineID
	}
	return fieldUserID
}

// levelOf grades a finished call: a failure is worth waking someone
// for, a success is not.
func levelOf(failed bool) slog.Level {
	if failed {
		return slog.LevelError
	}
	return slog.LevelInfo
}

// principalAttr renders a known principal under field, and an unknown
// one as no field at all: an empty one would read as a caller
// identified as nobody.
func principalAttr(field, subject string) slog.Attr {
	if subject == "" {
		return slog.Attr{}
	}
	return slog.String(field, subject)
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
// net/http otherwise keeps to itself, and the cause a surface's error
// writer reported for a 5xx.
type statusRecorder struct {
	http.ResponseWriter
	status int
	cause  error
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// RecordCause is the recording half of the seam the surfaces' error
// writers declare: a handled error that becomes a 5xx reaches the
// line logServed emits, never the client body.
func (s *statusRecorder) RecordCause(err error) {
	s.cause = err
}

// Flush forwards a flush to the writer under the recorder when it can
// stream: a handler behind this middleware — WatchAgent's SSE stream is
// one — asserts http.Flusher, which the wrapper otherwise hides (#380).
// Unwrap serves the same handlers written against
// http.NewResponseController.
func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap exposes the writer under the recorder, so
// http.NewResponseController and anything else that walks wrappers
// reaches the capabilities the recorder does not name.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
