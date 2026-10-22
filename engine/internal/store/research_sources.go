package store

import (
	"desk/internal/agenttools"
	"encoding/json"
)

// Only upgrade complete known stock guides; never replace customized prompts.
func (s *Store) upgradeResearchSourceTools() error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var raw string
	if e = tx.QueryRow("SELECT value FROM settings WHERE key='agent_tools'").Scan(&raw); e != nil {
		return e
	}
	var cfg agenttools.Config
	if e = json.Unmarshal([]byte(raw), &cfg); e != nil {
		return e
	}
	old := agenttools.LegacyInstructions + "\n\n" + agenttools.LegacyAnalyticsInstructions
	if cfg.Instructions != old && cfg.Instructions != old+"\n\n"+agenttools.DecisionInstructions {
		return nil
	}
	if _, e = tx.Exec("INSERT INTO settings(key,value) VALUES('agent_tools_before_research_sources',?) ON CONFLICT(key) DO NOTHING", raw); e != nil {
		return e
	}
	cfg.Instructions = agenttools.DefaultInstructions
	b, e := json.Marshal(cfg)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE settings SET value=? WHERE key='agent_tools'", string(b)); e != nil {
		return e
	}
	return tx.Commit()
}
