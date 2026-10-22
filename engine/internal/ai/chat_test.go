package ai

import (
	"strings"
	"testing"
)

func TestParseNativeProtocolIntegrity(t *testing.T) {
	for _, raw := range []string{
		`{"choices":[{"finish_reason":"length","message":{"content":"truncated","tool_calls":[{"id":"x","function":{"name":"write_file","arguments":"{}"}}]}}]}`,
		`{"choices":[{"message":{"content":"","tool_calls":[{"function":{"name":"write_file","arguments":"{}"}}]}}]}`,
		`{"choices":[{"message":{"content":"","tool_calls":[{"id":"x","function":{"name":"read_file","arguments":"{}"}},{"id":"x","function":{"name":"read_file","arguments":"{}"}}]}}]}`,
		`{"choices":[{"message":{"content":null}}]}`,
	} {
		if _, err := ParseTurn([]byte(raw)); err == nil {
			t.Fatal("accepted incomplete protocol", raw)
		}
	}
	turn, err := ParseTurn([]byte(`{"choices":[{"message":{"content":"","reasoning_content":"provider-required-state","tool_calls":[{"id":"x","function":{"name":"read_file","arguments":"{}"}}]}}]}`))
	if err != nil || !strings.Contains(string(turn.Raw), `"reasoning_content":"provider-required-state"`) {
		t.Fatal("lost provider replay state", err)
	}
}

func TestParseTurnToolCall(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"content":"先看账户","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_account","arguments":"{}"}}]}}]}`)
	turn, err := ParseTurn(raw)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Content != "先看账户" || len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "get_account" {
		t.Fatalf("%+v", turn)
	}
}

func TestToolError(t *testing.T) {
	if !toolError(errString("tools is not supported")) {
		t.Fatal("expected tool error")
	}
	if toolError(errString("模型接口连不上")) {
		t.Fatal("network error is not a tool error")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
