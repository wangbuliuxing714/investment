package store

import (
	"errors"
	"strings"

	"desk/internal/freqtrade"
)

// ConfiguredBinance preserves the original configurator/local connection.
func (s *Store) ConfiguredBinance() freqtrade.Creds {
	key, secret, demo := s.BinanceKeys()
	return freqtrade.Apply(key, secret, demo)
}

func (s *Store) demoSettings() (connection, key, secret string) {
	err := s.db.QueryRow(`SELECT
 COALESCE(MAX(CASE WHEN key='binance_connection' THEN value END), 'configured'),
 COALESCE(MAX(CASE WHEN key='binance_demo_api_key' THEN value END), ''),
 COALESCE(MAX(CASE WHEN key='binance_demo_api_secret' THEN value END), '')
 FROM settings WHERE key IN ('binance_connection','binance_demo_api_key','binance_demo_api_secret')`).Scan(&connection, &key, &secret)
	if err != nil {
		// A failed read must not silently enable production trading.
		return "demo", "", ""
	}
	return connection, key, secret
}

func (s *Store) BinanceConnection() string {
	connection, _, _ := s.demoSettings()
	return connection
}

func (s *Store) DemoBinanceKeys() (string, string) {
	_, key, secret := s.demoSettings()
	return key, secret
}

// BinanceCredentials is shared by account reads, manual orders and both agent
// paths. Explicit demo selection never falls back to configurator/live keys.
func (s *Store) BinanceCredentials() freqtrade.Creds {
	connection, key, secret := s.demoSettings()
	switch connection {
	case "configured":
		return s.ConfiguredBinance()
	case "demo":
		return freqtrade.Creds{Key: key, Secret: secret, Demo: true}
	default:
		return freqtrade.Creds{Demo: true}
	}
}

// SaveBinanceConnection commits the demo credential pair and selection together.
// The existing configurator file and local production credentials are untouched.
func (s *Store) SaveBinanceConnection(connection, key, secret string) error {
	if connection != "configured" && connection != "demo" {
		return errors.New("请选择原配置或币安模拟盘")
	}
	key, secret = strings.TrimSpace(key), strings.TrimSpace(secret)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if connection == "demo" {
		var oldKey, oldSecret string
		if err := tx.QueryRow(`SELECT
 COALESCE(MAX(CASE WHEN key='binance_demo_api_key' THEN value END), ''),
 COALESCE(MAX(CASE WHEN key='binance_demo_api_secret' THEN value END), '')
 FROM settings WHERE key IN ('binance_demo_api_key','binance_demo_api_secret')`).Scan(&oldKey, &oldSecret); err != nil {
			return err
		}
		if key != "" && key != oldKey && secret == "" {
			return errors.New("更换模拟盘 API Key 时，请同时填写对应的 Secret")
		}
		if key == "" {
			key = oldKey
		}
		if secret == "" {
			secret = oldSecret
		}
		if key == "" || secret == "" {
			return errors.New("请填写币安模拟盘专用的 API Key 和 Secret")
		}
		for name, value := range map[string]string{"binance_demo_api_key": key, "binance_demo_api_secret": secret} {
			if _, err := tx.Exec(`INSERT INTO settings (key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, name, value); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings (key,value) VALUES ('binance_connection',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, connection); err != nil {
		return err
	}
	return tx.Commit()
}
