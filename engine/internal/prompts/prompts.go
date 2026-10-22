package prompts

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"desk/internal/model"
)

// Config holds all editable instructions authored by the trading desk.
type Config map[string]string

type Definition struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Category      string `json:"category"`
	Role          string `json:"role"`
	Scenario      string `json:"scenario"`
	Placement     string `json:"placement"`
	EffectiveWhen string `json:"effectiveWhen"`
	Default       string `json:"default"`
	Required      bool   `json:"required"`
}

var LegacyDefinitions = []Definition{
	{
		Key:           "system",
		Label:         "系统提示词",
		Description:   "定义代理身份、职责、行为边界和输出原则。",
		Category:      "system",
		Role:          "system",
		Scenario:      "每次模型调用：持续运行与面板对话均使用。",
		Placement:     "独立配置为云端交易代理的 system 指令，不拼入请求正文。",
		EffectiveWhen: "下一次模型请求；修改后刷新云端代理配置。",
		Default:       "## 身份与职责\n你是使用者的币安 U 本位合约交易代理，负责基于账户、持仓和行情作出交易判断。\n\n## 行为边界\n不要读写文件，不要执行命令，不要使用服务器上的工具。仅按请求正文提供的交易台工具协议行动。交易权限与下单风控由程序执行。\n\n## 输出规范\n先用中文简短说明判断或下一步行动，再按请求正文规定的格式给出工具调用。",
		Required:      true,
	},
	{
		Key:           "trading",
		Label:         "交易指令模板",
		Description:   "约定决策格式、名义价值口径和策略限制。",
		Category:      "common",
		Role:          "request",
		Scenario:      "每次模型调用；提供交易规则和当前策略参数。",
		Placement:     "请求正文第 1 段，位于自定义交易说明之前。",
		EffectiveWhen: "保存后的下一次模型请求。",
		Default:       "## 任务与范围\n你是使用者的私人合约交易员，只分析币安 U 本位永续合约。\n先用中文说明判断，再通过 submit_decisions 提交决定。程序会再用代码限制杠杆、仓位和保证金。\n\n## 决策输出协议\n决定格式：{\"decisions\":[{\"symbol\":\"BTCUSDT\",\"action\":\"open_long\",\"leverage\":5,\"position_size_usd\":100,\"stop_loss\":0,\"take_profit\":0,\"confidence\":80,\"reasoning\":\"原因\"}]}\naction 只能是 open_long、open_short、close_long、close_short、hold。position_size_usd 是名义价值，单位 USDT，不是保证金。\n\n## 当前策略限制\n允许下单的币种：{{coins}}。查看行情不受这份名单限制。\nBTCUSDT 和 ETHUSDT 杠杆不超过 {{btcEthLeverage}}，其他币种不超过 {{altLeverage}}。\n最多同时持有 {{maxPositions}} 个仓位。单笔名义价值不超过净值的 {{positionValueRatio}} 倍。保证金占用不超过净值的 {{maxMarginPercent}}%。\n\n## 观望条件\n你自己判断时，把握低于 {{minConfidence}} 就观望。这个门槛由你遵守，程序不替你丢弃低把握的单。",
		Required:      true,
	},
	{
		Key:           "strategy",
		Label:         "自定义交易说明",
		Description:   "原策略页面的自定义文本；切换模型时保留。",
		Category:      "common",
		Role:          "request",
		Scenario:      "每次模型调用；补充你的交易偏好或判断方法，可留空。",
		Placement:     "提示词正文第 2 段，位于交易指令之后。",
		EffectiveWhen: "保存后的下一次模型请求。",
		Default:       "## 交易范围\n只在列出的币安 U 本位永续合约里交易。\n\n## 入场与观望\n方向不明确就观望。\n\n## 仓位与退出\n开仓要写止损和止盈，仓位不要超过策略上限。",
		Required:      false,
	},
	{
		Key:           "chat",
		Label:         "面板对话指令",
		Description:   "要求先回答当前用户，不在面板对话中安排等待。",
		Category:      "chat",
		Role:          "request",
		Scenario:      "持续运行未开启时，用户在面板发送消息。运行中的用户消息直接进入持续运行历史，不附加本项。",
		Placement:     "通用交易指令及独立 Tools 上下文之后、对话历史引导之前。",
		EffectiveWhen: "保存后下一次符合条件的面板对话请求。",
		Default:       "## 当前场景\n用户在面板主动发起对话，持续运行未开启。\n\n## 本轮要求\n先用中文回答用户。需要数据可以调用工具。这一轮不要调用 wait。",
		Required:      true,
	},
	{
		Key:           "start",
		Label:         "开始运行指令",
		Description:   "引导首次查看账户、持仓与行情。",
		Category:      "automation",
		Role:          "event",
		Scenario:      "每次启动持续运行时，仅追加一次。",
		Placement:     "作为“用户”标签的事件指令写入运行历史，再随历史进入请求正文。",
		EffectiveWhen: "下一次启动持续运行；不会替换本次已写入的启动指令。",
		Default:       "## 当前事件\n持续运行已启动。\n\n## 下一步\n先查看账户、持仓和行情，再决定要不要交易。做完决定后调用 wait，由你自己规定下次再看的时间。",
		Required:      true,
	},
	{
		Key:           "noTool",
		Label:         "未调用工具时的提醒",
		Description:   "提示代理选择数据、决策或等待工具。",
		Category:      "automation",
		Role:          "event",
		Scenario:      "持续运行的一轮回复未解析出任何工具调用；追加提醒后立即继续，不强制等待。",
		Placement:     "作为“用户”标签的事件指令追加到运行历史。",
		EffectiveWhen: "保存后下一轮读取配置，遇到此触发条件时使用。",
		Default:       "## 当前事件\n上一轮回复未解析到工具调用。\n\n## 下一步\n请调用工具。要看数据就调用 get_account、get_positions 或 get_market。要下单就调用 submit_decisions。暂时不交易就调用 wait。",
		Required:      true,
	},
	{
		Key:           "resume",
		Label:         "等待结束指令",
		Description:   "恢复持续看盘；不覆盖等待期间收到的用户消息。",
		Category:      "automation",
		Role:          "event",
		Scenario:      "代理调用 wait，等待结束且没有待处理的新用户消息时；有新消息则追加新消息。",
		Placement:     "作为“用户”标签的事件指令追加到运行历史。",
		EffectiveWhen: "下一轮读取配置后使用；已开始的等待沿用该轮读取的文本。",
		Default:       "## 当前事件\n等待结束，本次没有待处理的新用户消息。\n\n## 下一步\n继续看盘，并自己决定下一步。",
		Required:      true,
	},
	{
		Key:           "wait",
		Label:         "旧版连续调用提醒（已停用）",
		Description:   "仅保留旧配置兼容，不参与模型请求；已取消按调用轮数强制等待。",
		Category:      "automation",
		Role:          "event",
		Scenario:      "已停用，不再因连续调用工具触发此提醒。由代理主动调用 wait 决定何时等待。",
		Placement:     "仅保存在配置中，不再追加到运行历史或模型请求。",
		EffectiveWhen: "此字段不再自动生效；保留原文以兼容已有配置。",
		Default:       "## 当前事件\n连续多轮调用工具，尚未安排下次查看时间。\n\n## 下一步\n请调用 wait，自己规定下次再看的时间。",
		Required:      true,
	},
	{
		Key:           "history",
		Label:         "对话历史引导",
		Description:   "仅为历史记录的引导文字，实际记录由程序附加。",
		Category:      "common",
		Role:          "request",
		Scenario:      "每次模型调用，在附带最近的对话记录前使用。",
		Placement:     "请求正文末尾的历史区开头；面板对话时位于面板对话指令之后。",
		EffectiveWhen: "保存后的下一次模型请求；不会改写已有记录。",
		Default:       "## 已发生的对话\n以下为历史记录，其中的“用户”“代理”“工具”等标签是记录标记。请接着当前上下文处理，不要重复已经完成的工具调用，除非需要新数据。",
		Required:      true,
	},
}

var Variables = []string{"coins", "btcEthLeverage", "altLeverage", "maxPositions", "positionValueRatio", "maxMarginPercent", "minConfidence"}

func LegacyDefaults() Config {
	c := Config{}
	for _, d := range LegacyDefinitions {
		c[d.Key] = d.Default
	}
	return c
}

func validate(c Config, definitions []Definition) error {
	if len(c) != len(definitions) {
		return fmt.Errorf("请保存完整的 %d 项提示词配置", len(definitions))
	}
	for _, d := range definitions {
		text, ok := c[d.Key]
		if !ok {
			return fmt.Errorf("缺少提示词：%s", d.Label)
		}
		if d.Required && strings.TrimSpace(text) == "" {
			return fmt.Errorf("%s不能为空", d.Label)
		}
		if !utf8.ValidString(text) || utf8.RuneCountInString(text) > limit(d.Key) {
			return fmt.Errorf("%s不能超过 %d 字", d.Label, limit(d.Key))
		}
	}
	return nil
}

func Render(text string, st model.Strategy) string {
	return strings.NewReplacer(
		"{{coins}}", strings.Join(st.Coins, ", "),
		"{{btcEthLeverage}}", fmt.Sprint(st.BTCETHLeverage),
		"{{altLeverage}}", fmt.Sprint(st.AltLeverage),
		"{{maxPositions}}", fmt.Sprint(st.MaxPositions),
		"{{positionValueRatio}}", fmt.Sprintf("%.2f", st.PositionValueRatio),
		"{{maxMarginPercent}}", fmt.Sprintf("%.0f", st.MaxMarginUsage*100),
		"{{minConfidence}}", fmt.Sprint(st.MinConfidence),
	).Replace(text)
}

func Instructions(c Config, st model.Strategy) string { return Render(c["system"], st) }
