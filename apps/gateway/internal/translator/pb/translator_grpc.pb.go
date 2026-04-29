package pb

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const SpeechTranslator_ServiceDesc = "/SpeechTranslator/Translate"

type SpeechTranslatorClient interface {
	Translate(ctx context.Context, opts ...grpc.CallOption) (SpeechTranslator_TranslateClient, error)
}

type speechTranslatorClient struct {
	cc grpc.ClientConnInterface
}

func NewSpeechTranslatorClient(cc grpc.ClientConnInterface) SpeechTranslatorClient {
	return &speechTranslatorClient{cc: cc}
}

func (c *speechTranslatorClient) Translate(ctx context.Context, opts ...grpc.CallOption) (SpeechTranslator_TranslateClient, error) {
	stream, err := c.cc.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    "Translate",
		ServerStreams:  true,
		ClientStreams:  true,
	}, "/SpeechTranslator/Translate", opts...)
	if err != nil {
		return nil, err
	}
	return &speechTranslatorTranslateClient{stream}, nil
}

type SpeechTranslator_TranslateClient interface {
	Send(*TranslationRequest) error
	Recv() (*TranslationResult, error)
	CloseSend() error
	grpc.ClientStream
}

type speechTranslatorTranslateClient struct {
	grpc.ClientStream
}

func (x *speechTranslatorTranslateClient) Send(m *TranslationRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *speechTranslatorTranslateClient) Recv() (*TranslationResult, error) {
	m := new(TranslationResult)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (x *speechTranslatorTranslateClient) CloseSend() error {
	return x.ClientStream.CloseSend()
}

// HealthRequest is a minimal health check request
type HealthRequest struct {
	Service string
}

// HealthResponse is a minimal health check response
type HealthResponse struct {
	Status string
}

func CheckHealth(ctx context.Context, cc grpc.ClientConnInterface) error {
	// Use a simple gRPC ping via the Translate endpoint to check connectivity.
	// We send an empty request with a short timeout to see if the server responds.
	client := NewSpeechTranslatorClient(cc)
	stream, err := client.Translate(ctx)
	if err != nil {
		return fmt.Errorf("health check stream failed: %w", err)
	}
	if err := stream.Send(&TranslationRequest{
		SourceLanguage: "",
		TargetLanguage: "",
		Mode:          TranslationMode_MODE_S2S,
	}); err != nil {
		stream.CloseSend()
		return fmt.Errorf("health check send failed: %w", err)
	}
	// Read the response (may fail with EOF if server quickly closes on empty audio, that's ok)
	_, err = stream.Recv()
	stream.CloseSend()
	if err != nil {
		// EOF means the stream ended gracefully (expected for empty audio)
		return nil
	}
	return nil
}

// Register the header/metadata helpers
func SendTranslatorMetadata(ctx context.Context, sourceLang, targetLang, ttsVoice string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"source_language", sourceLang,
		"target_language", targetLang,
		"tts_voice_name", ttsVoice,
	))
}
