package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func demoStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"dry_run":true,"exchange":{"key":"configured-key","secret":"configured-secret"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DESK_FREQTRADE_CONFIG", config)
	path := filepath.Join(dir, "desk.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, config, path
}

func TestDemoConnectionOverridesConfiguratorAndSurvivesReopen(t *testing.T) {
	st, config, path := demoStore(t)
	original, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveBinance("local-live-key", "local-live-secret", false); err != nil {
		t.Fatal(err)
	}
	if !st.BinanceCredentials().Paper || st.BinanceConnection() != "configured" {
		t.Fatal("original configuration must remain selected until explicitly changed")
	}
	if err := st.SaveBinanceConnection("demo", "demo-key", "demo-secret"); err != nil {
		t.Fatal(err)
	}
	creds := st.BinanceCredentials()
	if !creds.Demo || creds.Paper || creds.FromFile || creds.Key != "demo-key" || creds.Secret != "demo-secret" {
		t.Fatal("demo must use only the independent demo pair and allow demo orders")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.BinanceCredentials() != creds {
		t.Fatal("demo connection did not survive restart")
	}
	if err := reopened.SaveBinanceConnection("configured", "", ""); err != nil {
		t.Fatal(err)
	}
	if got := reopened.BinanceCredentials(); !got.FromFile || !got.Paper || got.Key != "configured-key" {
		t.Fatal("original configurator connection was not restored")
	}
	if key, secret, demo := reopened.BinanceKeys(); key != "local-live-key" || secret != "local-live-secret" || demo {
		t.Fatal("local production credentials were overwritten")
	}
	if err := reopened.SaveBinanceConnection("demo", "", ""); err != nil || reopened.BinanceCredentials() != creds {
		t.Fatal("saved demo credentials should remain reusable")
	}
	current, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("configurator file was modified")
	}
}

func TestDemoConnectionValidationIsAtomic(t *testing.T) {
	st, _, _ := demoStore(t)
	for _, args := range [][3]string{{"demo", "", ""}, {"demo", "new-key", ""}, {"live", "key", "secret"}} {
		if err := st.SaveBinanceConnection(args[0], args[1], args[2]); err == nil {
			t.Fatal("invalid or incomplete configuration was accepted")
		}
		if st.BinanceConnection() != "configured" {
			t.Fatal("failed save changed the selected environment")
		}
	}
	if err := st.SaveBinanceConnection("demo", "original-key", "original-secret"); err != nil {
		t.Fatal(err)
	}
	before := st.BinanceCredentials()
	if err := st.SaveBinanceConnection("demo", "different-key", ""); err == nil {
		t.Fatal("a new key must not be paired with an old secret")
	}
	if st.BinanceCredentials() != before {
		t.Fatal("failed save partially changed the demo credentials")
	}
}

func TestMissingDemoCredentialsNeverFallBackToProduction(t *testing.T) {
	st, _, _ := demoStore(t)
	for _, connection := range []string{"demo", "unknown"} {
		if err := st.setSetting("binance_connection", connection); err != nil {
			t.Fatal(err)
		}
		got := st.BinanceCredentials()
		if !got.Demo || got.Key != "" || got.Secret != "" || got.FromFile {
			t.Fatal("invalid demo configuration fell back to another account")
		}
	}
}
