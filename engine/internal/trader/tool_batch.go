package trader

import (
	"context"
	"fmt"
	"strings"
	"time"

	"desk/internal/ai"
	"desk/internal/binance"
	"desk/internal/model"
	"desk/internal/store"
)

// Bound simultaneous HTTP work, not the number of tools or analysis rounds.
const maxParallelReads = 8

func readOnlyTool(name string) bool {
	return name == "get_history" || name == "get_account" || name == "get_positions" || name == "get_market" || binance.IsAnalyticsTool(name)
}

type toolRead struct {
	text      string
	account   *model.Account
	positions *[]model.Position
	marks     map[string]float64
}

// Workers never mutate the shared execution state. Snapshots are merged on
// the caller goroutine, in request order, before any subsequent order.
func (a *agentState) readTool(ctx context.Context, call toolCall) (result toolRead) {
	if reason := a.modeDenial(call.Name, call.Args); reason != "" {
		return toolRead{text: reason}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = toolRead{text: fmt.Sprintf("读取工具失败：%v", recovered)}
		}
	}()
	if err := ctx.Err(); err != nil {
		return toolRead{text: err.Error()}
	}
	if binance.IsAnalyticsTool(call.Name) {
		return toolRead{text: a.analytics(ctx, call)}
	}
	switch call.Name {
	case "get_history":
		return toolRead{text: a.localHistory(ctx, call.Args)}
	case "get_account":
		account, err := a.client.Account(ctx)
		if err != nil {
			return toolRead{text: err.Error()}
		}
		return toolRead{account: &account, text: fmt.Sprintf("净值 %.2f 钱包 %.2f 可用 %.2f 未实现盈亏 %.2f 已用保证金 %.2f",
			account.Equity, account.Wallet, account.Available, account.Unrealized, account.MarginUsed)}
	case "get_positions":
		var positions []model.Position
		var err error
		if source, ok := a.client.(interface {
			PositionsWithProtection(context.Context) ([]model.Position, error)
		}); ok {
			positions, err = source.PositionsWithProtection(ctx)
		} else {
			positions, err = a.client.Positions(ctx)
		}
		if err != nil {
			return toolRead{text: err.Error()}
		}
		result := toolRead{positions: &positions, marks: map[string]float64{}, text: "无持仓"}
		var b strings.Builder
		for _, p := range positions {
			fmt.Fprintf(&b, "%s %s 数量 %.6f 开仓价 %.4f 标记价 %.4f 盈亏 %.2f 杠杆 %d\n",
				p.Symbol, p.Side, p.Amount, p.Entry, p.Mark, p.PnL, p.Leverage)
			if p.ProtectionKnown {
				stop, take := "未设置", "未设置"
				for _, line := range p.Protection {
					if line.Kind == "stop" {
						stop = marketNumber(line.Price)
					}
					if line.Kind == "take" {
						take = marketNumber(line.Price)
					}
				}
				fmt.Fprintf(&b, "保护单 %s %s：止损 %s；止盈 %s（交易所查询结果）。缺少保护时使用 set_protection 补挂，不要重复开仓。\n", p.Symbol, p.Side, stop, take)
			} else if p.ProtectionError != "" {
				fmt.Fprintf(&b, "保护单 %s：读取失败，状态未知：%s\n", p.Symbol, p.ProtectionError)
			}
			if p.Mark > 0 {
				result.marks[p.Symbol] = p.Mark
			}
		}
		if len(positions) > 0 {
			result.text = b.String()
		}
		return result
	case "get_market":
		local := &agentState{client: a.client, marketSource: a.marketSource, strategy: a.strategy}
		text := local.market(ctx, call.Args)
		return toolRead{text: text, marks: local.marks}
	default:
		return toolRead{text: "不是只读工具"}
	}
}

func (a *agentState) applyRead(result toolRead) {
	if result.account != nil {
		a.account = *result.account
		a.marginUsed = result.account.MarginUsed
	}
	if result.positions != nil {
		a.positions = *result.positions
	}
	if len(result.marks) > 0 && a.marks == nil {
		a.marks = map[string]float64{}
	}
	for symbol, price := range result.marks {
		a.marks[symbol] = price
	}
}

func (a *agentState) readBatch(ctx context.Context, calls []toolCall, started func(toolCall), finished func(toolCall, string)) []memory {
	if len(calls) == 0 || ctx.Err() != nil {
		return nil
	}
	type completion struct {
		index int
		read  toolRead
	}
	jobs := make(chan int, len(calls))
	done := make(chan completion, len(calls))
	pending := map[string][]int{}
	for i, call := range calls {
		started(call)
		pending[call.Name] = append(pending[call.Name], i)
		jobs <- i
	}
	close(jobs)
	for worker := 0; worker < min(len(calls), maxParallelReads); worker++ {
		go func() {
			for i := range jobs {
				done <- completion{index: i, read: a.readTool(ctx, calls[i])}
			}
		}()
	}
	results := make([]toolRead, len(calls))
	ready := make([]bool, len(calls))
	for range calls {
		completed := <-done
		results[completed.index], ready[completed.index] = completed.read, true
		name := calls[completed.index].Name
		// Persisted clients pair by tool name FIFO. Publish same-name results
		// in request order even when their network responses arrive reversed.
		for len(pending[name]) > 0 && ready[pending[name][0]] {
			i := pending[name][0]
			finished(calls[i], results[i].text)
			pending[name] = pending[name][1:]
		}
	}
	mem := make([]memory, 0, len(calls))
	for i, result := range results {
		a.applyRead(result)
		mem = append(mem, memory{Role: "工具 " + toolLabel(calls[i].Name) + " " + calls[i].Args, Text: result.text})
	}
	return mem
}

// Only contiguous read-only calls share a batch. Decisions and explicit wait
// calls are barriers; no read can cross an order and observe the wrong state.
func (s *Service) runToolCalls(ctx context.Context, state *agentState, calls []toolCall, automatic bool) ([]memory, time.Duration, string) {
	var mem []memory
	var waited time.Duration
	var reason string
	started := func(call toolCall) {
		args := shortArgs(call.Args)
		if store.IsFileTool(call.Name) {
			args = strings.TrimSpace(call.Args)
		}
		s.say("tool", call.Name, strings.TrimSpace(toolLabel(call.Name)+" "+args))
	}
	finished := func(call toolCall, result string) { s.say("result", call.Name, result) }
	for i := 0; i < len(calls); {
		if ctx.Err() != nil || (!automatic && s.isRunning()) {
			break
		}
		call := calls[i]
		if reason := state.modeDenial(call.Name, call.Args); reason != "" {
			started(call)
			finished(call, reason)
			mem = append(mem, memory{Role: "工具 " + call.Name, Text: reason})
			i++
			continue
		}
		if readOnlyTool(call.Name) {
			end := i + 1
			for end < len(calls) && readOnlyTool(calls[end].Name) && state.modeDenial(calls[end].Name, calls[end].Args) == "" {
				end++
			}
			mem = append(mem, state.readBatch(ctx, calls[i:end], started, finished)...)
			i = end
			continue
		}
		i++
		if call.Name == "wait" {
			if !automatic {
				started(call)
				text := `{"error":"wait_unavailable_in_panel","executed":false,"scheduled":false}`
				finished(call, text)
				mem = append(mem, memory{Role: "工具 wait " + call.Args, Text: text})
				continue
			}
			waited, reason = waitDuration(call.Args)
			text := "等待 " + waitLabel(waited, reason)
			s.say("tool", call.Name, text)
			result := fmt.Sprintf(`{"scheduled":true,"seconds":%d,"reason":%q}`, int(waited.Seconds()), reason)
			finished(call, result)
			mem = append(mem, memory{Role: "工具", Text: result})
			continue
		}
		started(call)
		result := state.call(ctx, ai.ToolCall{Name: call.Name, Arguments: call.Args})
		finished(call, result)
		mem = append(mem, memory{Role: "工具 " + toolLabel(call.Name) + " " + shortArgs(call.Args), Text: result})
	}
	return mem, waited, reason
}

// Keep the entire current batch in the model's recent-turn window, including
// batches larger than the legacy eight-memory-item history window.
func toolBatchMemory(results []memory) memory {
	var b strings.Builder
	for _, result := range results {
		fmt.Fprintf(&b, "[%s]\n%s\n\n", result.Role, result.Text)
	}
	return memory{Role: "本轮工具执行结果", Text: strings.TrimSpace(b.String())}
}
