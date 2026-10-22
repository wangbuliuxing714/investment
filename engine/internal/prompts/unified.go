package prompts

import (
	"desk/internal/model"
	"fmt"
	"regexp"
	"strings"
)

// UnifiedDefinitions describes the previous five-field format, for migration only.
var UnifiedDefinitions = managedDefinitions()

func managedDefinitions() []Definition {
	var result []Definition
	defaults := UnifiedDefaults()
	for _, d := range LegacyDefinitions {
		if d.Key == "trading" || d.Key == "strategy" || d.Key == "history" || d.Key == "wait" {
			continue
		}
		if d.Key == "system" {
			d.Description = "每次请求共用的身份、交易规则、自定义偏好与历史处理规则。"
			d.Placement = "渲染策略参数后，和独立管理的 Tools 上下文一起作为 system 指令发送。"
			d.EffectiveWhen = "下一次模型请求；策略参数或 Tools 配置变化也会刷新系统上下文。"
		}
		d.Default = defaults[d.Key]
		result = append(result, d)
	}
	return result
}

func limit(key string) int {
	if key == "system" {
		return 120000
	}
	return 30000
}
func Validate(c Config) error { return validate(c, Definitions) }

// ValidateStored supports old documents solely while migrating, never for API writes.
func ValidateStored(c Config) error {
	if _, ok := c["optimization"]; ok {
		return Validate(c)
	}
	if len(c) == len(InvestDefinitions) {
		return validate(c, InvestDefinitions)
	}
	if len(c) == len(LegacyDefinitions) {
		return validate(c, LegacyDefinitions)
	}
	if len(c) == len(UnifiedDefinitions) {
		return validate(c, UnifiedDefinitions)
	}
	return Validate(c)
}

func UnifiedDefaults() Config {
	c, _ := Consolidate(LegacyDefaults())
	const marker = "## SEPA / VCP 冠军方法启发策略"
	if len(Presets) > 0 && !strings.Contains(c["system"], marker) {
		c["system"] = strings.TrimSpace(c["system"]) + "\n\n" + marker + "\n" + strings.TrimSpace(Presets[0].Prompt)
	}
	c["system"] = WithReviews(c["system"])
	return c
}

// ExtractDecisionProtocol moves this section without rewriting user-authored text.
func ExtractDecisionProtocol(text string) (string, string) {
	heading := regexp.MustCompile(`(?m)^##[ \t]+决策输出协议[^\r\n]*\r?$`)
	span := heading.FindStringIndex(text)
	if span == nil {
		return text, ""
	}
	end := len(text)
	if next := regexp.MustCompile(`(?m)^##[ \t]+`).FindStringIndex(text[span[1]:]); next != nil {
		end = span[1] + next[0]
	}
	return text[:span[0]] + text[end:], strings.TrimSpace(text[span[0]:end])
}

func Consolidate(c Config) (Config, string) {
	system, systemProtocol := ExtractDecisionProtocol(c["system"])
	if c["system"] == LegacyDefaults()["system"] {
		system = strings.ReplaceAll(system, "请求正文", "系统上下文")
	}
	trading, protocol := ExtractDecisionProtocol(c["trading"])
	parts := []string{system}
	if trading != "" {
		parts = append(parts, "## 交易规则\n"+trading)
	}
	if c["strategy"] != "" {
		parts = append(parts, "## 自定义交易说明\n"+c["strategy"])
	}
	if c["history"] != "" {
		history := c["history"]
		if history == LegacyDefaults()["history"] {
			history = "请求正文携带实际对话历史，其中的“用户”“代理”“工具”等标签是记录标记。请接着当前上下文处理，不要重复已经完成的工具调用，除非需要新数据。"
		}
		parts = append(parts, "## 历史处理规则\n"+history)
	}
	next := Config{"system": strings.Join(parts, "\n\n")}
	for _, key := range []string{"chat", "start", "noTool", "resume"} {
		next[key] = c[key]
	}
	if systemProtocol != "" {
		if protocol != "" {
			protocol += "\n\n"
		}
		protocol += systemProtocol
	}
	return next, protocol
}

type Parameter struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

func Parameters(st model.Strategy) []Parameter {
	return []Parameter{
		{"coins", "交易币种", strings.Join(st.Coins, ", "), ""},
		{"btcEthLeverage", "BTC / ETH 杠杆", fmt.Sprint(st.BTCETHLeverage), "倍"},
		{"altLeverage", "其他币种杠杆", fmt.Sprint(st.AltLeverage), "倍"},
		{"maxPositions", "最多持仓", fmt.Sprint(st.MaxPositions), "个"},
		{"positionValueRatio", "单笔名义价值上限", fmt.Sprintf("%.2f", st.PositionValueRatio), "倍净值"},
		{"maxMarginPercent", "保证金占用上限", fmt.Sprintf("%.0f", st.MaxMarginUsage*100), "%"},
		{"minConfidence", "最低把握", fmt.Sprint(st.MinConfidence), ""},
	}
}
