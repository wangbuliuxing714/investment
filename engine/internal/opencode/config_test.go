package opencode

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestChatRoot(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"https://api.example/v1/chat/completions", "https://api.example/v1"},
		{"https://api.example/v1/chat/completions/", "https://api.example/v1"},
		{"https://api.example/chat/completions", "https://api.example"},
		{"https://api.example/v1", "https://api.example/v1"},
	}
	for _, tc := range cases {
		got, err := ChatRoot(tc.in)
		if err != nil || got != tc.out {
			t.Fatalf("%s -> %s (%v), want %s", tc.in, got, err, tc.out)
		}
	}
	if _, err := ChatRoot("http://api.example/v1"); err == nil {
		t.Fatal("http should be rejected")
	}
}

func TestBuildConfig(t *testing.T) {
	body, err := BuildConfig(Input{
		APIURL: "https://api.example/v1/chat/completions",
		APIKey: "test-key",
		Model:  "org/model-a",
		Name:   "显示名",
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuildConfig(Input{
		APIURL: "https://api.example/v1/chat/completions",
		APIKey: "test-key",
		Model:  "org/model-a",
		Name:   "显示名",
	})
	if err != nil || string(body) != string(again) {
		t.Fatal("config bytes changed between builds")
	}
	var cfg fileConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultAgent != "desk" || cfg.Agents["desk"].Steps != 1 {
		t.Fatalf("agent: %+v", cfg.Agents["desk"])
	}
	if cfg.Agents["desk"].System != "" {
		t.Fatal("empty input silently replaced with default role")
	}
	if cfg.Permissions[0].Effect != "deny" || cfg.Agents["desk"].Permissions[0].Effect != "deny" {
		t.Fatal("permissions are not deny")
	}
	provider := cfg.Providers["desk"]
	if provider.Package != providerPkg {
		t.Fatal(provider.Package)
	}
	if provider.Settings["baseURL"] != "https://api.example/v1" {
		t.Fatal(provider.Settings["baseURL"])
	}
	if provider.Settings["apiKey"] != "test-key" {
		t.Fatal("api key was not stored")
	}
	if strings.Contains(provider.Settings["baseURL"], "chat/completions") {
		t.Fatal("base URL still has the chat path")
	}
	model, ok := provider.Models["org/model-a"]
	if !ok || model.Name != "显示名" || model.Capabilities.Tools {
		t.Fatalf("model: %+v ok=%v", model, ok)
	}
	if _, err := BuildConfig(Input{APIURL: "https://api.example/v1", APIKey: "k", Model: "a#b"}); err == nil {
		t.Fatal("hash in model id should fail")
	}
}

func TestPassphraseFromJSON(t *testing.T) {
	body := []byte(`{"mcpServers":{"ssh":{"env":{"SSH_MCP_PASSPHRASE":"local-secret"}}}}`)
	if got := passphraseFromJSON(body); got != "local-secret" {
		t.Fatal(got)
	}
	if got := passphraseFromJSON([]byte(`{"other":1}`)); got != "" {
		t.Fatal(got)
	}
}

func TestManagedSystemPromptChangesRemoteConfigWithoutChangingPermissions(t *testing.T) {
	in := Input{APIURL: "https://api.example/v1", APIKey: "test-key", Model: "test-model", SystemPrompt: "my managed system"}
	body, err := BuildConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	var cfg fileConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Agents["desk"].System != in.SystemPrompt {
		t.Fatal("custom system not installed")
	}
	if cfg.Permissions[0].Effect != "deny" || cfg.Agents["desk"].Permissions[0].Effect != "deny" {
		t.Fatal("permissions changed")
	}
	in.SystemPrompt = "changed system"
	next, _ := BuildConfig(in)
	if hashBytes(body) == hashBytes(next) {
		t.Fatal("system changes must trigger remote configuration refresh")
	}
}

func TestReplies(t *testing.T) {
	id, err := sessionID([]byte(`{"data":{"id":"ses_123"}}`))
	if err != nil || id != "ses_123" {
		t.Fatal(id, err)
	}
	text, err := generatedText([]byte(`{"data":{"text":"先看，再决定"}}`))
	if err != nil || text != "先看，再决定" {
		t.Fatal(text, err)
	}
	text, err = generatedText([]byte(`{"location":{},"data":{"data":{"text":"内层"}}}`))
	if err != nil || text != "内层" {
		t.Fatal(text, err)
	}
	if _, err := generatedText([]byte(`{"data":{}}`)); err == nil {
		t.Fatal("empty generate should fail")
	}
}

func TestUnavailable(t *testing.T) {
	err := down("SSH 没有连上服务器")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err.Error() != "SSH 没有连上服务器" {
		t.Fatal(err.Error())
	}
	plain := errors.New("模型回复无法解析，本轮不下单")
	if errors.Is(plain, ErrUnavailable) {
		t.Fatal("parse errors must not fall back")
	}
}
