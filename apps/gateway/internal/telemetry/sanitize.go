package telemetry

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// RedactedValue is deliberately conspicuous so operators can distinguish a
	// removed value from an absent one.
	RedactedValue  = "[REDACTED]"
	OmittedValue   = "[OMITTED]"
	TruncatedValue = "...[TRUNCATED]"

	defaultMaxStringBytes = 512
	maxSanitizeDepth      = 8
)

var (
	keySeparators = regexp.MustCompile(`[^a-z0-9]+`)

	jwtPattern              = regexp.MustCompile(`(?i)\beyJ[a-z0-9_-]{6,}\.[a-z0-9_-]{6,}\.[a-z0-9_-]{6,}\b`)
	authPattern             = regexp.MustCompile(`(?im)\b(?:proxy-)?authorization\s*[:=]\s*[^\r\n,;]+`)
	bearerPattern           = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[a-z0-9._~+/=-]+`)
	secretAssignmentPattern = regexp.MustCompile(`(?i)\b(password|passwd|secret|access[_-]?token|refresh[_-]?token|api[_-]?key|turn[_-]?credential|push[_-]?token|device[_-]?token)\s*[:=]\s*([^\s,;&]+)`)
	dsnPattern              = regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|redis|amqp(?:s)?)://[^\s]+`)
	sipIdentityPattern      = regexp.MustCompile(`(?i)\b(?:sips?:)?[a-z0-9_.!~*'()%+&=-]+@[a-z0-9.-]+(?::\d+)?\b`)
	ipv4Pattern             = regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1?\d?\d)(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}\b`)
	ipv6Pattern             = regexp.MustCompile(`(?i)(?:\b|\[)(?:[0-9a-f]{1,4}:){2,7}[0-9a-f]{0,4}(?:\b|\])`)
	longTokenPattern        = regexp.MustCompile(`\b[A-Za-z0-9_-]{40,}\b`)

	sipMessagePattern = regexp.MustCompile(`(?im)^\s*(?:INVITE|ACK|BYE|CANCEL|REGISTER|OPTIONS|MESSAGE|INFO|PRACK|UPDATE|REFER|NOTIFY|SUBSCRIBE|PUBLISH)\s+\S+\s+SIP/2\.0\b|^\s*SIP/2\.0\s+\d{3}\b`)
	sdpPattern        = regexp.MustCompile(`(?m)^\s*v=0\s*\r?$`)

	deniedKeys = map[string]struct{}{
		"authorization": {}, "proxyauthorization": {}, "cookie": {}, "setcookie": {},
		"password": {}, "passwd": {}, "secret": {}, "clientsecret": {}, "privatekey": {},
		"token": {}, "accesstoken": {}, "refreshtoken": {}, "idtoken": {}, "apikey": {},
		"credential": {}, "credentials": {}, "turncredential": {}, "turnpassword": {},
		"dsn": {}, "databasedsn": {}, "databaseurl": {}, "connectionstring": {},
		"rawsip": {}, "fullsip": {}, "sipmessage": {}, "siprequest": {}, "sipresponse": {},
		"sdp": {}, "rawsdp": {}, "fullsdp": {}, "offer": {}, "answer": {},
		"body": {}, "messagebody": {}, "transcript": {}, "transcriptbody": {}, "caption": {}, "payload": {},
		"pushtoken": {}, "devicetoken": {}, "registrationtoken": {}, "deviceid": {},
		"ip": {}, "ipaddress": {}, "address": {}, "remoteaddress": {}, "localaddress": {}, "uri": {},
		"sipuri": {}, "sipidentity": {}, "from": {}, "to": {}, "contact": {}, "username": {}, "phonenumber": {}, "number": {},
	}
)

// Sanitizer removes classified private values before they enter structured or
// newly-created legacy log output. It has no network or mutable global state.
type Sanitizer struct {
	MaxStringBytes int
}

func NewSanitizer(maxStringBytes int) Sanitizer {
	if maxStringBytes <= 0 {
		maxStringBytes = defaultMaxStringBytes
	}
	return Sanitizer{MaxStringBytes: maxStringBytes}
}

// Sanitize sanitizes one named value. Keys are matched case-insensitively and
// without punctuation, so Authorization, proxy_authorization, and
// Proxy-Authorization receive the same treatment.
func (s Sanitizer) Sanitize(key string, value any) any {
	return s.sanitize(key, value, 0)
}

// SanitizeMap recursively sanitizes an untrusted map. The result is a fresh
// map, leaving caller-owned data unchanged.
func (s Sanitizer) SanitizeMap(values map[string]any) map[string]any {
	clean := make(map[string]any, len(values))
	for key, value := range values {
		clean[s.sanitizeMapKey(key)] = s.sanitize(key, value, 0)
	}
	return clean
}

// SanitizeText is for newly touched legacy text output. Complete SIP and SDP
// messages are omitted; common inline secret and identity forms are scrubbed.
func (s Sanitizer) SanitizeText(value string) string {
	if looksLikeRawProtocol(value) {
		return OmittedValue
	}

	clean := authPattern.ReplaceAllStringFunc(value, redactAssignment)
	clean = bearerPattern.ReplaceAllString(clean, RedactedValue)
	clean = secretAssignmentPattern.ReplaceAllString(clean, `$1=`+RedactedValue)
	clean = dsnPattern.ReplaceAllString(clean, RedactedValue)
	clean = jwtPattern.ReplaceAllString(clean, RedactedValue)
	clean = sipIdentityPattern.ReplaceAllString(clean, RedactedValue)
	clean = ipv4Pattern.ReplaceAllString(clean, RedactedValue)
	clean = ipv6Pattern.ReplaceAllString(clean, RedactedValue)
	clean = longTokenPattern.ReplaceAllString(clean, RedactedValue)
	return truncateUTF8(clean, s.maxBytes())
}

func (s Sanitizer) sanitize(key string, value any, depth int) any {
	if depth >= maxSanitizeDepth {
		return OmittedValue
	}
	if isDeniedKey(key) {
		return RedactedValue
	}

	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return s.SanitizeText(typed)
	case []byte:
		return s.SanitizeText(string(typed))
	case bool:
		return typed
	case int:
		return typed
	case int8:
		return typed
	case int16:
		return typed
	case int32:
		return typed
	case int64:
		return typed
	case uint:
		return typed
	case uint8:
		return typed
	case uint16:
		return typed
	case uint32:
		return typed
	case uint64:
		return typed
	case float32:
		return typed
	case float64:
		return typed
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			clean[s.sanitizeMapKey(childKey)] = s.sanitize(childKey, childValue, depth+1)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for i, child := range typed {
			clean[i] = s.sanitize(key, child, depth+1)
		}
		return clean
	}

	// Do not serialize arbitrary structs, pointers, errors, or maps with
	// non-string keys: their String or MarshalJSON methods may expose secrets.
	rv := reflect.ValueOf(value)
	if rv.IsValid() && (rv.Kind() == reflect.Map || rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array || rv.Kind() == reflect.Struct || rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		return OmittedValue
	}
	return s.SanitizeText(fmt.Sprint(value))
}

func (s Sanitizer) sanitizeMapKey(key string) string {
	clean := s.SanitizeText(key)
	if clean == "" || clean == RedactedValue || clean == OmittedValue {
		return "redacted_field"
	}
	return clean
}

func (s Sanitizer) maxBytes() int {
	if s.MaxStringBytes <= 0 {
		return defaultMaxStringBytes
	}
	return s.MaxStringBytes
}

func normalizeKey(key string) string {
	return keySeparators.ReplaceAllString(strings.ToLower(strings.TrimSpace(key)), "")
}

func isDeniedKey(key string) bool {
	normalized := normalizeKey(key)
	if normalized == "" {
		return false
	}

	if _, denied := deniedKeys[normalized]; denied {
		return true
	}
	return strings.HasSuffix(normalized, "password") ||
		strings.HasSuffix(normalized, "secret") ||
		strings.HasSuffix(normalized, "token") ||
		strings.HasSuffix(normalized, "authorization") ||
		strings.HasSuffix(normalized, "credential")
}

func looksLikeRawProtocol(value string) bool {
	return sipMessagePattern.MatchString(value) || (sdpPattern.MatchString(value) && (strings.Contains(value, "\nm=") || strings.Contains(value, "\r\nm=")))
}

func redactAssignment(value string) string {
	separator := strings.IndexAny(value, ":=")
	if separator < 0 {
		return RedactedValue
	}
	return value[:separator+1] + " " + RedactedValue
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	limit := maxBytes - len(TruncatedValue)
	if limit <= 0 {
		return TruncatedValue[:maxBytes]
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + TruncatedValue
}
