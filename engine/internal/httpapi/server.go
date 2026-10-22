package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"desk/internal/admin"
	"desk/internal/binance"
	"desk/internal/freqtrade"
	"desk/internal/history"
	"desk/internal/model"
	"desk/internal/store"
	"desk/internal/trader"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	history      *history.Store
	historySync  *history.Syncer
	store        *store.Store
	svc          *trader.Service
	hub          *Hub
	exchangeMu   sync.RWMutex
	protectionMu sync.Mutex
}

func New(st *store.Store, svc *trader.Service, hub *Hub) *Server {
	return &Server{store: st, svc: svc, hub: hub}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(s.cors)
	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/api/ws", s.hub.Serve)
	r.Get("/api/models", s.models)
	r.Get("/api/settings", s.getSettings)
	r.Put("/api/settings/binance", s.saveBinance)
	r.Put("/api/settings/binance/connection", s.saveBinanceConnection)
	r.Put("/api/settings/model", s.saveModel)
	r.Get("/api/settings/prompts", s.getPrompts)
	r.Put("/api/settings/prompts", s.savePrompts)
	r.Get("/api/settings/tools", s.getTools)
	r.Get("/api/files", s.files)
	r.Post("/api/files", s.files)
	r.Put("/api/settings/tools", s.saveTools)
	r.Post("/api/binance/test", s.testBinance)
	r.Get("/api/strategy", s.getStrategy)
	r.Put("/api/strategy", s.saveStrategy)
	r.Get("/api/symbols", s.symbols)
	r.Get("/api/account", s.account)
	r.Get("/api/pnl", s.pnl)
	r.Get("/api/positions", s.positions)
	r.With(s.tradingOnly).Post("/api/positions/close", s.closePosition)
	r.With(s.tradingOnly).Post("/api/positions/protection", s.setProtection)
	r.Get("/api/klines", s.klines)
	r.Get("/api/history", s.historyStatus)
	r.Get("/api/history/klines", s.historyKlines)
	r.Post("/api/history/sync", s.syncHistory)
	r.Get("/api/klines/live", s.klineLive)
	r.Get("/api/orders", s.orders)
	r.Get("/api/trader", s.traderState)
	r.Post("/api/trader/start", s.start)
	r.Post("/api/trader/stop", s.stop)
	r.Post("/api/trader/scan", s.scan)
	r.Get("/api/agent/log", s.agentLog)
	r.Post("/api/agent/ask", s.agentAsk)
	r.Put("/api/agent/mode", s.agentMode)
	r.Get("/api/decisions", s.decisions)
	return r
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://127.0.0.1:3210")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,PUT,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	list, err := admin.List(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request) {
	s.exchangeMu.RLock()
	defer s.exchangeMu.RUnlock()
	writeJSON(w, http.StatusOK, s.settingsView())
}

func (s *Server) saveBinance(w http.ResponseWriter, r *http.Request) {
	if !s.exchangeMu.TryLock() {
		writeErr(w, http.StatusConflict, trader.ErrExchangeBusy)
		return
	}
	defer s.exchangeMu.Unlock()
	var body struct {
		APIKey    string `json:"apiKey"`
		APISecret string `json:"apiSecret"`
		Testnet   bool   `json:"testnet"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.svc.ConfigureExchange(func() error {
		return s.store.SaveBinance(body.APIKey, body.APISecret, body.Testnet)
	}); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, trader.ErrExchangeBusy) {
			status = http.StatusConflict
		}
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, s.settingsView())
}

func (s *Server) saveModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ProviderID    string `json:"providerId"`
		UpstreamModel string `json:"upstreamModel"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SaveModel(body.ID, body.Name, body.ProviderID, body.UpstreamModel); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, s.settingsView())
}

func (s *Server) testBinance(w http.ResponseWriter, r *http.Request) {
	s.exchangeMu.RLock()
	defer s.exchangeMu.RUnlock()
	client, err := s.client()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	account, err := client.Account(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	hedge, err := client.Hedge(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "testnet": account.Testnet, "paper": s.creds().Paper,
		"equity": account.Equity, "available": account.Available, "hedge": hedge,
	})
}

func (s *Server) getStrategy(w http.ResponseWriter, _ *http.Request) {
	st, err := s.store.Strategy()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) saveStrategy(w http.ResponseWriter, r *http.Request) {
	var st model.Strategy
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&st); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := normalizeStrategy(&st); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.SaveStrategy(st); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) symbols(w http.ResponseWriter, r *http.Request) {
	creds := s.creds()
	names, err := binance.New(creds.Key, creds.Secret, creds.Demo).Symbols(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, names)
}

func (s *Server) account(w http.ResponseWriter, r *http.Request) {
	client, err := s.client()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	account, err := client.Account(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) positions(w http.ResponseWriter, r *http.Request) {
	client, err := s.client()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rows, err := client.PositionsWithProtection(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) closePosition(w http.ResponseWriter, r *http.Request) {
	s.exchangeMu.RLock()
	defer s.exchangeMu.RUnlock()
	var body struct {
		Symbol string `json:"symbol"`
		Side   string `json:"side"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	body.Symbol = strings.ToUpper(strings.TrimSpace(body.Symbol))
	body.Side = strings.ToLower(strings.TrimSpace(body.Side))
	if body.Symbol == "" || (body.Side != "long" && body.Side != "short") {
		writeErr(w, http.StatusBadRequest, errors.New("请指定币种和方向"))
		return
	}
	if s.creds().Paper {
		writeErr(w, http.StatusBadRequest, errors.New("配置器处于模拟交易，没有向币安发单"))
		return
	}
	client, err := s.client()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	positions, err := client.Positions(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	var qty float64
	for _, p := range positions {
		if p.Symbol == body.Symbol && p.Side == body.Side {
			qty = p.Amount
		}
	}
	if qty <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("没有可平的仓"))
		return
	}
	hedge, err := client.Hedge(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	step, _, minQty, _, err := client.Rules(ctx, body.Symbol)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	text := trader.FloorToStep(qty, step)
	if text == "0" || traderAt(text) < minQty {
		writeErr(w, http.StatusBadRequest, errors.New("持仓数量低于最小下单量"))
		return
	}
	fill, err := client.Market(ctx, binance.MarketOrder{Symbol: body.Symbol, Side: body.Side, Qty: text, Hedge: hedge, Reduce: true})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if err := client.CancelProtection(ctx, body.Symbol, body.Side); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"fill": fill, "warning": "平仓已成交，但保护单清理失败：" + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, fill)
}

func (s *Server) klines(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	interval := strings.TrimSpace(r.URL.Query().Get("interval"))
	if interval == "" {
		interval = "15m"
	}
	if !allowedFrame(interval) {
		writeErr(w, http.StatusBadRequest, errors.New("K线周期格式无效，可选："+strings.Join(binance.Intervals, "、")))
		return
	}
	limit := 1500
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1500 {
			writeErr(w, http.StatusBadRequest, errors.New("limit 必须是 1 到 1500 的整数；需要更多历史请分页"))
			return
		}
	}
	var end int64
	if raw := strings.TrimSpace(r.URL.Query().Get("end")); raw != "" {
		var err error
		end, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || end < 0 {
			writeErr(w, http.StatusBadRequest, errors.New("end 必须是非负的 Unix 秒或毫秒时间戳，0 表示最新"))
			return
		}
	}
	if symbol == "" {
		writeErr(w, http.StatusBadRequest, errors.New("缺少币种"))
		return
	}
	client := binance.New("", "", s.creds().Demo)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rows, err := client.KlinesBefore(ctx, symbol, interval, limit, end)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if symbol == "" {
		writeErr(w, http.StatusBadRequest, errors.New("缺少币种"))
		return
	}
	client, err := s.client()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	rows, err := client.ChartOrders(ctx, symbol)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if rows == nil {
		rows = []model.Level{}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) traderState(w http.ResponseWriter, _ *http.Request) {
	st, err := s.svc.State()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConversationID string `json:"conversationId"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeErr(w, 400, err)
			return
		}
	}
	if err := s.svc.StartConversation(body.ConversationID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	st, _ := s.svc.State()
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	kind, reason := "manual", "收到停止请求（界面或 API）"
	if r.Body != nil && r.ContentLength != 0 {
		var body struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if err := d.Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("停止参数无效"))
			return
		}
		if body.Kind != "manual" && body.Kind != "maintenance" {
			writeErr(w, http.StatusBadRequest, errors.New("停止类型只能是 manual 或 maintenance"))
			return
		}
		kind, reason = body.Kind, strings.TrimSpace(body.Reason)
		if reason == "" {
			writeErr(w, http.StatusBadRequest, errors.New("请填写停止原因"))
			return
		}
	}
	if err := s.svc.StopWithReason(kind, reason); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	st, _ := s.svc.State()
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) scan(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusBadRequest, errors.New("不用手动扫描。启动后，服务器上的代理会自己运行"))
}

func (s *Server) agentLog(w http.ResponseWriter, r *http.Request) {
	if id := r.URL.Query().Get("conversationId"); id != "" {
		state, err := s.svc.State()
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		if id != state.ConversationID {
			writeErr(w, 409, store.ErrConversationChanged)
			return
		}
	}
	writeJSON(w, http.StatusOK, s.svc.Log())
}

func (s *Server) agentAsk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text           string `json:"text"`
		ConversationID string `json:"conversationId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("没有读到这句话"))
		return
	}
	if err := s.svc.AskConversation(body.Text, body.ConversationID); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) decisions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.store.Decisions(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) creds() freqtrade.Creds {
	return s.store.BinanceCredentials()
}

func (s *Server) settingsView() model.SettingsView {
	view := s.store.Settings()
	creds := s.creds()
	profile := binanceProfile(creds)
	view.HasBinanceKey = profile.HasBinanceKey
	view.KeyHint = profile.KeyHint
	view.KeySource = profile.KeySource
	view.DryRun = profile.DryRun
	view.Testnet = profile.Testnet
	view.BinanceConnection = s.store.BinanceConnection()
	view.ConfiguredBinance = binanceProfile(s.store.ConfiguredBinance())
	demoKey, demoSecret := s.store.DemoBinanceKeys()
	view.HasDemoKey = demoKey != "" && demoSecret != ""
	view.DemoKeyHint = keyHint(demoKey)
	return view
}

func (s *Server) client() (*binance.Client, error) {
	creds := s.creds()
	if creds.Key == "" || creds.Secret == "" {
		return nil, errors.New("先在交易所页面保存币安密钥")
	}
	return binance.New(creds.Key, creds.Secret, creds.Demo), nil
}

func normalizeStrategy(st *model.Strategy) error {
	st.Name = strings.TrimSpace(st.Name)
	if st.Name == "" {
		st.Name = "币安默认"
	}
	seen := map[string]bool{}
	coins := make([]string, 0, len(st.Coins))
	for _, c := range st.Coins {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c == "" || seen[c] {
			continue
		}
		for _, r := range c {
			if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
				return errors.New("币种只使用字母和数字，例如 BTCUSDT")
			}
		}
		if !strings.HasSuffix(c, "USDT") {
			return errors.New(c + " 需要是 U 本位合约，例如 BTCUSDT")
		}
		seen[c] = true
		coins = append(coins, c)
	}
	if len(coins) == 0 {
		return errors.New("至少选一个合约")
	}
	st.Coins = coins
	if st.BTCETHLeverage < 1 || st.BTCETHLeverage > 125 || st.AltLeverage < 1 || st.AltLeverage > 125 {
		return errors.New("杠杆需要在 1 到 125 之间")
	}
	if st.MaxPositions < 1 || st.MaxPositions > 10 {
		return errors.New("最多持仓需要在 1 到 10 之间")
	}
	if st.MinConfidence < 0 || st.MinConfidence > 100 {
		return errors.New("最低把握需要在 0 到 100 之间")
	}
	if st.PositionValueRatio < 0.05 || st.PositionValueRatio > 20 {
		return errors.New("单笔上限需要在 0.05 到 20 倍净值之间")
	}
	if st.MaxMarginUsage < 0.05 || st.MaxMarginUsage > 1 {
		return errors.New("保证金上限需要在 5% 到 100% 之间")
	}
	if st.ScanIntervalSec < 60 || st.ScanIntervalSec > 86400 {
		return errors.New("扫描间隔需要在 1 分钟到 24 小时之间")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func traderAt(v string) float64 {
	f, _ := strconv.ParseFloat(v, 64)
	return f
}
