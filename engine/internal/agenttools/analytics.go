package agenttools

const LegacyAnalyticsInstructions = "## 扩展研究数据（只读）\n以下六个工具用于持续运行和面板对话，可与其他独立读取一起并行调用；有依赖的分页或交易仍顺序执行。不强制每轮全部读取，按需要选择。\n所有工具必须提供 symbol。返回结构为 tool/status/source/fetchedAt/request/data/errors/notes。status=partial 时仅使用成功部分，unavailable 表示没有可靠数据，不能把缺失当作0。source 标注交易所、API主机、环境；数字时间戳除K线time为秒外均为Unix毫秒。请核对实际覆盖期、来源时间与closed状态，过期数据不可当成实时。\n公开工具默认 environment=\"active\"，与当前账户环境一致。模拟盘不支持的接口不会自动改用实盘。确需实盘公开参考行情时，显式指定 environment=\"live_reference\"，返回 referenceOnly=true；不得把它说成模拟盘行情、账户成交或下单价格。get_trading_history 只读当前账户，禁止live_reference。\n\n### get_indicators：连续技术指标\n参数：symbol、interval（默认15m）、limit（默认200，1–1500）、end（0最新，秒排他/毫秒直接传入）、environment。例：{\"symbol\":\"BTCUSDT\",\"interval\":\"1h\",\"limit\":200}。\n返回每根K线的EMA20、MACD(12,26,9) DIF/signal/histogram、Wilder RSI7/ATR14、close、closed。指标由程序按本页数据计算，不是AI编造。histogram=DIF-signal，不乘2；预热不足为null；不同窗口种子有差异。扩大limit获取足够预热，nextEnd原值传入end向前分页，分页间不能直接当成统一计算序列。\n\n### get_trading_history：实际成交与已实现收益\n参数：symbol、period（day=北京时间今天、7d默认、30d）、start/end（可选Unix秒或毫秒，覆盖period）、limit（成交条数默认100，1–1000）。\n完整分页的income包含同合约USDT realized/commission/funding/net，不含转账充值和非USDT费用；fills为独立成交样本（最多所选末7天），含手续费币种、买卖/持仓方向、实际成交价量和交易所时间。观察completeForIncomeWindow、possiblyTruncated和两个独立起止时间；达到limit时缩小start/end继续获取，严禁用样本推算全期胜率/回撤。开仓和平仓需要真实订单/持仓上下文，realizedPnl=0不足以判定开仓。\n\n### get_funding：资金费率\n参数：symbol、limit（默认30，1–1000）、start/end（Unix秒或毫秒）、environment。\n返回current的标记价/指数价/最近资金费率/下次结算时间，以及history实际历史费率。费率是小数，不保证下期费率不变；实际已收付金额看income.funding。history包含nextStart/previousEnd，实际保留范围由接口决定。\n\n### get_open_interest：未平仓合约数量\n参数：symbol、period（默认5m；5m/15m/30m/1h/2h/4h/6h/12h/1d）、limit（默认30，1–500）、start/end、environment。\n返回current和history，以及相邻样本change/changePercent。历史通常仅最近一个月；模拟盘可能只有current，按partial处理。持仓量变化不等于净资金流入。\n\n### get_order_flow：主动成交方向\n参数同get_open_interest；统计通常仅最近30天。\n返回buyVol/sellVol/buySellRatio，以及程序派生delta、imbalance、windowCVD。成交量单位为基础资产；windowCVD只在本次窗口内累积，不能假称全市场历史CVD。\n\n### get_order_book：可见盘口与滑点估算\n参数：symbol、limit（默认20，可选5/10/20/50/100/500/1000）、notionalUSD（可选，正的USDT名义金额）、environment。\n返回盘口档位、spread/spreadBps、mid、可见深度与不平衡度。给定金额时，以notionalUSD/mid确定数量，分别沿卖盘/买盘估计买入/卖出VWAP与相对mid的slippageBps。fullyCovered=false时完整订单价格/滑点为null；不含手续费、延迟和隐藏/RPI流动性。仅供研究，不能保证后续下单成交价。\n"

const AnalyticsInstructions = "## 扩展研究数据（只读）\n以下六个工具用于持续运行和面板对话，可与其他独立读取一起并行调用；有依赖的分页或交易仍顺序执行。不强制每轮全部读取，按需要选择。\n所有工具必须提供 symbol。返回结构为 tool/status/source/fetchedAt/request/data/errors/notes。status=partial 时仅使用成功部分，unavailable 表示没有可靠数据，不能把缺失当作0。source 标注交易所、API主机、环境；数字时间戳除K线time为秒外均为Unix毫秒。请核对实际覆盖期、来源时间与closed状态，过期数据不可当成实时。\n数据环境与交易环境分开：模拟盘模式下，get_open_interest 和 get_order_flow 省略 environment 时默认读取币安实盘公开参考数据（live_reference），用于真实市场统计分析；current/history使用同一来源。其他工具仍默认 active。source.environment/host 标注实际数据源，source.tradingEnvironment 标注交易账户环境，referenceOnly=true 说明仅供研究，账户和下单仍留在模拟盘。显式 environment=\"active\" 始终查询当前环境，不会偷偷替换。get_trading_history 只能读取当前账户，禁止live_reference。\n\n### get_indicators：连续技术指标\n参数：symbol、interval（默认15m）、limit（默认200，1–1500）、end（0最新，秒排他/毫秒直接传入）、environment。例：{\"symbol\":\"BTCUSDT\",\"interval\":\"1h\",\"limit\":200}。\n返回每根K线的EMA20、MACD(12,26,9) DIF/signal/histogram、Wilder RSI7/ATR14、close、closed。指标由程序按本页数据计算，不是AI编造。histogram=DIF-signal，不乘2；预热不足为null；不同窗口种子有差异。扩大limit获取足够预热，nextEnd原值传入end向前分页，分页间不能直接当成统一计算序列。\n\n### get_trading_history：实际成交与已实现收益\n参数：symbol、period（day=北京时间今天、7d默认、30d）、start/end（可选Unix秒或毫秒，覆盖period）、limit（成交条数默认100，1–1000）。\n完整分页的income包含同合约USDT realized/commission/funding/net，不含转账充值和非USDT费用；fills为独立成交样本（最多所选末7天），含手续费币种、买卖/持仓方向、实际成交价量和交易所时间。观察completeForIncomeWindow、possiblyTruncated和两个独立起止时间；达到limit时缩小start/end继续获取，严禁用样本推算全期胜率/回撤。开仓和平仓需要真实订单/持仓上下文，realizedPnl=0不足以判定开仓。\n\n### get_funding：资金费率\n参数：symbol、limit（默认30，1–1000）、start/end（Unix秒或毫秒）、environment。\n返回current的标记价/指数价/最近资金费率/下次结算时间，以及history实际历史费率。费率是小数，不保证下期费率不变；实际已收付金额看income.funding。history包含nextStart/previousEnd，实际保留范围由接口决定。\n\n### get_open_interest：未平仓合约数量\n参数：symbol、period（默认5m；5m/15m/30m/1h/2h/4h/6h/12h/1d）、limit（默认30，1–500）、start/end、environment。\n返回current和history，以及相邻样本change/changePercent。历史通常仅最近一个月；模拟盘模式默认公开实盘参考。显式active查询模拟盘时可能只有current，按partial处理。持仓量变化不等于净资金流入。\n\n### get_order_flow：主动成交方向\n参数同get_open_interest；统计通常仅最近30天。\n返回buyVol/sellVol/buySellRatio，以及程序派生delta、imbalance、windowCVD。成交量单位为基础资产；windowCVD只在本次窗口内累积，不能假称全市场历史CVD。\n\n### get_order_book：可见盘口与滑点估算\n参数：symbol、limit（默认20，可选5/10/20/50/100/500/1000）、notionalUSD（可选，正的USDT名义金额）、environment。\n返回盘口档位、spread/spreadBps、mid、可见深度与不平衡度。给定金额时，以notionalUSD/mid确定数量，分别沿卖盘/买盘估计买入/卖出VWAP与相对mid的slippageBps。fullyCovered=false时完整订单价格/滑点为null；不含手续费、延迟和隐藏/RPI流动性。仅供研究，不能保证后续下单成交价。\n\n### 数据失败后的处理\n如果显式active查询模拟盘历史统计失败，但需要持仓量/主动成交证据，应使用相同symbol、period、limit和日期范围，显式environment=\"live_reference\"查询公开市场统计。两个独立工具可并行。不需要用户切换交易模式或配置实盘密钥。引用结果必须注明实盘公开参考及统计时间；不得把模拟盘接口失败概括成这些市场数据完全不可获取。公开参考也失败时再说明缺失，不编造数据。部分可用要区分current与history，不要丢弃已成功部分；最新成功结果优先于旧错误。恢复后先重新确认当前账户、持仓和行情，不重放历史下单。\n"

func init() {
	Definitions = append(Definitions, []Definition{
		{Name: "get_indicators", Label: "连续技术指标", Description: "连续EMA/MACD/RSI/ATR；未收盘和预热状态明确标记。", Effect: "只读", Scope: "持续运行、面板对话；不修改交易状态", Example: "{\n  \"name\": \"get_indicators\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\"\n  }\n}", Parameters: analyticsParameters("get_indicators")},
		{Name: "get_trading_history", Label: "成交与收益", Description: "当前账户实际成交样本与完整分页的USDT收益汇总。", Effect: "只读", Scope: "持续运行、面板对话；不修改交易状态", Example: "{\n  \"name\": \"get_trading_history\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\"\n  }\n}", Parameters: analyticsParameters("get_trading_history")},
		{Name: "get_funding", Label: "资金费率", Description: "当前标记价、最近费率、下一结算时间与历史费率。", Effect: "只读", Scope: "持续运行、面板对话；不修改交易状态", Example: "{\n  \"name\": \"get_funding\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\"\n  }\n}", Parameters: analyticsParameters("get_funding")},
		{Name: "get_open_interest", Label: "合约持仓量", Description: "当前与历史未平仓量；部分可用时保留成功部分。", Effect: "只读", Scope: "持续运行、面板对话；不修改交易状态", Example: "{\n  \"name\": \"get_open_interest\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\"\n  }\n}", Parameters: analyticsParameters("get_open_interest")},
		{Name: "get_order_flow", Label: "主动买卖量", Description: "主动买卖成交量及本窗口内的成交差值、CVD。", Effect: "只读", Scope: "持续运行、面板对话；不修改交易状态", Example: "{\n  \"name\": \"get_order_flow\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\"\n  }\n}", Parameters: analyticsParameters("get_order_flow")},
		{Name: "get_order_book", Label: "盘口与滑点", Description: "可见盘口、价差、深度及指定金额的双向滑点估算。", Effect: "只读", Scope: "持续运行、面板对话；不修改交易状态", Example: "{\n  \"name\": \"get_order_book\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\"\n  }\n}", Parameters: analyticsParameters("get_order_book")},
	}...)
}

func analyticsParameters(name string) []Parameter {
	p := []Parameter{{Name: "symbol", Type: "string", Required: true, Description: "合约名，如BTCUSDT；研究不限于策略交易名单。"}}
	add := func(n, t, d string) { p = append(p, Parameter{Name: n, Type: t, Description: d}) }
	if name != "get_trading_history" {
		description := "默认active当前环境；显式live_reference读取实盘公开参考，不切换账户或订单环境。"
		if name == "get_open_interest" || name == "get_order_flow" {
			description = "模拟盘模式下省略时默认live_reference（实盘公开市场统计）；显式active查询模拟盘。source注明实际来源，交易账户保持原样。"
		}
		add("environment", "string", description)
	}
	switch name {
	case "get_indicators":
		add("interval", "string", "K线周期，默认15m，由代理选择。")
		add("limit", "integer", "1–1500根，默认200；null表示预热不足。")
		add("end", "integer", "0最新；nextEnd原值回传继续向前分页，秒排他、毫秒直接传入。")
	case "get_order_book":
		add("limit", "integer", "默认20档，可选5/10/20/50/100/500/1000。")
		add("notionalUSD", "number", "可选USDT名义金额，按金额/mid确定数量估算双向滑点；深度不足返回null。")
	default:
		add("start", "integer", "可选起始Unix秒或毫秒；与end限定范围。")
		add("end", "integer", "可选截止Unix秒或毫秒，0最新。")
		switch name {
		case "get_funding":
			add("limit", "integer", "历史条数默认30，1–1000。")
		case "get_trading_history":
			add("period", "string", "day北京时间当天、7d默认、30d；显式start/end优先。")
			add("limit", "integer", "成交样本默认100，1–1000；最长末7天，收益独立完整分页。")
		default:
			add("period", "string", "默认5m；5m/15m/30m/1h/2h/4h/6h/12h/1d。")
			add("limit", "integer", "历史条数默认30，1–500；币安通常仅提供最近30天。")
		}
	}
	return p
}
