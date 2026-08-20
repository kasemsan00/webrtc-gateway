package api

import (
	"log"
	"strings"

	"webrtc-sip-gateway/internal/session"
)

func (s *Server) handleTranslationCaption(sessionID string, event session.TranslationCaptionEvent) {
	if sessionID == "" || event.Direction != "sip_to_webrtc" {
		return
	}

	s.mu.RLock()
	client := s.wsClients[sessionID]
	s.mu.RUnlock()
	if client == nil {
		log.Printf("[%s] Translation caption dropped: no WebSocket client", sessionID)
		return
	}

	isFinal := event.IsFinal
	s.sendWSMessage(client, WSMessage{
		Type:           "translation_caption",
		SessionID:      sessionID,
		Direction:      event.Direction,
		SourceLang:     event.SourceLang,
		TargetLang:     event.TargetLang,
		RecognizedText: event.RecognizedText,
		TranslatedText: event.TranslatedText,
		IsFinal:        &isFinal,
	})
}

// handleWSTranslate enables S2S speech translation for a session.
func (s *Server) handleWSTranslate(client *WSClient, msg WSMessage) {
	if s.translatorClient == nil {
		s.sendWSError(client, msg.SessionID, "Translator not available")
		return
	}

	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}

	srcLang := msg.SourceLang
	if srcLang == "" {
		srcLang = s.translatorCfg.SourceLang
	}
	tgtLang := msg.TargetLang
	if tgtLang == "" {
		tgtLang = s.translatorCfg.TargetLang
	}
	outboundVoice := translatorVoiceForTargetLang(tgtLang, msg.TTSVoice)
	inboundVoice := translatorVoiceForTargetLang(srcLang, s.translatorCfg.TTSVoice)

	sess.SetTranslator(s.translatorClient, srcLang, tgtLang, outboundVoice, inboundVoice)
	sess.SetTranslationCaptionHandler(func(event session.TranslationCaptionEvent) {
		s.handleTranslationCaption(sessionID, event)
	})
	sess.EnableTranslator()

	log.Printf("[%s] 🎤 Translation enabled via WS: outbound %s → %s (voice: %s), inbound %s → %s (voice: %s)",
		sessionID, srcLang, tgtLang, outboundVoice, tgtLang, srcLang, inboundVoice)

	s.sendWSMessage(client, WSMessage{
		Type:       "translate",
		SessionID:  sessionID,
		State:      "enabled",
		SourceLang: srcLang,
		TargetLang: tgtLang,
		TTSVoice:   outboundVoice,
	})
}

func translatorVoiceForTargetLang(targetLang, fallback string) string {
	if voice, ok := translatorVoiceByLanguage[translatorLanguageBase(targetLang)]; ok {
		return voice
	}
	if fallback != "" {
		return fallback
	}
	return "en-US-AriaNeural"
}

func translatorLanguageBase(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		return ""
	}
	if idx := strings.IndexAny(lang, "-_"); idx >= 0 {
		return lang[:idx]
	}
	return lang
}

var translatorVoiceByLanguage = map[string]string{
	"th": "th-TH-PremwadeeNeural",
	"en": "en-US-AriaNeural",
	"zh": "zh-CN-XiaoxiaoNeural",
	"ko": "ko-KR-SunHiNeural",
	"ja": "ja-JP-NanamiNeural",
	"ru": "ru-RU-SvetlanaNeural",
	"hi": "hi-IN-SwaraNeural",
	"de": "de-DE-KatjaNeural",
	"fr": "fr-FR-DeniseNeural",
}

// handleWSTranslateStop disables S2S speech translation for a session.
func (s *Server) handleWSTranslateStop(client *WSClient, msg WSMessage) {
	sessionID := msg.SessionID
	if sessionID == "" {
		sessionID = client.sessionID
	}
	if sessionID == "" {
		s.sendWSError(client, "", "Session ID required")
		return
	}

	sess, ok := s.sessionMgr.GetSession(sessionID)
	if !ok {
		s.sendWSError(client, sessionID, "Session not found")
		return
	}

	sess.DisableTranslator()

	log.Printf("[%s] 🎤 Translation disabled via WS", sessionID)

	s.sendWSMessage(client, WSMessage{
		Type:      "translate_stop",
		SessionID: sessionID,
		State:     "disabled",
	})
}
