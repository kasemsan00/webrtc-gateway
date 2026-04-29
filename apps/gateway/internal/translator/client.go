package translator

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"k2-gateway/internal/translator/pb"
)

type Client struct {
	conn   *grpc.ClientConn
	client pb.SpeechTranslatorClient
	cfg    Config
	mu     sync.RWMutex
	closed bool
}

type Config struct {
	Addr        string
	SourceLang  string
	TargetLang  string
	TTSVoice    string
	OpusBitrate int
}

func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("client is closed")
	}

	conn, err := grpc.DialContext(ctx, c.cfg.Addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return fmt.Errorf("failed to dial translator at %s: %w", c.cfg.Addr, err)
	}

	c.conn = conn
	c.client = pb.NewSpeechTranslatorClient(conn)
	return nil
}

func (c *Client) TranslateStream(ctx context.Context) (pb.SpeechTranslator_TranslateClient, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("translator client not connected")
	}

	stream, err := c.client.Translate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create translate stream: %w", err)
	}
	return stream, nil
}

func (c *Client) CheckHealth(ctx context.Context) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}
	return pb.CheckHealth(ctx, conn)
}

func (c *Client) Send(ctx context.Context, audioData []byte) (*pb.TranslationResult, error) {
	stream, err := c.TranslateStream(ctx)
	if err != nil {
		return nil, err
	}

	req := &pb.TranslationRequest{
		SourceLanguage: c.cfg.SourceLang,
		TargetLanguage: c.cfg.TargetLang,
		ReturnAudio:    true,
		TTSVoiceName:   c.cfg.TTSVoice,
		AudioData:      audioData,
		Mode:           pb.TranslationMode_MODE_S2S,
	}

	if err := stream.Send(req); err != nil {
		stream.CloseSend()
		return nil, fmt.Errorf("failed to send translation request: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		stream.CloseSend()
		if err == io.EOF {
			return nil, fmt.Errorf("stream closed by server with no result")
		}
		return nil, fmt.Errorf("failed to receive translation result: %w", err)
	}
	stream.CloseSend()
	return resp, nil
}

func (c *Client) Config() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
