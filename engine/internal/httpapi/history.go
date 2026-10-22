package httpapi

import (
	"context"
	"desk/internal/history"
	"errors"
	"net/http"
	"strconv"
)

func (s *Server) AttachHistory(st *history.Store, syncer *history.Syncer) {
	s.history = st
	s.historySync = syncer
}
func (s *Server) historyStatus(w http.ResponseWriter, r *http.Request) {
	if s.history == nil {
		writeErr(w, 503, errors.New("历史库尚未开启"))
		return
	}
	datasets, err := s.history.Datasets(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	gaps := map[string][]history.Gap{}
	for _, d := range datasets {
		g, e := s.history.Gaps(r.Context(), d.Market)
		if e != nil {
			writeErr(w, 500, e)
			return
		}
		gaps[d.Market] = g
	}
	writeJSON(w, 200, map[string]any{"datasets": datasets, "gaps": gaps})
}
func (s *Server) historyKlines(w http.ResponseWriter, r *http.Request) {
	if s.history == nil {
		writeErr(w, 503, errors.New("历史库尚未开启"))
		return
	}
	q := r.URL.Query()
	m := q.Get("market")
	symbol := q.Get("symbol")
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	interval := q.Get("interval")
	if interval == "" {
		interval = "1d"
	}
	end := int64(0)
	limit := 1500
	var err error
	if q.Has("end") {
		end, err = strconv.ParseInt(q.Get("end"), 10, 64)
		if err != nil {
			writeErr(w, 400, err)
			return
		}
	}
	if q.Has("limit") {
		limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			writeErr(w, 400, err)
			return
		}
	}
	if !history.ValidMarket(m) || symbol != "BTCUSDT" || !history.ValidInterval(interval) || end < 0 || limit < 1 || limit > 5000 {
		writeErr(w, 400, errors.New("无效历史行情参数"))
		return
	}
	page, err := s.history.Query(r.Context(), m, symbol, interval, end, limit)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, page)
}
func (s *Server) syncHistory(w http.ResponseWriter, r *http.Request) {
	m := r.URL.Query().Get("market")
	if !history.ValidMarket(m) {
		writeErr(w, 400, errors.New("market 必须为 spot 或 um"))
		return
	}
	if s.historySync == nil {
		writeErr(w, 503, errors.New("历史同步尚未开启"))
		return
	}
	started := s.historySync.Start(context.Background(), m)
	writeJSON(w, 202, map[string]bool{"started": started})
}
