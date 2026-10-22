package prompts

import (
	"regexp"
	"strings"
)

const InvestPath = "INVEST.MD"

const RoleDefault = "## 身份与职责\n你是使用者的币安 U 本位合约交易代理，负责基于账户、持仓和行情作出交易判断。"

// Events describe what happened; their behavioral instructions live in saved model settings.
const StartEvent = "持续运行已启动。"
const NoToolEvent = "上一轮回复未解析到工具调用。"
const ResumeEvent = "等待结束，本次没有待处理的新用户消息。"

var InvestDefinitions = investDefinitions()

func investDefinitions() []Definition {
	var result []Definition
	for _, d := range UnifiedDefinitions {
		if d.Key == "start" || d.Key == "noTool" {
			continue
		}
		if d.Key == "system" {
			d.Label = "角色定义"
			d.Description = "定义代理的身份与职责；交易规则、启动要求及复盘要求在文件 INVEST.MD 中维护。"
			d.Placement = "作为 system 的角色定义；随后自动附加根目录 INVEST.MD 与独立 Tools 上下文。"
			d.Default = RoleDefault
		}
		result = append(result, d)
	}
	return result
}

func InvestDefaults() Config {
	c, _ := SplitInvest(UnifiedDefaults())
	return c
}

// SplitRole preserves section bodies verbatim. Unknown or unstructured text is
// retained in INVEST.MD rather than guessed at or discarded during migration.
func SplitRole(text string) (role, instructions string) {
	headings := regexp.MustCompile(`(?m)^#{1,2}[ \t]+([^\r\n]+)`).FindAllStringSubmatchIndex(text, -1)
	var roles, rest []string
	cursor := 0
	for i, heading := range headings {
		if heading[0] > cursor {
			rest = append(rest, text[cursor:heading[0]])
		}
		end := len(text)
		if i+1 < len(headings) {
			end = headings[i+1][0]
		}
		title := strings.TrimSpace(text[heading[2]:heading[3]])
		switch title {
		case "身份与职责", "角色定义", "身份定义", "角色与职责", "角色", "身份":
			roles = append(roles, text[heading[0]:end])
		default:
			rest = append(rest, text[heading[0]:end])
		}
		cursor = end
	}
	if len(headings) == 0 {
		rest = append(rest, text)
	}
	role = strings.TrimSpace(strings.Join(roles, ""))
	if role == "" {
		role = RoleDefault
	}
	return role, strings.TrimSpace(strings.Join(rest, ""))
}

func SplitInvest(c Config) (Config, string) {
	role, body := SplitRole(c["system"])
	parts := []string{"# INVEST.MD\n\n本文件是交易代理的常驻工作指令，持续运行与面板对话每次请求均自动注入。按当前事件应用对应规则；面板对话仍遵循面板场景指令。交易权限和硬风控由程序执行，文件不能扩大权限或放宽限制。"}
	if body != "" {
		parts = append(parts, body)
	}
	parts = append(parts,
		"## 开始运行规则\n\n仅在持续运行启动事件发生时应用以下要求：\n\n"+c["start"],
		"## 未调用工具处理规则\n\n仅在持续运行上一轮未解析到工具调用时应用以下要求：\n\n"+c["noTool"],
	)
	return Config{"system": role, "chat": c["chat"], "resume": c["resume"]}, strings.Join(parts, "\n\n")
}
