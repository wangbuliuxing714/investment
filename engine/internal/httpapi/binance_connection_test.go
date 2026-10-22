package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"desk/internal/model"
	"desk/internal/store"
	"desk/internal/trader"
)

func connectionServer(t *testing.T, paper bool) *Server {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"dry_run":true,"exchange":{"key":"configured-key","secret":"configured-secret"}}`
	if !paper {
		raw = strings.Replace(raw, "true", "false", 1)
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DESK_FREQTRADE_CONFIG", path)
	st, err := store.Open(filepath.Join(dir, "desk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	hub := NewHub()
	return New(st, trader.New(st, hub), hub)
}

func putConnection(s *Server, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings/binance/connection", strings.NewReader(body)))
	return w
}

func TestDemoAPIOverridesConfiguratorAndDoesNotExposeSecrets(t *testing.T) {
	s := connectionServer(t, true)
	w := putConnection(s, `{"connection":"demo","apiKey":"demo-test-key","apiSecret":"demo-test-secret"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save failed: %d %s", w.Code, w.Body.String())
	}
	var view model.SettingsView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.BinanceConnection != "demo" || !view.Testnet || view.DryRun || !view.HasDemoKey || !view.HasBinanceKey {
		t.Fatal("demo mode is not reflected in the settings response")
	}
	if view.ConfiguredBinance.KeySource != "freqtrade" || !view.ConfiguredBinance.DryRun {
		t.Fatal("original profile is not available for restoration")
	}
	for _, secret := range []string{"demo-test-key", "demo-test-secret", "configured-key", "configured-secret"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("a complete credential leaked into the response")
		}
	}
	if got := s.creds(); !got.Demo || got.Paper || got.Key != "demo-test-key" {
		t.Fatal("HTTP requests are not using the demo connection")
	}
	state, err := s.svc.State()
	if err != nil || state.Running || state.Busy {
		t.Fatal("selecting a connection must not start the agent")
	}
}

func TestRestoreLiveRequiresExplicitConfirmation(t *testing.T) {
	s := connectionServer(t, false)
	if err := s.store.SaveBinanceConnection("demo", "demo-key", "demo-secret"); err != nil {
		t.Fatal(err)
	}
	w := putConnection(s, `{"connection":"configured"}`)
	if w.Code != http.StatusBadRequest || !s.creds().Demo {
		t.Fatal("restoring a live profile without confirmation was allowed")
	}
	w = putConnection(s, `{"connection":"configured","confirmLive":true}`)
	if w.Code != http.StatusOK || s.creds().Demo || s.creds().Paper || !s.creds().FromFile {
		t.Fatal("confirmed restoration failed")
	}
}

func TestConnectionChangesBlockedDuringExecution(t *testing.T) {
	s := connectionServer(t, true)
	if err := s.store.SetRunning(true); err != nil {
		t.Fatal(err)
	}
	w := putConnection(s, `{"connection":"demo","apiKey":"key","apiSecret":"secret"}`)
	if w.Code != http.StatusConflict || s.store.BinanceConnection() != "configured" {
		t.Fatal("running agent allowed an environment switch")
	}
	if err := s.store.SetRunning(false); err != nil {
		t.Fatal(err)
	}
	s.exchangeMu.RLock()
	w = putConnection(s, `{"connection":"demo","apiKey":"key","apiSecret":"secret"}`)
	s.exchangeMu.RUnlock()
	if w.Code != http.StatusConflict {
		t.Fatal("in-flight manual operation allowed an environment switch")
	}
}

func TestBadDemoRequestsKeepOriginalMode(t *testing.T) {
	s := connectionServer(t, true)
	for _, body := range []string{`{`, `{}`, `{"connection":"live"}`, `{"connection":"demo"}`, `{"connection":"demo","apiKey":"key"}`} {
		w := putConnection(s, body)
		if w.Code != http.StatusBadRequest || s.store.BinanceConnection() != "configured" {
			t.Fatalf("bad request changed the environment: %d", w.Code)
		}
	}
}
