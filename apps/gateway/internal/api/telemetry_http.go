package api

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"webrtc-sip-gateway/internal/telemetry"
)

type telemetryResponseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *telemetryResponseWriter) Flush() {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (writer *telemetryResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (writer *telemetryResponseWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := writer.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (writer *telemetryResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := writer.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(reader)
	}
	return io.Copy(struct{ io.Writer }{writer.ResponseWriter}, reader)
}

func (writer *telemetryResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (writer *telemetryResponseWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *telemetryResponseWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(body)
}

func (s *Server) telemetryHTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// WebSocket connections have their own short control-message spans. Log
		// and health reads are intentionally excluded from noisy HTTP tracing.
		if strings.HasPrefix(request.URL.Path, "/ws") || strings.HasPrefix(request.URL.Path, "/api/logs") || strings.HasPrefix(request.URL.Path, "/api/health") {
			next.ServeHTTP(writer, request)
			return
		}
		route := ""
		if current := mux.CurrentRoute(request); current != nil {
			route, _ = current.GetPathTemplate()
		}
		ctx, span := telemetry.StartHTTPSpan(request.Context(), request.Method, route)
		captured := &telemetryResponseWriter{ResponseWriter: writer}
		started := time.Now()
		next.ServeHTTP(captured, request.WithContext(ctx))
		status := captured.status
		if status == 0 {
			status = http.StatusOK
		}
		outcome, reason := "success", "none"
		if status >= 400 {
			outcome, reason = "failure", httpReason(status)
		}
		telemetry.EndSpan(span, outcome, reason, status)
		_ = telemetry.Log(ctx, telemetry.LogEvent{
			Severity: telemetry.SeverityInfo, Component: "http", Name: "http.request.completed",
			Outcome: outcome, Reason: reason,
			Measurements: []telemetry.Measurement{telemetry.DurationMilliseconds("duration_ms", time.Since(started)), telemetry.Int64Measurement("status_code", int64(status))},
		})
	})
}

func httpReason(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	default:
		if status >= 400 && status < 500 {
			return "invalid_request"
		}
		if status >= 500 {
			return "internal_error"
		}
		return "unknown"
	}
}
