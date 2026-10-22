package agenttools

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"desk/internal/model"
	"desk/internal/prompts"
)

// Config is independently persisted; it is not a model prompt setting.
type Config struct {
	Instructions string `json:"instructions"`
}

const LegacyInstructions = "## 工具调用协议\n每次先用中文说明你在做什么，再给出工具调用。工具放在 ```tool 代码块里，内容是 JSON：{\"name\":\"工具名\",\"arguments\":{}}\n\n## 账户与持仓\nget_account，arguments 为 {}。查看净值、可用资金和已用保证金。\nget_positions，arguments 为 {}。查看当前持仓。\n\n## 行情与历史 K 线\nget_market，arguments 为 {\"symbol\":\"BTCUSDT\",\"interval\":\"1h\",\"limit\":200,\"end\":0}。你自行选择币安支持的合约、周期和根数，可以查看策略之外的合约作为参考。interval 例如 1s、1m、3m、5m、15m、30m、1h、2h、4h、6h、8h、12h、1d、3d、1w、1M，具体支持范围由币安接口返回。单次最多 1500 根是币安接口限制；需要更多历史时用返回的 nextEnd 继续向前读取，没有策略设置的总历史上限。end 接受 Unix 秒或毫秒，0 表示最新。省略 symbol 查看策略币种；省略 interval 和 limit 则使用 15m、200 根。\n\n## 提交交易决定\nsubmit_decisions，arguments 为 {\"decisions\":[...]}。这个工具提交开平仓决定；set_protection 只为已有持仓补挂保护单。仍只能交易策略允许的币种。看不清就提交 hold。\n\n## 持续运行与等待\n持续运行时，下次何时再看由你调用 wait 决定。面板对话时遵循该场景的附加指令。\nwait，arguments 为 {\"seconds\":120,\"reason\":\"原因\"}。秒数由你定，程序接受 15 秒到 15 分钟。\n\n## 用户消息与模型边界\n如果出现用户新说的话，先用中文直接回答，再决定要不要调用工具。不要无视用户。\n不要请求别的模型。程序不会改用本机模型。\n\n## 行情范围、时间与指标\n先核对工具返回的起止日期与根数，再判断是否满足分析范围。20 根 15m K线只覆盖约 5 小时，不代表只能查看今天；需要跨日或长周期依据时，自行增加 limit、切换 interval 或使用 nextEnd 连续向前分页。不要把短窗口当成完整历史。\n表格时间是每根 K线的开盘时间（UTC），不是当天市场开盘时间；北京时间为 UTC+8，引用时间时注明时区。end 使用 Unix 秒时表示严格早于该时刻，毫秒则直接传入接口；nextEnd 原值传回 end，不要再次减 1。\n历史页末根收盘价不是当前实时报价，最新一根也可能尚未收盘。指标只基于本页计算；N/A 代表样本不足，不得当作零或编造指标走势，需要时扩大数据窗口后重新读取。\n\n## 并行调用与顺序依赖\n同一条回复可以输出多个独立的 tool JSON 代码块，每个代码块仍是一份 {\"name\":\"工具名\",\"arguments\":{}} 对象。独立的 get_account、get_positions、不同币种或周期的 get_market 请尽量在同一条回复中一起发出，引擎会并行读取，不必一次只调用一个工具。每批最多同时执行 8 个只读工具，其余自动排队；这不限制调用总数或分析轮数，也没有固定 30 秒暂停。\n例如先说明“并行查看账户、持仓和 BTC/ETH 行情”，然后在同一条回复中分别给出四个 tool 代码块：get_account {}、get_positions {}、get_market {\"symbol\":\"BTCUSDT\",\"interval\":\"1h\",\"limit\":200}、get_market {\"symbol\":\"ETHUSDT\",\"interval\":\"1h\",\"limit\":200}。实际币种、周期和范围仍由你判断。\n依赖上一项结果的调用要等结果返回后再发出：例如 nextEnd 历史分页、根据账户和行情制定下单参数。submit_decisions 和 set_protection 按顺序执行，前面的查询全部结束后才进入下单；下单后的查询在订单处理完成后再运行，避免读到过期持仓。wait 仍由你主动选择，程序不因连续调用工具而强制休息。\n\n## 已有持仓的止盈止损\nget_positions 同时返回交易所实际保护单。未设置代表没有查到挂单；读取失败代表未知，不能当作没有保护。开仓成交不代表止盈止损成功，不得把模型计划或口头承诺说成已挂单。程序没有本地自动止盈止损兜底。\nset_protection，arguments 为 {\"symbol\":\"合约\",\"side\":\"long或short\",\"stop_loss\":止损价,\"take_profit\":止盈价}，至少填写一个正数价格。先查询持仓、实际保护状态与最新行情，再自行确定价格并调用。只补挂缺失项，保留已有保护单，不开仓、不加仓；0 或省略表示不补该项。按标记价触发，返回 confirmed 后仍应以交易所查询为准。若部分失败，重新读取实际保护单，仅补缺失项，严禁重复开仓。和交易决定一样按顺序执行，不与有依赖的下单并行。"

const DecisionInstructions = "## 决策输出协议\n决定格式：{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"open_long\",\"leverage\":5,\"position_size_usd\":100,\"stop_loss\":0,\"take_profit\":0,\"confidence\":80,\"reasoning\":\"原因\"}]}\naction 只能是 open_long、open_short、close_long、close_short、hold。position_size_usd 是名义价值，单位 USDT，不是保证金。"

var DefaultInstructions = NativeProtocolText(LegacyInstructions + "\n\n" + AnalyticsInstructions + "\n\n" + DecisionInstructions + "\n\n" + FileInstructions + "\n\n" + HistoryInstructions)

func Defaults() Config { return Config{Instructions: DefaultInstructions} }

func Validate(c Config) error {
	if strings.TrimSpace(c.Instructions) == "" {
		return fmt.Errorf("工具说明不能为空")
	}
	if !utf8.ValidString(c.Instructions) || utf8.RuneCountInString(c.Instructions) > 30000 {
		return fmt.Errorf("工具说明不能超过 30000 字")
	}
	return nil
}

func Render(c Config, st model.Strategy) string {
	return prompts.Render(c.Instructions, st)
}
