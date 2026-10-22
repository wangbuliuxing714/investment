package agenttools

const HistoryInstructions = `## 本地历史行情库
get_history，arguments 为 {"market":"spot","symbol":"BTCUSDT","interval":"1d","limit":200,"end":0}。market 必填：spot 现货，um 为 USDT 本位永续；数据来自币安正式市场公开历史，和交易账户环境无关。读取本地库，无需密钥。支持1m、3m、5m、15m、30m、1h、2h、4h、6h、8h、12h、1d、3d、1w、1M；UTC 聚合，周一开始，月为自然月，3d 以 Unix epoch 对齐。单次1–5000根，nextEnd 原值用于下一页 end（Unix秒，严格早于），hasMore 表示本地还有更早数据。先检查 dataset.status、first、last、availableFirst、syncedThrough、gapCount 和 missingMinutes；下载未完成不能声称完整历史。closed=false 表示周期未结束或分钟样本不全，缺口不填造数据。历史价格只作研究，不能当成当前可成交价格。`

func init() {
	Definitions = append(Definitions, Definition{Name: "get_history", Label: "读取本地历史", Description: "读取管理员历史行情页共用的 BTC 现货和永续历史库及覆盖状态。", Effect: "只读", Scope: "持续运行、面板对话，无需密钥", Example: `{"name":"get_history","arguments":{"market":"spot","interval":"1d","limit":200}}`, Parameters: []Parameter{{Name: "market", Type: "string", Required: true, Description: "spot 现货 / um 永续"}, {Name: "symbol", Type: "string", Description: "BTCUSDT"}, {Name: "interval", Type: "string", Description: "1m 至 1M，默认1d"}, {Name: "end", Type: "integer", Description: "早于此 Unix秒/毫秒，0为最新"}, {Name: "limit", Type: "integer", Description: "1–5000，默认200"}}})
}
