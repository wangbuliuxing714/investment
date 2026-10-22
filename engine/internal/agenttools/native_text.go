package agenttools

import "strings"

// One-time conversion of known obsolete protocol sentences. User strategy and
// custom sections are preserved verbatim; this is not injected at request time.
func NativeProtocolText(text string) string {
	return strings.NewReplacer(
		"每次先用中文说明你在做什么，再给出工具调用。工具放在 ```tool 代码块里，内容是 JSON：{\"name\":\"工具名\",\"arguments\":{}}", "先用中文简要说明，再通过原生工具调用执行。英文工具名和参数以本轮工具定义为准；正文里的 JSON 或代码块不会执行。",
		"先用简短中文说明，再用 tool JSON 代码块调用：`{\"name\":\"工具名\",\"arguments\":{}}`。独立查询可一起发，需要上次结果的就按顺序。", "先用简短中文说明，再通过原生工具调用执行。独立查询可一起发，需要上次结果的就按顺序；正文代码块不会执行。",
		"工具的 name 用 Tools 中的英文标识，不能用中文展示名称；每个 tool JSON 代码块只放一个调用对象。", "通过原生工具调用执行，英文名称和参数以本轮工具定义为准；正文代码块不会执行。",
		"同一条回复可以输出多个独立的 tool JSON 代码块，每个代码块仍是一份 {\"name\":\"工具名\",\"arguments\":{}} 对象。", "同一轮可以提交多个原生工具调用。",
		"然后在同一条回复中分别给出四个 tool 代码块：", "然后在同一轮提交四个原生调用：",
		"以下调用使用现有 tool JSON 协议。", "以下参数通过原生工具调用提交。",
		"持续运行上轮没有工具调用时，有步骤就调用对应工具；做完或卡住就 wait，不空转。", "持续运行需要再检查时用 wait 安排时间；直接回复结束表示本轮完成，代理停止，不空转。",
		"- 上轮没有调用工具：有步骤就调用对应工具；做完或卡住就用 wait 等待，不空转。", "- 连续运行需要再检查时用 wait 安排时间；直接回复结束表示本轮完成，代理停止，不空转。",
	).Replace(text)
}
