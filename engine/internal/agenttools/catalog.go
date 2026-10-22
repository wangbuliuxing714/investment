package agenttools

// Definitions describe the built-in executor, not user-defined executable tools.
type Parameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type Definition struct {
	Name        string      `json:"name"`
	Label       string      `json:"label"`
	Description string      `json:"description"`
	Effect      string      `json:"effect"`
	Scope       string      `json:"scope"`
	Example     string      `json:"example"`
	Parameters  []Parameter `json:"parameters"`
}

var Definitions = []Definition{
	{
		Name:        "get_account",
		Label:       "查看账户",
		Description: "读取净值、钱包余额、可用资金、未实现盈亏和已用保证金。",
		Effect:      "只读",
		Scope:       "持续运行、面板对话",
		Example:     "{\n  \"name\": \"get_account\",\n  \"arguments\": {}\n}",
		Parameters:  []Parameter{},
	},
	{
		Name:        "get_positions",
		Label:       "查看持仓",
		Description: "读取当前持仓的方向、数量、开仓价、标记价、盈亏和杠杆。",
		Effect:      "只读",
		Scope:       "持续运行、面板对话",
		Example:     "{\n  \"name\": \"get_positions\",\n  \"arguments\": {}\n}",
		Parameters:  []Parameter{},
	},
	{
		Name:        "get_market",
		Label:       "查看行情",
		Description: "代理自行选择合约、周期和历史范围；允许查看交易名单之外的合约。",
		Effect:      "只读",
		Scope:       "持续运行、面板对话",
		Example:     "{\n  \"name\": \"get_market\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\",\n    \"interval\": \"1h\",\n    \"limit\": 200,\n    \"end\": 0\n  }\n}",
		Parameters: []Parameter{
			{Name: "symbol", Type: "string", Required: false, Description: "合约名，如 BTCUSDT；省略时查看策略币种。"},
			{Name: "interval", Type: "string", Required: false, Description: "K 线周期，默认 15m；实际支持范围由行情接口决定。"},
			{Name: "limit", Type: "integer", Required: false, Description: "本次读取根数，默认 200，范围 1–1500。20 根 15m 仅约 5 小时；更长范围可增加根数、改变周期或分页，没有仅限当天的限制。"},
			{Name: "end", Type: "integer", Required: false, Description: "历史截止时间；Unix 秒为排他截止，毫秒直接传入接口；省略或 0 表示最新。将返回的 nextEnd 原值传回此字段，不要再次减 1。结果同时注明 UTC 与北京时间范围。"},
		},
	},
	{
		Name:        "submit_decisions",
		Label:       "提交交易决定",
		Description: "提交 open_long、open_short、close_long、close_short 或 hold。交易币种、杠杆、仓位与保证金继续由程序校验。",
		Effect:      "可能下单",
		Scope:       "持续运行、面板对话；取决于当前账户模式和风控",
		Example:     "{\n  \"name\": \"submit_decisions\",\n  \"arguments\": {\n    \"decisions\": [\n      {\n        \"symbol\": \"BTCUSDT\",\n        \"action\": \"hold\",\n        \"reasoning\": \"等待更多数据\"\n      }\n    ]\n  }\n}",
		Parameters: []Parameter{
			{Name: "decisions", Type: "array", Required: true, Description: "决定列表，每项包含 symbol 和 action；可填写 leverage、position_size_usd（名义价值，单位 USDT）、stop_loss、take_profit、confidence、reasoning。"},
		},
	},
	{
		Name: "set_protection", Label: "设置止盈止损", Description: "为已有持仓补挂缺失的交易所止盈止损，不开新仓、不改数量、不删除已有保护单。", Effect: "可能创建保护单", Scope: "持续运行、面板对话；仅策略合约且必须有实际持仓",
		Example: "{\n  \"name\": \"set_protection\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\",\n    \"side\": \"long\",\n    \"stop_loss\": 0,\n    \"take_profit\": 0\n  }\n}",
		Parameters: []Parameter{
			{Name: "symbol", Type: "string", Required: true, Description: "已有持仓的合约。"},
			{Name: "side", Type: "string", Required: true, Description: "已有持仓方向：long 或 short。"},
			{Name: "stop_loss", Type: "number", Description: "标记价触发的止损价；至少填写止盈/止损之一，必须是有效正数。"},
			{Name: "take_profit", Type: "number", Description: "标记价触发的止盈价；已有保护单保持原样，不重复创建。"},
		},
	},
	{
		Name:        "wait",
		Label:       "等待",
		Description: "安排持续运行的下一次查看时间；有新用户消息时可提前唤醒。",
		Effect:      "调度",
		Scope:       "仅持续运行；面板对话不会执行等待",
		Example:     "{\n  \"name\": \"wait\",\n  \"arguments\": {\n    \"seconds\": 120,\n    \"reason\": \"等待下一次行情变化\"\n  }\n}",
		Parameters: []Parameter{
			{Name: "seconds", Type: "integer", Required: false, Description: "等待秒数，程序限制为 15–900 秒；省略或低于下限时按 15 秒处理。"},
			{Name: "reason", Type: "string", Required: false, Description: "等待原因。"},
		},
	},
}
