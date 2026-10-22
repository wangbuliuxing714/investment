package prompts

import (
	_ "embed"
	"regexp"
	"strings"
)

// These templates initialize saved, editable settings once. Runtime assembly
// must use Config, never these defaults.
//
//go:embed defaults/common.md
var CommonDefault string

//go:embed defaults/trading.md
var TradingDefault string

//go:embed defaults/research.md
var ResearchDefault string

var Definitions = modeDefinitions()

func modeDefinitions() []Definition {
	defs := []Definition{
		{Key: "system", Label: "系统提示词", Description: "两种模式共用：身份、文件修改、记录、输出和工具使用规则。这里不放具体买卖策略。", Category: "system", Role: "system", Scenario: "交易和策略优化的每次请求。", Placement: "系统上下文的第一部分。", EffectiveWhen: "保存后的下一次模型请求。", Default: CommonDefault, Required: true},
		{Key: "trading", Label: "交易模式指令", Description: "只在策略优化开关关闭时加载：查账户、处理持仓、执行交易及交易复盘。", Category: "common", Role: "system", Scenario: "开关关闭：交易模式，持续运行和面板对话。", Placement: "通用系统提示词之后，仅加载本模式。", EffectiveWhen: "保存或切换模式后的下一次模型请求。", Default: TradingDefault, Required: true},
		{Key: "optimization", Label: "策略优化模式指令", Description: "只在策略优化开关开启时加载：按策略验证、记录问题、修正和复测。", Category: "common", Role: "system", Scenario: "开关开启：策略优化模式，持续运行和面板对话。", Placement: "通用系统提示词之后，仅加载本模式。", EffectiveWhen: "保存或切换模式后的下一次模型请求。", Default: ResearchDefault, Required: true},
	}
	for _, d := range InvestDefinitions {
		if d.Key == "chat" || d.Key == "resume" {
			d.Default = InvestDefaults()[d.Key]
			if d.Key == "resume" {
				d.Default = "## 当前事件\n" + ResumeEvent
			}
			defs = append(defs, d)
		}
	}
	return defs
}

func Defaults() Config {
	c := Config{}
	for _, d := range Definitions {
		c[d.Key] = d.Default
	}
	return c
}

// Separate known instruction sections without rephrasing strategy bodies.
// Unknown/custom sections stay intact in the strategy document for review.
// Headings inside fenced examples are never interpreted as document sections.
func SeparateInstructions(content string) (strategy, common, trading, research string) {
	lines := strings.SplitAfter(content, "\n")
	heading := regexp.MustCompile(`^(#{1,6})[ \t]+([^\r\n]+)`)
	dest, depth, fence := "strategy", 0, ""
	parts := map[string]*strings.Builder{}
	for _, key := range []string{"strategy", "common", "trading", "research"} {
		parts[key] = &strings.Builder{}
	}
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			if fence == "" {
				fence = trim[:3]
			} else if strings.HasPrefix(trim, fence) {
				fence = ""
			}
			if b := parts[dest]; b != nil {
				b.WriteString(line)
			}
			continue
		}
		if fence == "" {
			if m := heading.FindStringSubmatch(line); m != nil {
				level, title := len(m[1]), strings.TrimSpace(m[2])
				if level <= depth {
					dest, depth = "strategy", 0
				}
				switch title {
				case "文件怎么改", "通用要求和工具用法", "版本和每次问题怎么记", "通用：保持工作区简洁", "行为规则", "行为边界", "输出规范", "历史处理规则", "工具协议", "Markdown 工作区工具", "本地历史行情库":
					dest, depth = "common", level
				case "没机会时怎么说", "原有过程评分继续保留", "交易记录怎么维护", "文件记忆与逐次复盘责任", "开始运行规则", "未调用工具处理规则":
					dest, depth = "trading", level
				case "回测前还缺什么", "研究流程", "权限与记录", "等待结束规则", "面板对话规则":
					dest, depth = "research", level
				case "两种模式":
					dest, depth = "discard", level
				case "策略和做事规则":
					parts["strategy"].WriteString("# INVEST.MD\n\n")
					dest, depth = "discard", 6
					continue
				}
			}
		}
		if strings.HasPrefix(trim, "- 连续运行启动或恢复：交易先查") {
			continue
		}
		if dest == "strategy" && (strings.HasPrefix(trim, "- 条件全部满足、权限允许时及时提交") || strings.HasPrefix(trim, "- 提交不等于成交") || strings.HasPrefix(trim, "- 成交后检查交易所里真实存在")) {
			parts["trading"].WriteString(line)
			continue
		}
		if b := parts[dest]; b != nil {
			b.WriteString(line)
		}
	}
	return strings.TrimSpace(parts["strategy"].String()), strings.TrimSpace(parts["common"].String()), strings.TrimSpace(parts["trading"].String()), strings.TrimSpace(parts["research"].String())
}
