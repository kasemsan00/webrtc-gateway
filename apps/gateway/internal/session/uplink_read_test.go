package session

import (
	"errors"
	"io"
	"testing"
)

func TestVideoReadShouldStopOnlyWhenTrackIsGone(t *testing.T) {
	if videoReadShouldStop(nil, false) {
		t.Fatal("nil error stopped the reader")
	}
	if videoReadShouldStop(io.ErrShortBuffer, false) {
		t.Fatal("short buffer stopped the reader")
	}
	if videoReadShouldStop(errors.New("temporary"), false) {
		t.Fatal("temporary error stopped the reader")
	}
	if !videoReadShouldStop(io.ErrShortBuffer, true) {
		t.Fatal("ended session kept reading")
	}
	if !videoReadShouldStop(io.EOF, false) {
		t.Fatal("EOF kept reading")
	}
	if !videoReadShouldStop(errors.New("interceptor is closed"), false) {
		t.Fatal("closed interceptor kept reading")
	}
}
