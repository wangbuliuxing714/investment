package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"desk/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	schema := `
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS strategy (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  name TEXT NOT NULL,
  coins TEXT NOT NULL,
  btc_eth_leverage INTEGER NOT NULL,
  alt_leverage INTEGER NOT NULL,
  max_positions INTEGER NOT NULL,
  min_confidence INTEGER NOT NULL,
  position_value_ratio REAL NOT NULL,
  max_margin_usage REAL NOT NULL,
  scan_interval_sec INTEGER NOT NULL,
  prompt TEXT NOT NULL,
  timeframe TEXT NOT NULL,
  kline_count INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS trader (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  running INTEGER NOT NULL,
  started_at TEXT NOT NULL DEFAULT '',
  last_scan_at TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS decisions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL,
  status TEXT NOT NULL,
  reasoning TEXT NOT NULL DEFAULT '',
  raw_response TEXT NOT NULL DEFAULT '',
  input_prompt TEXT NOT NULL DEFAULT '',
  decisions_json TEXT NOT NULL DEFAULT '',
  execution_log TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS agent_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  role TEXT NOT NULL,
  name TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL,
  created_at TEXT NOT NULL
);`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM strategy`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		_, err := s.db.Exec(`INSERT INTO strategy (
			id, name, coins, btc_eth_leverage, alt_leverage, max_positions, min_confidence,
			position_value_ratio, max_margin_usage, scan_interval_sec, prompt, timeframe, kline_count
		) VALUES (1, ?, ?, 5, 3, 2, 75, 1, 0.5, 900, ?, '15m', 30)`,
			"币安默认", `["BTCUSDT","ETHUSDT"]`,
			"只在列出的币安 U 本位永续合约里交易。方向不明确就观望。开仓要写止损和止盈，仓位不要超过策略上限。")
		if err != nil {
			return err
		}
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM trader`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.db.Exec(`INSERT INTO trader (id, running) VALUES (1, 0)`); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES ('binance_testnet', '1') ON CONFLICT(key) DO NOTHING`); err != nil {
		return err
	}
	if err := s.migratePrompts(); err != nil {
		return err
	}
	if err := s.migrateWorkspace(); err != nil {
		return err
	}
	if err := s.migrateInvestPrompt(); err != nil {
		return err
	}
	if err := s.migrateOptimizationInvest(); err != nil {
		return err
	}
	if err := s.migrateSingleInvest(); err != nil {
		return err
	}
	if err := s.migrateModePrompts(); err != nil {
		return err
	}
	if err := s.migrateConversations(); err != nil {
		return err
	}
	return s.migrateNative()
}

func (s *Store) setting(key string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v
}

func (s *Store) setSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) Settings() model.SettingsView {
	key := s.setting("binance_api_key")
	hint := ""
	if len(key) >= 4 {
		hint = key[len(key)-4:]
	}
	return model.SettingsView{
		HasBinanceKey: strings.TrimSpace(key) != "" && strings.TrimSpace(s.setting("binance_api_secret")) != "",
		KeyHint:       hint,
		Testnet:       s.setting("binance_testnet") != "0",
		ModelID:       s.setting("model_id"),
		ModelName:     s.setting("model_name"),
		ProviderID:    s.setting("provider_id"),
		UpstreamModel: s.setting("upstream_model"),
	}
}

func (s *Store) BinanceKeys() (key, secret string, testnet bool) {
	return s.setting("binance_api_key"), s.setting("binance_api_secret"), s.setting("binance_testnet") != "0"
}

func (s *Store) SaveBinance(key, secret string, testnet bool) error {
	key = strings.TrimSpace(key)
	secret = strings.TrimSpace(secret)
	oldKey, oldSecret, _ := s.BinanceKeys()
	if key == "" {
		key = oldKey
	}
	if secret == "" {
		secret = oldSecret
	}
	if key == "" || secret == "" {
		return errors.New("请填写 API Key 和 Secret")
	}
	if err := s.setSetting("binance_api_key", key); err != nil {
		return err
	}
	if err := s.setSetting("binance_api_secret", secret); err != nil {
		return err
	}
	flag := "1"
	if !testnet {
		flag = "0"
	}
	return s.setSetting("binance_testnet", flag)
}

func (s *Store) SaveModel(id, name, providerID, upstream string) error {
	id, name = strings.TrimSpace(id), strings.TrimSpace(name)
	providerID, upstream = strings.TrimSpace(providerID), strings.TrimSpace(upstream)
	if id == "" || name == "" || providerID == "" || upstream == "" {
		return errors.New("请选择一个后台模型")
	}
	for k, v := range map[string]string{
		"model_id":       id,
		"model_name":     name,
		"provider_id":    providerID,
		"upstream_model": upstream,
	} {
		if err := s.setSetting(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Strategy() (model.Strategy, error) {
	var st model.Strategy
	var coins string
	err := s.db.QueryRow(`SELECT name, coins, btc_eth_leverage, alt_leverage, max_positions, min_confidence,
		position_value_ratio, max_margin_usage, scan_interval_sec, prompt, timeframe, kline_count FROM strategy WHERE id = 1`).
		Scan(&st.Name, &coins, &st.BTCETHLeverage, &st.AltLeverage, &st.MaxPositions, &st.MinConfidence,
			&st.PositionValueRatio, &st.MaxMarginUsage, &st.ScanIntervalSec, &st.Prompt, &st.Timeframe, &st.KlineCount)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal([]byte(coins), &st.Coins); err != nil {
		return st, fmt.Errorf("策略币种损坏: %w", err)
	}
	return st, nil
}

func (s *Store) SaveStrategy(st model.Strategy) error {
	coins, err := json.Marshal(st.Coins)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE strategy SET name=?, coins=?, btc_eth_leverage=?, alt_leverage=?, max_positions=?,
		min_confidence=?, position_value_ratio=?, max_margin_usage=?, scan_interval_sec=? WHERE id=1`,
		st.Name, string(coins), st.BTCETHLeverage, st.AltLeverage, st.MaxPositions, st.MinConfidence,
		st.PositionValueRatio, st.MaxMarginUsage, st.ScanIntervalSec)
	return err
}

func (s *Store) Trader() (model.TraderState, error) {
	var st model.TraderState
	var running int
	err := s.db.QueryRow(`SELECT running, started_at, last_scan_at, last_error FROM trader WHERE id=1`).
		Scan(&running, &st.StartedAt, &st.LastScanAt, &st.LastError)
	st.Running = running == 1
	if err == nil {
		var stop struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
			At     string `json:"at"`
		}
		if raw := s.setting("trader_last_stop"); raw != "" && json.Unmarshal([]byte(raw), &stop) == nil {
			st.StopKind, st.StopReason, st.StoppedAt = stop.Kind, stop.Reason, stop.At
		}
	}
	return st, err
}

// Persist the stop and its cause together. Restarting the engine must not
// erase whether this was maintenance, a stop request, or a runtime error.
func (s *Store) SetStopped(kind, reason string) error {
	body, err := json.Marshal(map[string]string{"kind": kind, "reason": reason, "at": time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE trader SET running=0, started_at='' WHERE id=1`); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES('trader_last_stop',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(body)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetRunning(running bool) error {
	flag := 0
	started := ""
	if running {
		flag = 1
		started = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`UPDATE trader SET running=?, started_at=CASE WHEN ?=1 THEN ? ELSE '' END WHERE id=1`, flag, flag, started)
	return err
}

func (s *Store) SetScanResult(when, lastErr string) error {
	_, err := s.db.Exec(`UPDATE trader SET last_scan_at=?, last_error=? WHERE id=1`, when, lastErr)
	return err
}

func (s *Store) ClearLastError() error {
	_, err := s.db.Exec(`UPDATE trader SET last_error='' WHERE id=1`)
	return err
}

func (s *Store) InsertDecision() (int64, error) {
	res, err := s.db.Exec(`INSERT INTO decisions (created_at, status) VALUES (?, 'running')`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishDecision(rec model.DecisionRecord) error {
	_, err := s.db.Exec(`UPDATE decisions SET status=?, reasoning=?, raw_response=?, input_prompt=?, decisions_json=?, execution_log=?, error=? WHERE id=?`,
		rec.Status, rec.Reasoning, rec.RawResponse, rec.InputPrompt, rec.DecisionsJSON, rec.ExecutionLog, rec.Error, rec.ID)
	return err
}

// ChatLogKeep is how many agent lines stay in the local database.
const ChatLogKeep = 500

// ChatRow is one persisted agent transcript line.
type ChatRow struct {
	ID        int64
	Role      string
	Name      string
	Text      string
	CreatedAt string
}

func (s *Store) AppendChat(role, name, body, at string) (int64, error) {
	c, err := s.Conversation()
	if err != nil {
		return 0, err
	}
	return s.AppendConversationChat(c.ID, role, name, body, at)
}

func (s *Store) RecentChat(limit int) ([]ChatRow, error) {
	c, err := s.Conversation()
	if err != nil {
		return nil, err
	}
	return s.ConversationChat(c.ID, limit)
}
func (s *Store) ConversationChat(conversation string, limit int) ([]ChatRow, error) {
	if limit <= 0 || limit > ChatLogKeep {
		limit = ChatLogKeep
	}
	rows, err := s.db.Query(`SELECT id, role, name, body, created_at FROM agent_log WHERE conversation_id=? ORDER BY id DESC LIMIT ?`, conversation, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatRow
	for rows.Next() {
		var row ChatRow
		if err := rows.Scan(&row.ID, &row.Role, &row.Name, &row.Text, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []ChatRow{}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *Store) Decisions(limit int) ([]model.DecisionRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.Query(`SELECT id, created_at, status, reasoning, raw_response, input_prompt, decisions_json, execution_log, error
		FROM decisions ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DecisionRecord
	for rows.Next() {
		var rec model.DecisionRecord
		if err := rows.Scan(&rec.ID, &rec.CreatedAt, &rec.Status, &rec.Reasoning, &rec.RawResponse, &rec.InputPrompt, &rec.DecisionsJSON, &rec.ExecutionLog, &rec.Error); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if out == nil {
		out = []model.DecisionRecord{}
	}
	return out, rows.Err()
}
