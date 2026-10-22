package httpapi

import (
	"desk/internal/binance"
	"desk/internal/prompts"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromptManagementAPI(t *testing.T) {
	s := connectionServer(t, true)
	c := prompts.Defaults()
	c["system"] = "managed custom system; limit={{positionValueRatio}}, coins={{coins}}"
	body, _ := json.Marshal(map[string]any{"prompts": c})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings/prompts", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var view struct {
		Prompts     prompts.Config
		Definitions []prompts.Definition
		Preview     map[string]string
		Parameters  []prompts.Parameter
		Presets     []prompts.Preset
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Prompts["system"] != c["system"] || len(view.Definitions) != 5 || len(view.Parameters) != 7 || len(view.Presets) == 0 {
		t.Fatal("prompt values missing")
	}
	if _, exists := view.Prompts["tools"]; exists {
		t.Fatal("tool instructions leaked into prompt API")
	}
	systemCount := 0
	for _, definition := range view.Definitions {
		if definition.Role == "system" {
			systemCount++
			if definition.Key != "system" && definition.Key != "trading" && definition.Key != "optimization" {
				t.Fatal("request instruction mislabeled as system")
			}
		}
		if definition.Category == "" || definition.Scenario == "" || definition.Placement == "" || definition.EffectiveWhen == "" {
			t.Fatal("prompt usage metadata missing", definition.Key)
		}
	}
	if systemCount != 3 {
		t.Fatal("must expose common and two mode system blocks")
	}
	if !strings.Contains(view.Preview["system"], "limit=1.00") || strings.Contains(view.Preview["system"], "{{coins}}") || !strings.Contains(view.Preview["instructions"], "# INVEST.MD") || strings.Contains(view.Preview["instructions"], "{{coins}}") {
		t.Fatal("preview not rendered")
	}
	state, _ := s.svc.State()
	if state.Running || state.Busy {
		t.Fatal("saving prompts started trading")
	}
	for _, bad := range []string{`{"prompts":{}}`, string(body) + ` {}`, `{"prompts":null}`} {
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings/prompts", strings.NewReader(bad)))
		if w.Code != http.StatusBadRequest {
			t.Fatal("bad payload accepted", w.Code)
		}
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/settings/prompts", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "managed custom system") {
		t.Fatal("saved prompts lost")
	}
}

func TestStrategyDoesNotConstrainChartRequest(t *testing.T) {
	s := connectionServer(t, true)
	st, _ := s.store.Strategy()
	st.Timeframe = "1w"
	st.KlineCount = 1500
	if err := normalizeStrategy(&st); err != nil {
		t.Fatal(err)
	}
	for _, interval := range binance.Intervals {
		if !allowedFrame(interval) {
			t.Fatal("chart blocked exchange interval", interval)
		}
	}
	if allowedFrame("made-up") {
		t.Fatal("invalid interval allowed")
	}
}

func TestInvalidChartRangeDoesNotSilentlyFetchLatest(t *testing.T) {
	s := connectionServer(t, true)
	for _, query := range []string{"end=yesterday", "end=-1", "end=9223372036854775808", "limit=abc", "limit=0", "limit=1501", "interval=invalid"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/klines?symbol=BTCUSDT&"+query, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatal("bad range accepted", query, w.Code)
		}
	}
}
