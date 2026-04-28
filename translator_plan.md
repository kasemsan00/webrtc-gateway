# แผนการ Implement ฟังก์ชันแปลภาษา S2S สำหรับ webrtc-gateway + ttrs-vri

**วัตถุประสงค์:** ให้ ttrs-vri (React Native mobile) พูดภาษาอังกฤษ → แปลเป็นไทย → ส่งเฉพาะเสียงไทยไปยัง Linphone desktop ผ่าน gateway พร้อมรองรับการแปลข้อความ chat (SIP MESSAGE) ด้วย

**AzureGrpcTranslationServer:** รันแยก service ที่ `TRANSLATOR_ADDR:5000`

---

## ส่วนที่ 1: webrtc-gateway (Go backend)

### 1.1 config — internal/config/config.go

**เพิ่ม struct:**
```go
type TranslatorConfig struct {
    Enable        bool   // TRANSLATOR_ENABLE
    Addr          string // TRANSLATOR_ADDR (default "localhost:5000")
    SourceLang    string // TRANSLATOR_SOURCE_LANG (default "en-US")
    TargetLang    string // TRANSLATOR_TARGET_LANG (default "th")
    TTSVoice      string // TRANSLATOR_TTS_VOICE (default "th-TH-PremwadeeNeural")
    OpusBitrate   int    // TRANSLATOR_OPUS_BITRATE (default 32000)
}
```

**เพิ่ม field ใน Config struct:** `Translator TranslatorConfig`

### 1.2 translator.proto

**คัดลอกจาก AzureGrpcTranslationServer:**

```protobuf
syntax = "proto3";
option go_package = "k2-gateway/internal/translator/pb";

enum TranslationMode {
  MODE_UNSPECIFIED = 0;
  MODE_S2T = 1;
  MODE_S2S = 2;
  MODE_T2S = 3;
}

service SpeechTranslator {
  rpc Translate(stream TranslationRequest) returns (stream TranslationResult);
}

message TranslationRequest {
  string source_language = 1;
  string target_language = 2;
  bool return_audio = 3;
  string tts_voice_name = 4;
  bytes audio_data = 5;
  string text_input = 6;
  TranslationMode mode = 7;
}

message TranslationResult {
  string recognized_text = 1;
  string translated_text = 2;
  bytes audio_data = 3;
  string tts_voice_used = 4;
}
```

**Generate stub:** `protoc --go_out=. --go-grpc_out=. translator.proto`

### 1.3 internal/translator/client.go — ไฟล์ใหม่

**gRPC client เชื่อมต่อ Azure:**

```go
type Client struct {
    conn   *grpc.ClientConn
    client pb.SpeechTranslatorClient
}

func NewClient(addr string) (*Client, error)
func (c *Client) TranslateStream(ctx context.Context) (pb.SpeechTranslator_TranslateClient, error)
func (c *Client) CheckHealth(ctx context.Context) error
func (c *Client) Close()
```

### 1.4 internal/translator/opus.go — ไฟล์ใหม่

**Opus codec functions:**

```go
// DecodeOpusRTP — แยก RTP header (12 bytes) → decode Opus payload → PCM int16 16kHz
func DecodeOpusRTP(rtpPacket []byte, sampleRate int) ([]int16, error)

// EncodeOpusRTP — PCM int16 → Opus encode → RTP packetize (seq, ssrc, timestamp, pt)
func EncodeOpusRTP(pcm []int16, seq uint16, ssrc uint32, timestamp uint32, pt uint8, bitrate int) ([]byte, error)
```

**ใช้ `github.com/gopxl/opus`** (cgo binding to libopus):
- decoder: `opus.NewDecoder(sampleRate, channels)` → `decoder.Decode(pcm, frameSize)`
- encoder: `opus.NewEncoder(sampleRate, channels, opus.AppVoIP)` → `encoder.Encode(pcm, frameSize)`

**หมายเหตุ:** libopus ต้องติดตั้งในระบบ หรือใช้ docker image ที่มี libopus

### 1.5 internal/translator/orchestrator.go — ไฟล์ใหม่

**Orchestrator pipeline:**

```
Audio fork (Opus RTP payload)
  → Opus decode → PCM
  → gRPC Translate() stream → Azure S2S (source=targetLang, target=sourceLang, return_audio=true)
  → gRPC response stream:
      ├─ Recognizing → partial text → (optional SIP MESSAGE subtitle)
      └─ Recognized → translated_text + audioData (PCM)
                        → Opus encode → RTP packetize
                        → WriteToUDP → Asterisk → Linphone
```

**Struct:**
```go
type Orchestrator struct {
    client       *Client
    stream       pb.SpeechTranslator_TranslateClient
    session      *session.Session
    sipServer    *sip.Server
    sourceLang   string
    targetLang   string
    ttsVoice     string
    
    // Audio state
    audioSeq     uint16
    audioSSRC    uint32
    audioPT      uint8
    
    // Channels
    inputCh      chan []byte   // Opus RTP payload จาก fork
    done         chan struct{}
}
```

**Key methods:**
- `NewOrchestrator(client, session, sipServer, cfg)` — create pipeline
- `Start()` — เปิด gRPC stream, start goroutine receive loop
- `Stop()` — close gRPC stream, cleanup
- `pushAudio(opusPayload []byte)` — non-blocking send to inputCh
- `processLoop()` — goroutine: อ่าน inputCh → Opus decode → gRPC send
- `receiveLoop()` — goroutine: อ่าน gRPC response → Recognized → Opus encode → RTP write to Asterisk

### 1.6 session — internal/session/session.go

**เพิ่ม fields ใน Session struct:**
```go
Translator       *translator.Orchestrator
TranslatorEnabled bool
TranslatorSrcLang string
TranslatorTgtLang string
TranslatorTTSVoice string
```

### 1.7 rtp_forward — internal/session/rtp_forward.go

**แก้ `forwardRTPToAsterisk()` — ส่วน audio (kind=="audio"):**

```go
// หลัง Parse RTP packet (ประมาณบรรทัด 33)
if kind == "audio" && s.TranslatorEnabled && s.Translator != nil {
    // ส่ง Opus payload เข้า orchestrator pipeline
    // orchestrator จะ decode → gRPC → Azure → Encode → RTP → Asterisk
    s.Translator.PushAudio(packet.Payload)
    // ไม่ forward audio ต้นฉบับไป Asterisk
    continue
}
```

**หมายเหตุ:** gateway จะหยุด forward audio ต้นฉบับไป Asterisk ทันทีที่ translation เปิด (Linphone จะเงียบจนกว่า TTS จะมา)

### 1.8 SIP MESSAGE translate — internal/sip/message.go

**ในฟังก์ชันที่รับ SIP MESSAGE (in-dialog):**

```go
// หลัง Parse SIP MESSAGE body — ถ้า session มี translator เปิด
if sess.TranslatorEnabled && sess.Translator != nil && !isRTT(body) && !isControl(body) {
    // ส่งข้อความไป T2S แปล
    translatedText, err := sess.Translator.TranslateText(ctx, body, sess.TranslatorSrcLang, sess.TranslatorTgtLang)
    if err == nil {
        // ส่ง translated_text ไปยัง api server เพื่อส่ง WS message → ttrs-vri
        // ส่ง original + translated
        s.messageNotifier.NotifyTranslatedMessage(sess.ID, body, translatedText)
        // ไม่ส่งข้อความต้นฉบับไป WS (ส่งเฉพาะ translated)
        return
    }
}
// fallback: ส่งต้นฉบับปกติ
```

### 1.9 api server — internal/api/server.go

**WebSocket message handlers (ใน `handleWebSocket` switch):**

```go
case "translate":
    // รับ: { source_language, target_language, tts_voice_name }
    // เปิด orchestrator สำหรับ session นี้
    h.handleTranslate(ws, session, msg)
    
case "translate_stop":
    // หยุด orchestrator, revert audio เป็น passthrough
    h.handleTranslateStop(ws, session)
```

**REST endpoint:**
```go
r.HandleFunc("/api/translator/status", h.handleTranslatorStatus).Methods("GET")
```

### 1.10 api handlers — internal/api/handlers.go

**เพิ่ม handler functions:**

```go
func (h *Server) handleTranslate(ws *websocket.Conn, sess *session.Session, msg map[string]interface{})
    // 1. Parse source_language, target_language, tts_voice_name
    // 2. Create orchestrator
    // 3. orchestrator.Start()
    // 4. sess.Translator = orchestrator
    // 5. sess.TranslatorEnabled = true
    // 6. อัปเดต src/tgt lang fields
    // 7. Send translation_started WS event
    
func (h *Server) handleTranslateStop(ws *websocket.Conn, sess *session.Session)
    // 1. sess.Translator.Stop()
    // 2. sess.Translator = nil
    // 3. sess.TranslatorEnabled = false
    // 4. Send translation_stopped WS event
    // 5. Audio กลับสู่ Opus passthrough ปกติ

func (h *Server) handleTranslatorStatus(w http.ResponseWriter, r *http.Request)
    // 1. gRPC health check ไป Azure
    // 2. Return { "status": "ok" | "error", "addr": config }
```

### 1.11 main.go — inject dependencies

```go
// หลัง init SIP server
var translatorClient *translator.Client
if cfg.Translator.Enable {
    translatorClient, err = translator.NewClient(cfg.Translator.Addr)
    if err != nil {
        log.Printf("Warning: translator init failed: %v", err)
    }
}

// ส่ง translatorClient ไปยัง API server
apiServer.SetTranslatorClient(translatorClient)
```

---

## ส่วนที่ 2: ttrs-vri-webrtc-react-native (React Native)

### 2.1 types — lib/gateway/types.ts

```ts
export interface TranslationData {
  source_language: string
  target_language: string
  tts_voice_name?: string
}

export interface TranslatedMessageData {
  original_text: string
  translated_text: string
  source_language: string
  target_language: string
}

export interface GatewayCallbacks {
  // ... existing callbacks
  
  onTranslationStarted?: (data: TranslationData) => void
  onTranslationStopped?: () => void
  onTranslationError?: (error: string) => void
  onTranslatedMessage?: (data: TranslatedMessageData) => void
}
```

### 2.2 gateway-client — lib/gateway/gateway-client.ts

**เพิ่ม methods:**
```ts
sendTranslate(srcLang: string, tgtLang: string, ttsVoice?: string) {
  this.send({
    type: 'translate',
    source_language: srcLang,
    target_language: tgtLang,
    tts_voice_name: ttsVoice,
  })
}

sendTranslateStop() {
  this.send({ type: 'translate_stop' })
}
```

**เพิ่ม cases ใน `onmessage` handler:**
```ts
case 'translation_started':
  this._callbacks?.onTranslationStarted?.(msg)
  break
case 'translation_stopped':
  this._callbacks?.onTranslationStopped?.()
  break
case 'translation_error':
  this._callbacks?.onTranslationError?.(msg.error || 'Translation error')
  break
case 'translated_message':
  this._callbacks?.onTranslatedMessage?.(msg)
  break
```

### 2.3 sip-store — store/sip-store.ts

**เพิ่ม state:**
```ts
translationEnabled: boolean
translationSourceLang: string
translationTargetLang: string
translationTtsVoice: string
isTranslating: boolean
translationError: string | null
translatedMessages: Map<string, string>  // original → translated lookup
```

**เพิ่ม actions:**
```ts
enableTranslation: (srcLang?: string, tgtLang?: string, ttsVoice?: string) => {
  const settings = useSettingsStore.getState()
  const source = srcLang || settings.translationSourceLang || 'en-US'
  const target = tgtLang || settings.translationTargetLang || 'th'
  const voice = ttsVoice || settings.translationTtsVoice || 'th-TH-PremwadeeNeural'
  
  set({ isTranslating: true, translationError: null })
  get().gatewayClient?.sendTranslate(source, target, voice)
}

disableTranslation: () => {
  get().gatewayClient?.sendTranslateStop()
}
```

**Wire callbacks ใน `connect()`:**
```ts
gatewayClient.onTranslationStarted = (data) => {
  set({
    translationEnabled: true,
    isTranslating: false,
    translationSourceLang: data.source_language,
    translationTargetLang: data.target_language,
  })
}

gatewayClient.onTranslationStopped = () => {
  set({
    translationEnabled: false,
    isTranslating: false,
    translatedMessages: {},
  })
}

gatewayClient.onTranslationError = (error) => {
  set({ translationError: error, translationEnabled: false, isTranslating: false })
}

gatewayClient.onTranslatedMessage = (data) => {
  const state = get()
  // เพิ่ม translated message ลง messages array
  set({
    messages: [...state.messages, {
      id: generateId(),
      text: data.translated_text,
      originalText: data.original_text,
      sourceLang: data.source_language,
      targetLang: data.target_language,
      isIncoming: true,
      timestamp: new Date(),
    }]
  })
}
```

### 2.4 settings-store — store/settings-store.ts

```ts
// เพิ่ม fields (persisted in MMKV)
translationSourceLang: string   // default 'en-US'
translationTargetLang: string   // default 'th'
translationTtsVoice: string     // default 'th-TH-PremwadeeNeural'
```

### 2.5 in-call-screen — components/softphone/in-call-screen.tsx

**เพิ่ม components:**

```tsx
// 1. ปุ่ม toggle translation (ถัดจากปุ่ม Chat)
<ActionButton
  icon={translationEnabled ? Languages : Globe}
  label={translationEnabled ? 'EN→TH' : 'แปล'}
  onPress={() => {
    if (translationEnabled) {
      disableTranslation()
    } else {
      setShowLangPicker(true)
    }
  }}
  variant={translationEnabled ? 'active' : 'default'}
/>

// 2. Language picker modal
<Modal visible={showLangPicker}>
  <View>
    <Text>เลือกภาษาปลายทาง</Text>
    {LANGUAGES.map(lang => (
      <TouchableOpacity key={lang.code} onPress={() => {
        enableTranslation('en-US', lang.code, lang.voice)
        setShowLangPicker(false)
      }}>
        <Text>{lang.label}</Text>
      </TouchableOpacity>
    ))}
  </View>
</Modal>

// 3. Status badge
{translationEnabled && (
  <View style={styles.translationBadge}>
    <Text>EN → TH</Text>
  </View>
)}
```

**Language options:**
```ts
const LANGUAGES = [
  { code: 'th', label: 'ไทย', voice: 'th-TH-PremwadeeNeural' },
  { code: 'en', label: 'English', voice: 'en-US-JennyNeural' },
  { code: 'zh', label: '中文', voice: 'zh-CN-XiaoxiaoNeural' },
  { code: 'ja', label: '日本語', voice: 'ja-JP-NanamiNeural' },
  { code: 'ko', label: '한국어', voice: 'ko-KR-SunHiNeural' },
  { code: 'de', label: 'Deutsch', voice: 'de-DE-KatjaNeural' },
  { code: 'fr', label: 'Français', voice: 'fr-FR-DeniseNeural' },
  { code: 'ru', label: 'Русский', voice: 'ru-RU-SvetlanaNeural' },
  { code: 'hi', label: 'हिन्दी', voice: 'hi-IN-SwaraNeural' },
]
```

### 2.6 chat — components/softphone/chat.tsx

**ChatMessage type เพิ่ม field:**
```ts
interface ChatMessage {
  // ...
  originalText?: string
  sourceLang?: string
  targetLang?: string
}
```

**Render translated message:**
```tsx
{msg.originalText && (
  <View style={styles.originalTextContainer}>
    <Text style={styles.originalText}>{msg.originalText}</Text>
  </View>
)}
<View style={styles.messageBubble}>
  <Text style={styles.messageText}>{msg.text}</Text>
</View>
```

---

## ส่วนที่ 3: Dependencies

### webrtc-gateway (go.mod)

```
google.golang.org/grpc v1.71.x
google.golang.org/protobuf v1.36.x
github.com/gopxl/opus v0.0.0-202410xxxxxxxx  // cgo — ต้อง libopus
```

### ttrs-vri (package.json)

ไม่ต้องเพิ่ม dependency — ใช้ WebSocket ที่มีอยู่แล้ว

---

## ส่วนที่ 4: Environment Variables

### .env (webrtc-gateway)

```env
TRANSLATOR_ENABLE=true
TRANSLATOR_ADDR=203.x.x.x:5000
TRANSLATOR_SOURCE_LANG=en-US
TRANSLATOR_TARGET_LANG=th
TRANSLATOR_TTS_VOICE=th-TH-PremwadeeNeural
TRANSLATOR_OPUS_BITRATE=32000
```

### .env (ttrs-vri)

ไม่ต้องเพิ่ม — ส่งผ่าน WebSocket command

---

## ลำดับการ Implement

### Phase 1: Gateway Foundation
1. เพิ่ม `TranslatorConfig` ใน `config.go`
2. คัดลอก `translator.proto` + generate Go stub
3. เขียน `internal/translator/client.go` — gRPC connection
4. เขียน `internal/translator/opus.go` — Opus codec
5. เขียน `internal/translator/orchestrator.go` — pipeline

### Phase 2: Gateway Integration
6. แก้ `session.go` — เพิ่ม translator fields
7. แก้ `rtp_forward.go` — fork audio เมื่อ translator เปิด
8. แก้ `server.go` — register WS handlers
9. เขียน `handlers.go` — translate/translate_stop handlers
10. แก้ `main.go` — init translator client

### Phase 3: Chat Translation
11. แก้ `message.go` — SIP MESSAGE translate

### Phase 4: ttrs-vri
12. แก้ `types.ts` — เพิ่ม callback types
13. แก้ `gateway-client.ts` — sendTranslate + receive events
14. แก้ `sip-store.ts` — state + actions + callback wiring
15. แก้ `settings-store.ts` — persisted language prefs
16. แก้ `in-call-screen.tsx` — toggle button + lang picker
17. แก้ `chat.tsx` — show translated text

---

## ข้อควรระวัง

1. **Opus decode/encode ต้องใช้ libopus** — `github.com/gopxl/opus` เป็น cgo binding ต้องมี libopus-dev ใน build environment (Docker ต้อง apt install)
2. **gRPC connection fail** — ถ้า AzureGrpcTranslationServer ไม่พร้อม ต้องไม่ crash gateway: orchestrator.Start() ควร return error และ fallback เป็น passthrough
3. **Audio timing** — RTP seq/timestamp ต้องต่อเนื่องเมื่อกลับจาก translation → passthrough (หรือ reset SDP)
4. **SIP MESSAGE ที่ไม่ใช่ chat** — ต้อง filter RTT XML (`<rtt...`) และ control messages (`@signal`, `@switch`, `@video`) ไม่ส่งเข้า T2S
5. **Resource cleanup** — เมื่อ orchestrator.Stop() ต้อง close gRPC stream และ release Opus encoder/decoder
