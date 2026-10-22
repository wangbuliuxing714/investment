package store

import (
	"desk/internal/agenttools"
	"desk/internal/prompts"
	"encoding/json"
)

// splitToolSettings moves the original instructions verbatim. Both sides and
// the backup commit together; an existing independent configuration takes precedence.
func (s *Store) splitToolSettings() error {
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
	tools := agenttools.Defaults()
	original, moved := c["tools"]
	if moved {
		tools.Instructions = original
		delete(c, "tools")
	}
	if err = prompts.ValidateStored(c); err != nil {
		return err
	}
	if err = agenttools.Validate(tools); err != nil {
		return err
	}
	body, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO settings (key,value) VALUES ('agent_tools',?) ON CONFLICT(key) DO NOTHING", string(body)); err != nil {
		return err
	}
	if moved {
		if _, err = tx.Exec("INSERT INTO settings (key,value) VALUES ('model_prompts_before_tools_split',?) ON CONFLICT(key) DO NOTHING", raw); err != nil {
			return err
		}
		body, err = json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(body)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Tools() (agenttools.Config, error) {
	var raw string
	if err := s.db.QueryRow("SELECT value FROM settings WHERE key='agent_tools'").Scan(&raw); err != nil {
		return agenttools.Config{}, err
	}
	var c agenttools.Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, err
	}
	return c, agenttools.Validate(c)
}

func (s *Store) SaveTools(c agenttools.Config) error {
	if err := agenttools.Validate(c); err != nil {
		return err
	}
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.setSetting("agent_tools", string(body))
}
