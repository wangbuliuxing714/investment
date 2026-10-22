package trader

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"desk/internal/store"
)

func (a *agentState) fileTool(name, args string) string {
	if a.service == nil || a.service.store == nil {
		return `{"error":"文件工作区不可用"}`
	}
	result, err := a.service.store.FileCommand(name, args)
	if err != nil {
		result = map[string]string{"error": err.Error()}
	}
	if file, ok := result.(store.WorkspaceFile); ok && !file.Directory {
		// Hash the exact UTF-8 content returned by both write and read, without
		// changing the stored document or revision semantics.
		result = struct {
			store.WorkspaceFile
			ContentBytes int    `json:"contentBytes"`
			SHA256       string `json:"sha256"`
			Complete     bool   `json:"complete"`
		}{file, len(file.Content), fmt.Sprintf("%x", sha256.Sum256([]byte(file.Content))), true}
	}
	body, _ := json.Marshal(result)
	return string(body)
}
