package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"desk/internal/freqtrade"
	"desk/internal/model"
	"desk/internal/trader"
)

func (s *Server) saveBinanceConnection(w http.ResponseWriter, r *http.Request) {
	if !s.exchangeMu.TryLock() {
		writeErr(w, http.StatusConflict, trader.ErrExchangeBusy)
		return
	}
	defer s.exchangeMu.Unlock()
	var body struct {
		Connection  string `json:"connection"`
		APIKey      string `json:"apiKey"`
		APISecret   string `json:"apiSecret"`
		ConfirmLive bool   `json:"confirmLive"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("无法读取交易环境设置"))
		return
	}
	err := s.svc.ConfigureExchange(func() error {
		if body.Connection == "configured" {
			configured := s.store.ConfiguredBinance()
			if !configured.Demo && !configured.Paper && !body.ConfirmLive {
				return errors.New("原配置为实盘，请确认后再切回正式账户")
			}
		}
		return s.store.SaveBinanceConnection(body.Connection, body.APIKey, body.APISecret)
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, trader.ErrExchangeBusy) {
			status = http.StatusConflict
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, s.settingsView())
}

func keyHint(key string) string {
	if len(key) < 4 {
		return ""
	}
	return key[len(key)-4:]
}

func binanceProfile(creds freqtrade.Creds) model.BinanceProfileView {
	source := "local"
	if creds.FromFile {
		source = "freqtrade"
	}
	return model.BinanceProfileView{
		HasBinanceKey: creds.Key != "" && creds.Secret != "",
		KeyHint:       keyHint(creds.Key), Testnet: creds.Demo, DryRun: creds.Paper, KeySource: source,
	}
}
