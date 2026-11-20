package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

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

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/machine"
)

// The service name every trace, metric and log line carries, so one
// stream tells the servers apart.
const (
	serviceAPI     = "garam-api"
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
// are collected from. It sits in the composition root so each serve
// command builds its own.
type observability struct {
	logger   *slog.Logger
	registry *prometheus.Registry
}

// newObservability builds the logger, the metrics registry and the
// tracer for one server. Metrics are defined by the otelhttp
// instrumentation, which finds the providers set here as globals; the registry is the one place they are collected from.
func newObservability(service string) (*observability, error) {
	logger := newLogger(os.Stderr, service)
	// A line written through the default logger lands in the same
	// stream as the handler lines below, and carries the same trace id:
	// the surfaces write their error-path lines that way.
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
	// the propagator keeps that id across a call between processes.
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithResource(res)))
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &observability{logger: logger, registry: registry}, nil
}

// newLogger is the logger one server writes every line through: JSON on
// w, the service name on each line, and the call's trace id supplied by
// the handler rather than by the caller.
func newLogger(w io.Writer, service string) *slog.Logger {
	return slog.New(tracedHandler{Handler: slog.NewJSONHandler(w, nil)}).
		With(slog.String("service", service))
}

// tracedHandler puts trace_id on every record, so a line written
// anywhere inside a served call carries it without naming it —
// stack-go.md §Logging & observability asks for the field on handler
// lines, and a handler that writes one through log/slog reaches this
// rather than an id of its own. The id is [traceIDOf]'s: the call's
// span, or the fallback that rule names when nothing started one.
type tracedHandler struct{ slog.Handler }

func (h tracedHandler) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(slog.String("trace_id", traceIDOf(ctx)))
	return h.Handler.Handle(ctx, r)
}

func (h tracedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return tracedHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h tracedHandler) WithGroup(name string) slog.Handler {
	return tracedHandler{Handler: h.Handler.WithGroup(name)}
}

// metricsHandler serves the registry in the prometheus text format for
// a scraper to pull. A collector that cannot read its source, as the
// policy feed's when the database is down, leaves its own metric out
// of the scrape and does not fail the rest of it.
func (o *observability) metricsHandler() http.Handler {
	return promhttp.HandlerFor(o.registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	})
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
// once the handler has had its chance to resolve one. The cause the
// surface's error writer recorded rides the same line, at whatever
// status: the client body withheld it and the rule in
// runtime-safety.md §Errors reach a handler puts it here instead.
//
// The level is not raised with it. A 4xx is an answer the server
// meant to give, and a duplicate registration that pages an operator
// is worse than one that is merely diagnosable.
func (o *observability) logServed(w http.ResponseWriter, r *http.Request, next http.Handler, field string, principal func() string) {
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	scrubSpanPath(r)

	next.ServeHTTP(recorder, r)

	attrs := []slog.Attr{
		slog.String("operation", r.Pattern),
		slog.Int("status", recorder.status),
		principalAttr(field, principal()),
	}
	if recorder.cause != nil {
		attrs = append(attrs, slog.String("error", recorder.cause.Error()))
	}
	o.logger.LogAttrs(r.Context(), levelOf(recorder.status >= http.StatusInternalServerError),
		"http request", attrs...)
}

// scrubSpanPath replaces the url.path otelhttp took from the concrete
// path with the route's own, for the span the request runs in. A path
// can be a bearer secret — an invitation's is its token (ADR-0006) — and
// the span is exported wherever a deployment points it, so it names the
// route the way the request line does.
func scrubSpanPath(r *http.Request) {
	_, route, found := strings.Cut(r.Pattern, " ")
	if !found {
		route = r.Pattern
	}
	trace.SpanFromContext(r.Context()).SetAttributes(semconv.URLPath(route))
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
				slog.Int("status", http.StatusInternalServerError),
				slog.String("error", fmt.Sprintf("panic: %v\n%s", v, debug.Stack())))
			http.Error(w, "internal error", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
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
// writer reported for an answer that withheld one.
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
// writers declare: a chain the answer's body does not carry reaches
// the line logServed emits instead of the client.
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
