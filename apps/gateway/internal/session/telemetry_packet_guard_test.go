package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestPacketLoopsContainNoTelemetryCallsOrAttributes(t *testing.T) {
	paths := []string{"rtp_forward.go", "rtp_cache.go", "keyframe.go", "video_feedback.go", "video_recovery.go", "rtp_disorder.go"}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.SelectorExpr:
				if identifier, ok := value.X.(*ast.Ident); ok && identifier.Name == "telemetry" {
					t.Errorf("%s calls telemetry.%s", path, value.Sel.Name)
				}
			case *ast.BasicLit:
				lower := strings.ToLower(value.Value)
				for _, forbidden := range []string{"session.id", "call_id", "username", "device", "error.message", "url"} {
					if strings.Contains(lower, forbidden) {
						t.Errorf("%s contains high-cardinality telemetry attribute %q", path, forbidden)
					}
				}
			}
			return true
		})
	}
}

func BenchmarkRTPPacketCacheTelemetryGuard(b *testing.B) {
	sess := &Session{VideoRTPHistorySize: 512}
	sess.initVideoRTPHistory()
	packet := make([]byte, 1200)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		sess.CacheVideoRTPPacket(uint16(index), packet)
	}
}
