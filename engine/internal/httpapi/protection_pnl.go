package httpapi

import (
	"context"
	"desk/internal/trader"
	"errors"
	"net/http"
	"time"
)

func pnlRange(period string, now time.Time) (time.Time, error) {
	switch period {
	case "", "day":
		local := now.In(time.FixedZone("UTC+8", 8*3600))
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()), nil
	case "7d":
		return now.AddDate(0, 0, -7), nil
	case "30d":
		return now.AddDate(0, 0, -30), nil
	default:
		return time.Time{}, errors.New("盈亏周期可选 day、7d、30d")
	}
}
func (s *Server) pnl(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	from, err := pnlRange(r.URL.Query().Get("period"), now)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	client, err := s.client()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := client.IncomeSummary(ctx, from.UnixMilli(), now.UnixMilli())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
func (s *Server) setProtection(w http.ResponseWriter, r *http.Request) {
	s.exchangeMu.RLock()
	defer s.exchangeMu.RUnlock()
	s.protectionMu.Lock()
	defer s.protectionMu.Unlock()
	req, err := trader.DecodeProtection(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if s.creds().Paper {
		writeErr(w, http.StatusBadRequest, errors.New("模拟不发单模式不能设置交易所保护单"))
		return
	}
	client, err := s.client()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := trader.ProtectPosition(ctx, client, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
