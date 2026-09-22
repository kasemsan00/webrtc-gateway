package chatimage

import (
	"bytes"
	"net/http"
	"strings"
)

const (
	ContentTypeJPEG = "image/jpeg"
	ContentTypePNG  = "image/png"
	ContentTypeGIF  = "image/gif"
	ContentTypeWEBP = "image/webp"
)

var allowedContentTypes = map[string]struct{}{
	ContentTypeJPEG: {},
	ContentTypePNG:  {},
	ContentTypeGIF:  {},
	ContentTypeWEBP: {},
}

// DetectContentType inspects magic bytes. The caller-supplied Content-Type is ignored.
func DetectContentType(data []byte) (string, bool) {
	if len(data) < 12 {
		return "", false
	}
	if bytes.HasPrefix(data, []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return ContentTypeWEBP, true
	}
	detected := http.DetectContentType(data)
	if slash := strings.IndexByte(detected, ';'); slash >= 0 {
		detected = detected[:slash]
	}
	detected = strings.TrimSpace(strings.ToLower(detected))
	if _, ok := allowedContentTypes[detected]; ok {
		return detected, true
	}
	return "", false
}
