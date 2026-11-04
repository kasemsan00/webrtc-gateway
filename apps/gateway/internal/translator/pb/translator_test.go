package pb

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestTranslationRequestImplementsProtoMessage(t *testing.T) {
	req := &TranslationRequest{
		SourceLanguage: "en",
		TargetLanguage: "th",
		ReturnAudio:    true,
		TTSVoiceName:   "th-TH-Sarawut",
		AudioData:      []byte{1, 2, 3, 4},
		Mode:           TranslationMode_MODE_S2S,
	}

	wire, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal TranslationRequest: %v", err)
	}

	var got TranslationRequest
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal TranslationRequest: %v", err)
	}

	if got.SourceLanguage != req.SourceLanguage ||
		got.TargetLanguage != req.TargetLanguage ||
		got.ReturnAudio != req.ReturnAudio ||
		got.TTSVoiceName != req.TTSVoiceName ||
		got.Mode != req.Mode ||
		string(got.AudioData) != string(req.AudioData) {
		t.Fatalf("round trip mismatch: got lang=%q/%q voice=%q mode=%q audio=%d want lang=%q/%q voice=%q mode=%q audio=%d",
			got.SourceLanguage, got.TargetLanguage, got.TTSVoiceName, got.Mode, len(got.AudioData),
			req.SourceLanguage, req.TargetLanguage, req.TTSVoiceName, req.Mode, len(req.AudioData))
	}
}

func TestTranslationResultImplementsProtoMessage(t *testing.T) {
	res := &TranslationResult{
		RecognizedText: "hello",
		TranslatedText: "sawasdee",
		AudioData:      []byte{5, 6, 7, 8},
		TTSVoiceUsed:   "th-TH-Sarawut",
	}

	wire, err := proto.Marshal(res)
	if err != nil {
		t.Fatalf("marshal TranslationResult: %v", err)
	}

	var got TranslationResult
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal TranslationResult: %v", err)
	}

	if got.RecognizedText != res.RecognizedText ||
		got.TranslatedText != res.TranslatedText ||
		got.TTSVoiceUsed != res.TTSVoiceUsed ||
		string(got.AudioData) != string(res.AudioData) {
		t.Fatalf("round trip mismatch: got text=%q/%q voice=%q audio=%d want text=%q/%q voice=%q audio=%d",
			got.RecognizedText, got.TranslatedText, got.TTSVoiceUsed, len(got.AudioData),
			res.RecognizedText, res.TranslatedText, res.TTSVoiceUsed, len(res.AudioData))
	}
}
