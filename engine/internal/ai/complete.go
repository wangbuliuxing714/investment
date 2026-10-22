package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Delta func(text string)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
}

// Complete calls an OpenAI-compatible chat endpoint and streams text chunks.
func Complete(ctx context.Context, apiURL, apiKey, modelName, system, user string, onDelta Delta) (string, error) {
	text, err := complete(ctx, apiURL, apiKey, modelName, system, user, true, onDelta)
	if err == nil {
		return text, nil
	}
	if !errors.Is(err, errStream) && !strings.Contains(err.Error(), "stream") {
		if strings.Contains(strings.ToLower(err.Error()), "temperature") {
			return complete(ctx, apiURL, apiKey, modelName, system, user, false, onDelta)
		}
		return "", err
	}
	return complete(ctx, apiURL, apiKey, modelName, system, user, false, onDelta)
}

var errStream = errors.New("stream unavailable")

func complete(ctx context.Context, apiURL, apiKey, modelName, system, user string, stream bool, onDelta Delta) (string, error) {
	temp := 0.2
	body, err := json.Marshal(request{
		Model: modelName,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: &temp,
		MaxTokens:   4096,
		Stream:      stream,
	})
	if err != nil {
		return "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	client := &http.Client{Timeout: 160 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("模型接口连不上")
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4000))
		return "", cleanHTTPError(res.StatusCode, msg)
	}
	if !stream {
		return readJSON(res.Body, onDelta)
	}
	text, err := readSSE(res.Body, onDelta)
	if err != nil {
		return text, err
	}
	if strings.TrimSpace(text) == "" {
		return "", errStream
	}
	return text, nil
}

func readJSON(r io.Reader, onDelta Delta) (string, error) {
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 2<<20)).Decode(&payload); err != nil {
		return "", errors.New("模型返回的内容无法解析")
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return "", errors.New(payload.Error.Message)
	}
	if len(payload.Choices) == 0 {
		return "", errors.New("模型没有返回内容")
	}
	text := payload.Choices[0].Message.Content
	if onDelta != nil && text != "" {
		onDelta(text)
	}
	return text, nil
}

func readSSE(r io.Reader, onDelta Delta) (string, error) {
	sc := bufio.NewScanner(io.LimitReader(r, 4<<20))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var b strings.Builder
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return b.String(), errors.New(chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		piece := chunk.Choices[0].Delta.Content
		if piece == "" {
			piece = chunk.Choices[0].Message.Content
		}
		if piece == "" {
			continue
		}
		b.WriteString(piece)
		if onDelta != nil {
			onDelta(piece)
		}
	}
	if err := sc.Err(); err != nil {
		return b.String(), err
	}
	return b.String(), nil
}

func cleanHTTPError(status int, body []byte) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &payload)
	msg := payload.Error.Message
	if msg == "" {
		msg = payload.Message
	}
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", status)
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "api key") || strings.Contains(lower, "unauthorized") || status == http.StatusUnauthorized {
		return fmt.Errorf("模型通道拒绝了密钥（%s）", trim(msg))
	}
	return errors.New(trim(msg))
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		return s[:300]
	}
	return s
}
