package pb

import "fmt"

type TranslationMode int32

const (
	TranslationMode_MODE_UNSPECIFIED TranslationMode = 0
	TranslationMode_MODE_S2T         TranslationMode = 1
	TranslationMode_MODE_S2S         TranslationMode = 2
	TranslationMode_MODE_T2S         TranslationMode = 3
)

var TranslationMode_name = map[int32]string{
	0: "MODE_UNSPECIFIED",
	1: "MODE_S2T",
	2: "MODE_S2S",
	3: "MODE_T2S",
}

var TranslationMode_value = map[string]int32{
	"MODE_UNSPECIFIED": 0,
	"MODE_S2T":         1,
	"MODE_S2S":         2,
	"MODE_T2S":         3,
}

func (x TranslationMode) String() string {
	if s, ok := TranslationMode_name[int32(x)]; ok {
		return s
	}
	return fmt.Sprintf("TranslationMode(%d)", x)
}

type TranslationRequest struct {
	SourceLanguage string          `json:"source_language,omitempty"`
	TargetLanguage string          `json:"target_language,omitempty"`
	ReturnAudio    bool            `json:"return_audio,omitempty"`
	TTSVoiceName   string          `json:"tts_voice_name,omitempty"`
	AudioData      []byte          `json:"audio_data,omitempty"`
	TextInput      string          `json:"text_input,omitempty"`
	Mode           TranslationMode `json:"mode,omitempty"`
}

type TranslationResult struct {
	RecognizedText string `json:"recognized_text,omitempty"`
	TranslatedText string `json:"translated_text,omitempty"`
	AudioData      []byte `json:"audio_data,omitempty"`
	TTSVoiceUsed   string `json:"tts_voice_used,omitempty"`
}
