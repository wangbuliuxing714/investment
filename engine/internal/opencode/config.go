package opencode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

const (
	providerID   = "desk"
	providerPkg  = "@opencode-ai/ai/providers/openai-compatible"
	workspaceDir = "/var/lib/opencode-desk/workspace"
	configPath   = "/var/lib/opencode-desk/config/opencode.json"
	passwordFile = "/etc/opencode-desk.env"
	unitName     = "opencode-desk"
)

// Input is one trading question for the desk OpenCode process.
type Input struct {
	ConversationID string
	APIURL         string
	APIKey         string
	Model          string
	Name           string
	Prompt         string
	SystemPrompt   string
}

type rule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

type agentSpec struct {
	Mode        string `json:"mode"`
	Steps       int    `json:"steps"`
	Description string `json:"description"`
	System      string `json:"system"`
	Permissions []rule `json:"permissions"`
}

type capabilities struct {
	Tools  bool     `json:"tools"`
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type modelSpec struct {
	Name         string       `json:"name"`
	Capabilities capabilities `json:"capabilities"`
}

type providerSpec struct {
	Name     string               `json:"name"`
	Package  string               `json:"package"`
	Settings map[string]string    `json:"settings"`
	Models   map[string]modelSpec `json:"models"`
}

type fileConfig struct {
	DefaultAgent string                  `json:"default_agent"`
	Permissions  []rule                  `json:"permissions"`
	Agents       map[string]agentSpec    `json:"agents"`
	Providers    map[string]providerSpec `json:"providers"`
}

func denyAll() []rule {
	return []rule{{Action: "*", Resource: "*", Effect: "deny"}}
}

// ChatRoot strips the chat completions suffix so OpenCode receives the API root.
func ChatRoot(apiURL string) (string, error) {
	raw := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	lower := strings.ToLower(raw)
	for _, suffix := range []string{"/chat/completions"} {
		if strings.HasSuffix(lower, suffix) {
			raw = raw[:len(raw)-len(suffix)]
			raw = strings.TrimRight(raw, "/")
			break
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("模型通道地址无效")
	}
	return raw, nil
}

// BuildConfig writes the desk OpenCode config. The bytes include the provider key.
func BuildConfig(in Input) ([]byte, error) {
	modelID := strings.TrimSpace(in.Model)
	if modelID == "" {
		return nil, errors.New("没有上游模型名称")
	}
	if strings.Contains(modelID, "#") {
		return nil, errors.New("模型名称里不能有 #")
	}
	apiKey := strings.TrimSpace(in.APIKey)
	if apiKey == "" {
		return nil, errors.New("模型通道没有密钥")
	}
	root, err := ChatRoot(in.APIURL)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = modelID
	}
	system := in.SystemPrompt
	cfg := fileConfig{
		DefaultAgent: "desk",
		Permissions:  denyAll(),
		Agents: map[string]agentSpec{
			"desk": {
				Mode:        "primary",
				Steps:       1,
				Description: "交易台代理",
				System:      system,
				Permissions: denyAll(),
			},
		},
		Providers: map[string]providerSpec{
			providerID: {
				Name:    "交易台",
				Package: providerPkg,
				Settings: map[string]string{
					"apiKey":  apiKey,
					"baseURL": root,
				},
				Models: map[string]modelSpec{
					modelID: {
						Name: name,
						Capabilities: capabilities{
							Tools:  false,
							Input:  []string{"text"},
							Output: []string{"text"},
						},
					},
				},
			},
		},
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
