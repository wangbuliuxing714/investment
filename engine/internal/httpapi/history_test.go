package httpapi

import (
	"desk/internal/history"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHistoryRoutesUseLocalStoreAndValidateParameters(t *testing.T) {
	s := connectionServer(t, true)
	h, e := history.Open(filepath.Join(t.TempDir(), "market.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	s.AttachHistory(h, nil)
	for path, code := range map[string]int{"/api/history": 200, "/api/history/klines?market=spot&interval=1w": 200, "/api/history/klines?market=um&interval=1M": 200, "/api/history/klines?market=spot&symbol=ETHUSDT": 400, "/api/history/klines?market=bad": 400, "/api/history/klines?market=spot&limit=5001": 400, "/api/history/klines?market=spot&end=oops": 400, "/api/history/klines?market=spot&interval=1s": 400} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != code {
			t.Fatal(path, w.Code, w.Body.String())
		}
		if code == 200 && path != "/api/history" {
			var page history.Page
			if e = json.Unmarshal(w.Body.Bytes(), &page); e != nil || page.Candles == nil || len(page.Candles) != 0 || page.Dataset.Status != "pending" {
				t.Fatal(page, e)
			}
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/history/sync?market=bad", nil))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	state, _ := s.svc.State()
	if state.Running || state.Busy || len(s.svc.Log()) > 0 {
		t.Fatal("history routes changed trader")
	}
}
