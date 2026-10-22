package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrToolsUnsupported means this model channel cannot accept tool calls.
var ErrToolsUnsupported = errors.New("模型通道不支持工具调用")

// ToolCall is one function call from the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Turn is one assistant step in a tool loop.
type Turn struct {
	Content   string
	ToolCalls []ToolCall
	Raw       json.RawMessage
}

// Round sends the current message list and tool definitions, without streaming.
func Round(ctx context.Context, apiURL, apiKey, modelName string, messages []json.RawMessage, tools []any) (Turn, error) {
	turn, err := round(ctx, apiURL, apiKey, modelName, messages, tools, true)
	if err == nil || errors.Is(err, ErrToolsUnsupported) {
		return turn, err
	}
	if strings.Contains(strings.ToLower(err.Error()), "temperature") {
		return round(ctx, apiURL, apiKey, modelName, messages, tools, false)
	}
	return turn, err
}

func round(ctx context.Context, apiURL, apiKey, modelName string, messages []json.RawMessage, tools []any, withTemp bool) (Turn, error) {
	payload := map[string]any{
		"model":    modelName,
		"messages": messages,
		"tools":    tools,
		"stream":   false,
	}
	if withTemp {
		payload["temperature"] = 0.2
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Turn{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return Turn{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 160 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return Turn{}, errors.New("模型接口连不上")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode >= 400 {
		err = cleanHTTPError(res.StatusCode, raw)
		if toolError(err) {
			return Turn{}, ErrToolsUnsupported
		}
		return Turn{}, err
	}
	turn, err := ParseTurn(raw)
	if err != nil {
		return Turn{}, err
	}
	return turn, nil
}

// ParseTurn reads an OpenAI-compatible chat response.
func ParseTurn(raw []byte) (Turn, error) {
	var payload struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Refusal          string          `json:"refusal"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
				Content          json.RawMessage `json:"content"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Turn{}, errors.New("模型返回的内容无法解析")
	}
	if payload.Error != nil && payload.Error.Message != "" {
		err := errors.New(payload.Error.Message)
		if toolError(err) {
			return Turn{}, ErrToolsUnsupported
		}
		return Turn{}, err
	}
	if len(payload.Choices) == 0 {
		return Turn{}, errors.New("模型没有返回内容")
	}
	choice := payload.Choices[0]
	if choice.FinishReason == "length" || choice.FinishReason == "content_filter" {
		return Turn{}, errors.New("模型回复被截断或拦截，本轮工具未执行")
	}
	msg := choice.Message
	if msg.Refusal != "" {
		return Turn{}, errors.New("模型拒绝了本轮请求，工具未执行")
	}
	turn := Turn{Content: contentString(msg.Content)}
	ids := map[string]bool{}
	for _, call := range msg.ToolCalls {
		id := call.ID
		if id == "" || ids[id] || call.Function.Name == "" {
			return Turn{}, errors.New("原生工具调用缺少唯一ID或名称，本轮工具未执行")
		}
		ids[id] = true
		turn.ToolCalls = append(turn.ToolCalls, ToolCall{
			ID:        id,
			Name:      call.Function.Name,
			Arguments: argsString(call.Function.Arguments),
		})
	}
	if len(turn.ToolCalls) == 0 && strings.TrimSpace(turn.Content) == "" {
		return Turn{}, errors.New("模型返回空回复，本轮工具未执行")
	}
	message := map[string]any{
		"role":       "assistant",
		"content":    turn.Content,
		"tool_calls": encodeCalls(turn.ToolCalls),
	}
	// DeepSeek thinking channels require this field on assistant tool turns.
	if len(msg.ReasoningContent) > 0 && string(msg.ReasoningContent) != "null" {
		message["reasoning_content"] = msg.ReasoningContent
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return Turn{}, err
	}
	if len(turn.ToolCalls) == 0 {
		encoded, err = json.Marshal(map[string]any{"role": "assistant", "content": turn.Content})
		if err != nil {
			return Turn{}, err
		}
	}
	turn.Raw = encoded
	return turn, nil
}

func encodeCalls(calls []ToolCall) []any {
	out := make([]any, 0, len(calls))
	for _, call := range calls {
		out = append(out, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}
	return out
}

func contentString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return strings.Trim(string(raw), `"`)
}

func argsString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

func toolError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	feature := strings.Contains(text, "tool") || strings.Contains(text, "function call") || strings.Contains(text, "functions")
	return feature && (strings.Contains(text, "not support") || strings.Contains(text, "unsupported") || strings.Contains(text, "not allowed") || strings.Contains(text, "unknown parameter"))
}

// Message builds a JSON chat message.
func Message(role, content string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"role": role, "content": content})
	return raw
}

// ToolResult builds a tool response message.
func ToolResult(id, content string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"role": "tool", "tool_call_id": id, "content": content})
	return raw
}
