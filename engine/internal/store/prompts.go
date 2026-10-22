package store

import (
	"database/sql"
	"desk/internal/prompts"
	"encoding/json"
)

func (s *Store) migratePrompts() error {
	c := prompts.LegacyDefaults()
	var cStrategyPrompt string
	// Preserve even an intentionally blank legacy prompt. Never overwrite a saved config.
	if err := s.db.QueryRow("SELECT prompt FROM strategy WHERE id=1").Scan(&cStrategyPrompt); err != nil {
		return err
	}
	c["strategy"] = cStrategyPrompt
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO settings (key, value) VALUES ('model_prompts', ?) ON CONFLICT(key) DO NOTHING", string(body))
	if err != nil {
		return err
	}
	if err = s.splitToolSettings(); err != nil {
		return err
	}
	if err = s.upgradeAnalyticsTools(); err != nil {
		return err
	}
	if err = s.upgradeResearchSourceTools(); err != nil {
		return err
	}
	if err = s.structurePromptDefaultsV2(); err != nil {
		return err
	}
	return s.unifySystemPrompts()
}

// Upgrade stock templates once, with the original document backed up in the same transaction.
func (s *Store) structurePromptDefaultsV2() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var marker string
	err = tx.QueryRow("SELECT value FROM settings WHERE key='model_prompts_structure_v2'").Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	var raw string
	if err = tx.QueryRow("SELECT value FROM settings WHERE key='model_prompts'").Scan(&raw); err != nil {
		return err
	}
	var c prompts.Config
	if err = json.Unmarshal([]byte(raw), &c); err != nil {
		return err
	}
	if err = prompts.ValidateStored(c); err != nil {
		return err
	}
	if next, changed := prompts.UpgradeStockDefaultsV2(c); changed {
		body, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO settings (key, value) VALUES ('model_prompts_before_structure_v2', ?) ON CONFLICT(key) DO NOTHING", raw); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE settings SET value=? WHERE key='model_prompts'", string(body)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO settings (key, value) VALUES ('model_prompts_structure_v2', 'applied')"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Prompts() (prompts.Config, error) {
	var raw string
	if err := s.db.QueryRow("SELECT value FROM settings WHERE key='model_prompts'").Scan(&raw); err != nil {
		return nil, err
	}
	var c prompts.Config
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	if err := prompts.Validate(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) SavePrompts(c prompts.Config) error {
	if err := prompts.Validate(c); err != nil {
		return err
	}
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.setSetting("model_prompts", string(body))
}
