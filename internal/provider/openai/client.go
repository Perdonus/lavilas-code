package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/runtime"
)

type Client struct {
	cfg        Config
	httpClient *http.Client
	endpoint   string
}

func NewClient(cfg Config) (*Client, error) {
	normalized := cfg.withDefaults()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	return &Client{
		cfg:        normalized,
		httpClient: normalized.HTTPClientOrDefault(),
		endpoint:   normalized.Endpoint(),
	}, nil
}

func (c *Client) Name() string {
	return c.cfg.Name
}

func (c *Client) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Streaming: true,
		Tools:     true,
		Images:    true,
		Audio:     true,
	}
}

func (c *Client) Create(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	payload, err := requestFromRuntime(req, c.cfg, false)
	if err != nil {
		return nil, err
	}

	httpReq, err := c.newRequest(ctx, payload, "application/json")
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	var completion ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&completion); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return responseToRuntime(c.Name(), completion)
}

func (c *Client) Stream(ctx context.Context, req runtime.Request) (runtime.Stream, error) {
	payload, err := requestFromRuntime(req, c.cfg, true)
	if err != nil {
		return nil, err
	}

	httpReq, err := c.newRequest(ctx, payload, "text/event-stream")
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if err := c.checkResponse(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}

	return &streamReader{
		body:   resp.Body,
		reader: bufio.NewReader(resp.Body),
	}, nil
}

func (c *Client) newRequest(ctx context.Context, payload ChatCompletionRequest, accept string) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, values := range c.cfg.BuildHeaders(accept) {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	return request, nil
}

func (c *Client) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	trimmed := strings.TrimSpace(string(body))

	var parsed ErrorResponse
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return &provider.Error{
			Provider:   c.Name(),
			StatusCode: resp.StatusCode,
			Code:       parsed.Error.Code,
			Message:    parsed.Error.Message,
			Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		}
	}
	if trimmed == "" {
		trimmed = resp.Status
	}
	return &provider.Error{
		Provider:   c.Name(),
		StatusCode: resp.StatusCode,
		Message:    trimmed,
		Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
	}
}

type streamReader struct {
	body    io.ReadCloser
	reader  *bufio.Reader
	pending []runtime.StreamEvent
	done    bool
}

func (s *streamReader) Recv() (runtime.StreamEvent, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	if s.done {
		return runtime.StreamEvent{}, io.EOF
	}

	for {
		payload, err := readSSEPayload(s.reader)
		if err != nil {
			return runtime.StreamEvent{}, err
		}
		if len(payload) == 0 {
			continue
		}
		if bytes.Equal(payload, []byte("[DONE]")) {
			s.done = true
			return runtime.StreamEvent{Type: runtime.StreamEventTypeDone}, nil
		}

		var chunk ChatCompletionChunk
		if err := json.Unmarshal(payload, &chunk); err != nil {
			return runtime.StreamEvent{}, fmt.Errorf("decode stream chunk: %w", err)
		}
		s.pending = chunkToRuntimeEvents(chunk)
		if len(s.pending) == 0 {
			continue
		}
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
}

func (s *streamReader) Close() error {
	if s.body == nil {
		return nil
	}
	err := s.body.Close()
	s.body = nil
	return err
}

func readSSEPayload(reader *bufio.Reader) ([]byte, error) {
	var data bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}

		if trimmed == "" {
			if data.Len() == 0 {
				if err == io.EOF {
					return nil, io.EOF
				}
				continue
			}
			return bytes.TrimSpace(data.Bytes()), nil
		}

		if err == io.EOF {
			if data.Len() == 0 {
				return nil, io.EOF
			}
			return bytes.TrimSpace(data.Bytes()), nil
		}
	}
}
