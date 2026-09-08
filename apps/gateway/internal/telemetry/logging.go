package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var defaultStructuredLogger atomic.Pointer[StructuredLogger]

// SetStructuredLogger installs the process-wide high-signal logger. It keeps
// the existing local writer; it does not create an exporter or network path.
func SetStructuredLogger(logger *StructuredLogger) {
	if logger != nil {
		defaultStructuredLogger.Store(logger)
	}
}

// Log emits through the installed structured logger and is nil-safe.
func Log(ctx context.Context, event LogEvent) error {
	logger := defaultStructuredLogger.Load()
	if logger == nil {
		return nil
	}
	return logger.Log(ctx, event)
}

type LogFormat string

const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

type Severity string

const (
	SeverityDebug Severity = "DEBUG"
	SeverityInfo  Severity = "INFO"
	SeverityWarn  Severity = "WARN"
	SeverityError Severity = "ERROR"
)

type ResourceIdentity struct {
	ServiceName       string
	ServiceVersion    string
	Environment       string
	GatewayInstanceID string
}

type Correlation struct {
	SessionID         string
	CallCorrelationID string
	TraceID           string
	SpanID            string
	TrunkID           int64
}

type Measurement struct {
	Name  string
	Value any
}

func Int64Measurement(name string, value int64) Measurement {
	return Measurement{Name: name, Value: value}
}

func Float64Measurement(name string, value float64) Measurement {
	return Measurement{Name: name, Value: value}
}

func DurationMilliseconds(name string, value time.Duration) Measurement {
	return Measurement{Name: name, Value: float64(value) / float64(time.Millisecond)}
}

type LogEvent struct {
	Severity     Severity
	Component    string
	Name         string
	Outcome      string
	Reason       string
	Correlation  Correlation
	Measurements []Measurement
}

type StructuredLogConfig struct {
	Format    LogFormat
	Resource  ResourceIdentity
	Sanitizer Sanitizer
	Now       func() time.Time
}

// StructuredLogger writes high-signal events to the supplied existing writer.
// It performs no export or network work; stdout/file collection remains owned
// by the deployment and its OpenTelemetry Collector.
type StructuredLogger struct {
	mu        sync.Mutex
	writer    io.Writer
	format    LogFormat
	resource  ResourceIdentity
	sanitizer Sanitizer
	now       func() time.Time
}

func NewStructuredLogger(writer io.Writer, config StructuredLogConfig) *StructuredLogger {
	format := config.Format
	if format != LogFormatText {
		format = LogFormatJSON
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &StructuredLogger{
		writer:    writer,
		format:    format,
		resource:  config.Resource,
		sanitizer: config.Sanitizer,
		now:       now,
	}
}

// Log writes one sanitized record. Context is accepted now so trace correlation
// can be added without changing call sites when tracing is wired in; no values
// are inferred from arbitrary context keys.
func (l *StructuredLogger) Log(_ context.Context, event LogEvent) error {
	if l == nil || l.writer == nil {
		return nil
	}
	record := l.buildRecord(event)

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.format == LogFormatText {
		_, err := io.WriteString(l.writer, encodeText(record)+"\n")
		return err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal structured log: %w", err)
	}
	encoded = append(encoded, '\n')
	_, err = l.writer.Write(encoded)
	return err
}

type structuredRecord struct {
	Timestamp             string         `json:"timestamp"`
	Severity              Severity       `json:"severity"`
	ServiceName           string         `json:"service.name"`
	ServiceVersion        string         `json:"service.version"`
	DeploymentEnvironment string         `json:"deployment.environment"`
	ServiceInstanceID     string         `json:"service.instance.id"`
	Component             string         `json:"component"`
	EventName             string         `json:"event.name"`
	Outcome               string         `json:"outcome,omitempty"`
	Reason                string         `json:"reason,omitempty"`
	SessionID             string         `json:"session.id,omitempty"`
	CallCorrelationID     string         `json:"call.correlation_id,omitempty"`
	TraceID               string         `json:"trace_id,omitempty"`
	SpanID                string         `json:"span_id,omitempty"`
	TrunkID               int64          `json:"trunk.id,omitempty"`
	Measurements          map[string]any `json:"measurements,omitempty"`
}

func (l *StructuredLogger) buildRecord(event LogEvent) structuredRecord {
	sanitizeString := func(key, value string) string {
		clean, _ := l.sanitizer.Sanitize(key, value).(string)
		return clean
	}
	record := structuredRecord{
		Timestamp:             l.now().UTC().Format(time.RFC3339Nano),
		Severity:              normalizeSeverity(event.Severity),
		ServiceName:           sanitizeString("service.name", l.resource.ServiceName),
		ServiceVersion:        sanitizeString("service.version", l.resource.ServiceVersion),
		DeploymentEnvironment: sanitizeString("deployment.environment", l.resource.Environment),
		ServiceInstanceID:     sanitizeString("service.instance.id", l.resource.GatewayInstanceID),
		Component:             sanitizeString("component", event.Component),
		EventName:             sanitizeString("event.name", event.Name),
		Outcome:               sanitizeString("outcome", event.Outcome),
		Reason:                sanitizeString("reason", event.Reason),
		SessionID:             sanitizeString("session.id", event.Correlation.SessionID),
		CallCorrelationID:     sanitizeString("call.correlation_id", event.Correlation.CallCorrelationID),
		TraceID:               sanitizeString("trace_id", event.Correlation.TraceID),
		SpanID:                sanitizeString("span_id", event.Correlation.SpanID),
		TrunkID:               event.Correlation.TrunkID,
	}
	for _, measurement := range event.Measurements {
		name := sanitizeString("measurement.name", measurement.Name)
		if !validMeasurementName(name) {
			continue
		}
		value := l.sanitizer.Sanitize(name, measurement.Value)
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			if record.Measurements == nil {
				record.Measurements = make(map[string]any)
			}
			record.Measurements[name] = value
		}
	}
	return record
}

func validMeasurementName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		if index == 0 && (character < 'a' || character > 'z') {
			return false
		}
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func normalizeSeverity(severity Severity) Severity {
	switch severity {
	case SeverityDebug, SeverityInfo, SeverityWarn, SeverityError:
		return severity
	default:
		return SeverityInfo
	}
}

func encodeText(record structuredRecord) string {
	fields := []string{
		"timestamp=" + quoteText(record.Timestamp),
		"severity=" + string(record.Severity),
		"service.name=" + quoteText(record.ServiceName),
		"service.version=" + quoteText(record.ServiceVersion),
		"deployment.environment=" + quoteText(record.DeploymentEnvironment),
		"service.instance.id=" + quoteText(record.ServiceInstanceID),
		"component=" + quoteText(record.Component),
		"event.name=" + quoteText(record.EventName),
	}
	appendString := func(key, value string) {
		if value != "" {
			fields = append(fields, key+"="+quoteText(value))
		}
	}
	appendString("outcome", record.Outcome)
	appendString("reason", record.Reason)
	appendString("session.id", record.SessionID)
	appendString("call.correlation_id", record.CallCorrelationID)
	appendString("trace_id", record.TraceID)
	appendString("span_id", record.SpanID)
	if record.TrunkID != 0 {
		fields = append(fields, "trunk.id="+strconv.FormatInt(record.TrunkID, 10))
	}
	measurementNames := make([]string, 0, len(record.Measurements))
	for name := range record.Measurements {
		measurementNames = append(measurementNames, name)
	}
	sort.Strings(measurementNames)
	for _, name := range measurementNames {
		fields = append(fields, "measurement."+name+"="+fmt.Sprint(record.Measurements[name]))
	}
	return strings.Join(fields, " ")
}

func quoteText(value string) string {
	return strconv.Quote(value)
}
