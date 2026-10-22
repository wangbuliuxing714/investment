package store

import (
	"desk/internal/agenttools"
	"desk/internal/prompts"
	"encoding/json"
	"strings"
)

func (s *Store) unifySystemPrompts() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw, toolsRaw string
	if err = tx.QueryRow("SELECT value FROM settings WHERE key='model_prompts'").Scan(&raw); err != nil {
		return err
	}
	var c prompts.Config
	if err = json.Unmarshal([]byte(raw), &c); err != nil {
		return err
	}
	if len(c) == len(prompts.InvestDefinitions) || len(c) == len(prompts.UnifiedDefinitions) {
		return prompts.ValidateStored(c)
	}
	if err = prompts.ValidateStored(c); err != nil {
		return err
	}
	if err = tx.QueryRow("SELECT value FROM settings WHERE key='agent_tools'").Scan(&toolsRaw); err != nil {
		return err
	}
	var tools agenttools.Config
	if err = json.Unmarshal([]byte(toolsRaw), &tools); err != nil {
		return err
	}
	next, protocol := prompts.Consolidate(c)
	if protocol != "" && !strings.Contains(tools.Instructions, protocol) {
		tools.Instructions += "\n\n" + protocol
	}
	if err = prompts.ValidateStored(next); err != nil {
		return err
	}
	if err = agenttools.Validate(tools); err != nil {
		return err
	}
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	toolBody, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	for key, value := range map[string]string{"model_prompts_before_unified_v3": raw, "agent_tools_before_unified_v3": toolsRaw} {
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO NOTHING", key, value); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(body)); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE settings SET value=? WHERE key='agent_tools'", string(toolBody)); err != nil {
		return err
	}
	return tx.Commit()
}
