package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func FromEnv() *Client {
	home, _ := os.UserHomeDir()
	key := strings.TrimSpace(os.Getenv("DESK_SSH_KEY"))
	if key == "" {
		key = filepath.Join(home, ".ssh", "daizong_server_ed25519")
	}
	return &Client{
		Host:       getenv("DESK_SSH_HOST", "47.96.102.91"),
		User:       getenv("DESK_SSH_USER", "root"),
		KeyPath:    key,
		Passphrase: passphrase(),
		Local:      getenv("DESK_OPENCODE_LOCAL", "127.0.0.1:18793"),
		Remote:     getenv("DESK_OPENCODE_REMOTE", "127.0.0.1:8793"),
		Directory:  workspaceDir,
		ConfigPath: configPath,
		Unit:       unitName,
	}
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DESK_OPENCODE"))) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

func passphrase() string {
	if v := os.Getenv("DESK_SSH_PASSPHRASE"); strings.TrimSpace(v) != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	if err != nil {
		return ""
	}
	return passphraseFromJSON(body)
}

func passphraseFromJSON(body []byte) string {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	return findString(v, "SSH_MCP_PASSPHRASE")
}

func findString(v any, key string) string {
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			if k == key {
				if s, ok := child.(string); ok {
					return s
				}
			}
			if s := findString(child, key); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range node {
			if s := findString(child, key); s != "" {
				return s
			}
		}
	}
	return ""
}
