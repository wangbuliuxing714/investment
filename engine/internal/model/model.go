package model

// Strategy is the saved trading policy. The scan loop reads this row.
type Strategy struct {
	Name               string   `json:"name"`
	Coins              []string `json:"coins"`
	BTCETHLeverage     int      `json:"btcEthLeverage"`
	AltLeverage        int      `json:"altLeverage"`
	MaxPositions       int      `json:"maxPositions"`
	MinConfidence      int      `json:"minConfidence"`
	PositionValueRatio float64  `json:"positionValueRatio"`
	MaxMarginUsage     float64  `json:"maxMarginUsage"`
	ScanIntervalSec    int      `json:"scanIntervalSec"`
	// Retained solely to read legacy database rows; no longer strategy settings.
	Prompt     string `json:"-"`
	Timeframe  string `json:"-"`
	KlineCount int    `json:"-"`
}

// SettingsView is safe to send to the browser. Secrets stay out of it.
type SettingsView struct {
	BinanceConnection string             `json:"binanceConnection"`
	HasDemoKey        bool               `json:"hasDemoKey"`
	DemoKeyHint       string             `json:"demoKeyHint"`
	ConfiguredBinance BinanceProfileView `json:"configuredBinance"`
	HasBinanceKey     bool               `json:"hasBinanceKey"`
	KeyHint           string             `json:"keyHint"`
	Testnet           bool               `json:"testnet"`
	KeySource         string             `json:"keySource"`
	DryRun            bool               `json:"dryRun"`
	ModelID           string             `json:"modelId"`
	ModelName         string             `json:"modelName"`
	ProviderID        string             `json:"providerId"`
	UpstreamModel     string             `json:"upstreamModel"`
}

// BinanceProfileView describes a connection without exposing credentials.
type BinanceProfileView struct {
	HasBinanceKey bool   `json:"hasBinanceKey"`
	KeyHint       string `json:"keyHint"`
	Testnet       bool   `json:"testnet"`
	KeySource     string `json:"keySource"`
	DryRun        bool   `json:"dryRun"`
}

type TraderState struct {
	Mode           string `json:"mode"`
	ConversationID string `json:"conversationId"`
	Switching      bool   `json:"switching"`
	Running        bool   `json:"running"`
	StartedAt      string `json:"startedAt"`
	LastScanAt     string `json:"lastScanAt"`
	LastError      string `json:"lastError"`
	Busy           bool   `json:"busy"`
	StopKind       string `json:"stopKind,omitempty"`
	StopReason     string `json:"stopReason,omitempty"`
	StoppedAt      string `json:"stoppedAt,omitempty"`
}

type Conversation struct {
	ID        string `json:"id"`
	Mode      string `json:"mode"`
	CreatedAt string `json:"createdAt"`
}

type DecisionRecord struct {
	ID            int64  `json:"id"`
	CreatedAt     string `json:"createdAt"`
	Status        string `json:"status"`
	Reasoning     string `json:"reasoning"`
	RawResponse   string `json:"rawResponse"`
	InputPrompt   string `json:"inputPrompt"`
	DecisionsJSON string `json:"decisionsJson"`
	ExecutionLog  string `json:"executionLog"`
	Error         string `json:"error"`
}

type Account struct {
	Equity     float64 `json:"equity"`
	Wallet     float64 `json:"wallet"`
	Available  float64 `json:"available"`
	Unrealized float64 `json:"unrealized"`
	MarginUsed float64 `json:"marginUsed"`
	Testnet    bool    `json:"testnet"`
}

type Position struct {
	Symbol          string  `json:"symbol"`
	Side            string  `json:"side"`
	Amount          float64 `json:"amount"`
	Entry           float64 `json:"entry"`
	Mark            float64 `json:"mark"`
	PnL             float64 `json:"pnl"`
	Leverage        int     `json:"leverage"`
	Notional        float64 `json:"notional"`
	Liquidation     float64 `json:"liquidation"`
	Protection      []Level `json:"protection,omitempty"`
	ProtectionKnown bool    `json:"protectionKnown"`
	ProtectionError string  `json:"protectionError,omitempty"`
}

type Candle struct {
	CloseTime int64   `json:"closeTime,omitempty"`
	Closed    *bool   `json:"closed,omitempty"`
	Time      int64   `json:"time"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
	UpdatedAt int64   `json:"updatedAt,omitempty"`
	Source    string  `json:"source,omitempty"`
}

type PnLSummary struct {
	Asset      string  `json:"asset"`
	From       int64   `json:"from"`
	To         int64   `json:"to"`
	Realized   float64 `json:"realized"`
	Commission float64 `json:"commission"`
	Funding    float64 `json:"funding"`
	Net        float64 `json:"net"`
	UpdatedAt  int64   `json:"updatedAt"`
}

// Level is a horizontal price on the chart: entry, stop, take, liquidation, or a resting order.
type Level struct {
	Kind        string  `json:"kind"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	Protective  bool    `json:"protective"`
	WorkingType string  `json:"workingType,omitempty"`
}

type AdminModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ProviderID    string `json:"providerId"`
	UpstreamModel string `json:"upstreamModel"`
}

// Decision is what the model asked for, before the program applies caps.
type Decision struct {
	Symbol          string  `json:"symbol"`
	Action          string  `json:"action"`
	Leverage        int     `json:"leverage"`
	PositionSizeUSD float64 `json:"position_size_usd"`
	StopLoss        float64 `json:"stop_loss"`
	TakeProfit      float64 `json:"take_profit"`
	Confidence      int     `json:"confidence"`
	Reasoning       string  `json:"reasoning"`
}
