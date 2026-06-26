package pb

import (
	reflect "reflect"
	sync "sync"

	proto "google.golang.org/protobuf/proto"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

const (
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type TranslationMode int32

const (
	TranslationMode_MODE_UNSPECIFIED TranslationMode = 0
	TranslationMode_MODE_S2T         TranslationMode = 1
	TranslationMode_MODE_S2S         TranslationMode = 2
	TranslationMode_MODE_T2S         TranslationMode = 3
)

var (
	TranslationMode_name = map[int32]string{
		0: "MODE_UNSPECIFIED",
		1: "MODE_S2T",
		2: "MODE_S2S",
		3: "MODE_T2S",
	}
	TranslationMode_value = map[string]int32{
		"MODE_UNSPECIFIED": 0,
		"MODE_S2T":         1,
		"MODE_S2S":         2,
		"MODE_T2S":         3,
	}
)

func (x TranslationMode) Enum() *TranslationMode {
	p := new(TranslationMode)
	*p = x
	return p
}

func (x TranslationMode) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (TranslationMode) Descriptor() protoreflect.EnumDescriptor {
	return file_proto_translator_proto_enumTypes[0].Descriptor()
}

func (TranslationMode) Type() protoreflect.EnumType {
	return &file_proto_translator_proto_enumTypes[0]
}

func (x TranslationMode) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use TranslationMode.Descriptor instead.
func (TranslationMode) EnumDescriptor() ([]byte, []int) {
	return file_proto_translator_proto_rawDescGZIP(), []int{0}
}

type TranslationRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	SourceLanguage string          `protobuf:"bytes,1,opt,name=source_language,json=sourceLanguage,proto3" json:"source_language,omitempty"`
	TargetLanguage string          `protobuf:"bytes,2,opt,name=target_language,json=targetLanguage,proto3" json:"target_language,omitempty"`
	ReturnAudio    bool            `protobuf:"varint,3,opt,name=return_audio,json=returnAudio,proto3" json:"return_audio,omitempty"`
	TTSVoiceName   string          `protobuf:"bytes,4,opt,name=tts_voice_name,json=ttsVoiceName,proto3" json:"tts_voice_name,omitempty"`
	AudioData      []byte          `protobuf:"bytes,5,opt,name=audio_data,json=audioData,proto3" json:"audio_data,omitempty"`
	TextInput      string          `protobuf:"bytes,6,opt,name=text_input,json=textInput,proto3" json:"text_input,omitempty"`
	Mode           TranslationMode `protobuf:"varint,7,opt,name=mode,proto3,enum=pb.TranslationMode" json:"mode,omitempty"`
}

func (x *TranslationRequest) Reset() {
	*x = TranslationRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_proto_translator_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *TranslationRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TranslationRequest) ProtoMessage() {}

func (x *TranslationRequest) ProtoReflect() protoreflect.Message {
	mi := &file_proto_translator_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TranslationRequest.ProtoReflect.Descriptor instead.
func (*TranslationRequest) Descriptor() ([]byte, []int) {
	return file_proto_translator_proto_rawDescGZIP(), []int{0}
}

func (x *TranslationRequest) GetSourceLanguage() string {
	if x != nil {
		return x.SourceLanguage
	}
	return ""
}

func (x *TranslationRequest) GetTargetLanguage() string {
	if x != nil {
		return x.TargetLanguage
	}
	return ""
}

func (x *TranslationRequest) GetReturnAudio() bool {
	if x != nil {
		return x.ReturnAudio
	}
	return false
}

func (x *TranslationRequest) GetTTSVoiceName() string {
	if x != nil {
		return x.TTSVoiceName
	}
	return ""
}

func (x *TranslationRequest) GetAudioData() []byte {
	if x != nil {
		return x.AudioData
	}
	return nil
}

func (x *TranslationRequest) GetTextInput() string {
	if x != nil {
		return x.TextInput
	}
	return ""
}

func (x *TranslationRequest) GetMode() TranslationMode {
	if x != nil {
		return x.Mode
	}
	return TranslationMode_MODE_UNSPECIFIED
}

type TranslationResult struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	RecognizedText string `protobuf:"bytes,1,opt,name=recognized_text,json=recognizedText,proto3" json:"recognized_text,omitempty"`
	TranslatedText string `protobuf:"bytes,2,opt,name=translated_text,json=translatedText,proto3" json:"translated_text,omitempty"`
	AudioData      []byte `protobuf:"bytes,3,opt,name=audio_data,json=audioData,proto3" json:"audio_data,omitempty"`
	TTSVoiceUsed   string `protobuf:"bytes,4,opt,name=tts_voice_used,json=ttsVoiceUsed,proto3" json:"tts_voice_used,omitempty"`
}

func (x *TranslationResult) Reset() {
	*x = TranslationResult{}
	if protoimpl.UnsafeEnabled {
		mi := &file_proto_translator_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *TranslationResult) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TranslationResult) ProtoMessage() {}

func (x *TranslationResult) ProtoReflect() protoreflect.Message {
	mi := &file_proto_translator_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TranslationResult.ProtoReflect.Descriptor instead.
func (*TranslationResult) Descriptor() ([]byte, []int) {
	return file_proto_translator_proto_rawDescGZIP(), []int{1}
}

func (x *TranslationResult) GetRecognizedText() string {
	if x != nil {
		return x.RecognizedText
	}
	return ""
}

func (x *TranslationResult) GetTranslatedText() string {
	if x != nil {
		return x.TranslatedText
	}
	return ""
}

func (x *TranslationResult) GetAudioData() []byte {
	if x != nil {
		return x.AudioData
	}
	return nil
}

func (x *TranslationResult) GetTTSVoiceUsed() string {
	if x != nil {
		return x.TTSVoiceUsed
	}
	return ""
}

var File_proto_translator_proto protoreflect.FileDescriptor

var file_proto_translator_proto_rawDescOnce sync.Once
var file_proto_translator_proto_rawDescData = buildTranslatorRawDescriptor()

func file_proto_translator_proto_rawDescGZIP() []byte {
	file_proto_translator_proto_rawDescOnce.Do(func() {
		file_proto_translator_proto_rawDescData = protoimpl.X.CompressGZIP(file_proto_translator_proto_rawDescData)
	})
	return file_proto_translator_proto_rawDescData
}

var file_proto_translator_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_proto_translator_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_proto_translator_proto_goTypes = []interface{}{
	(TranslationMode)(0),
	(*TranslationRequest)(nil),
	(*TranslationResult)(nil),
}
var file_proto_translator_proto_depIdxs = []int32{
	0, // 0: TranslationRequest.mode:type_name -> TranslationMode
	2, // 1: SpeechTranslator.Translate:output_type -> TranslationResult
	1, // 2: SpeechTranslator.Translate:input_type -> TranslationRequest
	1, // [1:2] is the sub-list for method output_type
	2, // [2:3] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_proto_translator_proto_init() }

func file_proto_translator_proto_init() {
	if File_proto_translator_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_proto_translator_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*TranslationRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_proto_translator_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*TranslationResult); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_proto_translator_proto_rawDescData,
			NumEnums:      1,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_proto_translator_proto_goTypes,
		DependencyIndexes: file_proto_translator_proto_depIdxs,
		EnumInfos:         file_proto_translator_proto_enumTypes,
		MessageInfos:      file_proto_translator_proto_msgTypes,
	}.Build()
	File_proto_translator_proto = out.File
	file_proto_translator_proto_rawDescData = nil
	file_proto_translator_proto_goTypes = nil
	file_proto_translator_proto_depIdxs = nil
}

func buildTranslatorRawDescriptor() []byte {
	syntax := "proto3"
	name := "proto/translator.proto"
	csharpNamespace := "AzureGrpcTranslationServer"
	modeTypeName := ".pb.TranslationMode"

	fd := &descriptorpb.FileDescriptorProto{
		Name:   &name,
		Syntax: &syntax,
		Options: &descriptorpb.FileOptions{
			CsharpNamespace: &csharpNamespace,
		},
		EnumType: []*descriptorpb.EnumDescriptorProto{
			{
				Name: proto.String("TranslationMode"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("MODE_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("MODE_S2T"), Number: proto.Int32(1)},
					{Name: proto.String("MODE_S2S"), Number: proto.Int32(2)},
					{Name: proto.String("MODE_T2S"), Number: proto.Int32(3)},
				},
			},
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("TranslationRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("source_language", 1),
					stringField("target_language", 2),
					boolField("return_audio", 3),
					stringField("tts_voice_name", 4),
					bytesField("audio_data", 5),
					stringField("text_input", 6),
					enumField("mode", 7, modeTypeName),
				},
			},
			{
				Name: proto.String("TranslationResult"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("recognized_text", 1),
					stringField("translated_text", 2),
					bytesField("audio_data", 3),
					stringField("tts_voice_used", 4),
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("SpeechTranslator"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:            proto.String("Translate"),
						InputType:       proto.String(".pb.TranslationRequest"),
						OutputType:      proto.String(".pb.TranslationResult"),
						ClientStreaming: proto.Bool(true),
						ServerStreaming: proto.Bool(true),
					},
				},
			},
		},
	}
	b, err := proto.Marshal(fd)
	if err != nil {
		panic(err)
	}
	return b
}

func stringField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_STRING)
}

func bytesField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_BYTES)
}

func boolField(name string, number int32) *descriptorpb.FieldDescriptorProto {
	return scalarField(name, number, descriptorpb.FieldDescriptorProto_TYPE_BOOL)
}

func scalarField(name string, number int32, typ descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(number),
		Label:  &label,
		Type:   &typ,
	}
}

func enumField(name string, number int32, typeName string) *descriptorpb.FieldDescriptorProto {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	typ := descriptorpb.FieldDescriptorProto_TYPE_ENUM
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(number),
		Label:    &label,
		Type:     &typ,
		TypeName: proto.String(typeName),
	}
}
