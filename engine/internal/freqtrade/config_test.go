package freqtrade

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyUsesConfiguratorFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freqtrade", "user_data", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"dry_run":true,"trading_mode":"spot","exchange":{"name":"binance","key":"abc12345","secret":"secret999"}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DESK_FREQTRADE_CONFIG", path)

	got := Apply("local-key", "local-secret", true)
	if got.Key != "abc12345" || got.Secret != "secret999" || !got.FromFile || !got.Paper || got.Demo {
		t.Fatalf("file creds = %+v", got)
	}
	if findUp(dir) != path {
		t.Fatalf("findUp = %s", findUp(dir))
	}
}

func TestApplyFallsBackWhenFileMissing(t *testing.T) {
	t.Setenv("DESK_FREQTRADE_CONFIG", filepath.Join(t.TempDir(), "missing.json"))
	got := Apply("local-key", "local-secret", true)
	if got.FromFile || got.Paper || !got.Demo || got.Key != "local-key" {
		t.Fatalf("fallback = %+v", got)
	}
}

func TestApplyLiveFileCanSendOrders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := []byte(`{"dry_run":false,"exchange":{"key":"livekey1","secret":"livesecret"}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DESK_FREQTRADE_CONFIG", path)
	got := Apply("", "", true)
	if got.Paper || got.Demo || !got.FromFile {
		t.Fatalf("live file = %+v", got)
	}
}
