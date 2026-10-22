package store

import (
	"desk/internal/prompts"
	"encoding/json"
	"strings"
	"time"
)

// Move existing instructions and back up the exact records atomically. Once
// mode fields exist, reopening never restores templates or rewrites user edits.
func (s *Store) migrateModePrompts() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow("SELECT value FROM settings WHERE key='model_prompts'").Scan(&raw); err != nil {
		return err
	}
	var c prompts.Config
	if err = json.Unmarshal([]byte(raw), &c); err != nil {
		return err
	}
	if _, ok := c["optimization"]; ok {
		return prompts.Validate(c)
	}
	var f WorkspaceFile
	if err = tx.QueryRow("SELECT path,directory,content,revision,updated_at FROM workspace_files WHERE path=?", prompts.InvestPath).Scan(&f.Path, &f.Directory, &f.Content, &f.Revision, &f.UpdatedAt); err != nil {
		return err
	}
	strategy, common, trading, research := prompts.SeparateInstructions(f.Content)
	common = strings.NewReplacer("改本文件", "改 INVEST.MD", "本文件每次请求", "INVEST.MD 每次请求", "研究草案和相关工作规则", "研究草案").Replace(common)
	common = strings.ReplaceAll(common, "先用简短中文说明，再用 tool JSON 代码块调用：`{\"name\":\"工具名\",\"arguments\":{}}`。独立查询可一起发，需要上次结果的就按顺序。", "先用简短中文说明，再通过平台工具接口调用。独立查询可一起发，需要上次结果的就按顺序。文字和代码示例不算实际调用。")
	research = strings.ReplaceAll(research, "再改本节", "再改 INVEST.MD 中的研究草案")
	next := prompts.Defaults()
	if c["system"] != prompts.RoleDefault {
		next["system"] = c["system"] + "\n\n" + next["system"]
	}
	for key, text := range map[string]string{"system": common, "trading": trading, "optimization": research} {
		if text != "" {
			next[key] += "\n\n" + text
		}
	}
	if text, ok := c["chat"]; ok {
		next["chat"] = text
	}
	if text, ok := c["resume"]; ok && text != prompts.InvestDefaults()["resume"] {
		next["resume"] = text
	}
	if strings.TrimSpace(strategy) == "" {
		strategy = "# INVEST.MD\n\n尚未定义策略。"
	}
	if err = prompts.Validate(next); err != nil {
		return err
	}
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	backup, err := json.Marshal(f)
	if err != nil {
		return err
	}
	for key, value := range map[string]string{"model_prompts_before_modes_v1": raw, "invest_before_modes_v1": string(backup)} {
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO NOTHING", key, value); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(body)); err != nil {
		return err
	}
	if strategy != strings.TrimSpace(f.Content) {
		if _, err = tx.Exec("UPDATE workspace_files SET content=?,revision=revision+1,updated_at=? WHERE path=?", strategy, time.Now().UTC().Format(time.RFC3339Nano), prompts.InvestPath); err != nil {
			return err
		}
	}
	return tx.Commit()
}
