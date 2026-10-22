package trader

import (
	"context"
	"math"
	"strings"

	"desk/internal/ai"
	"desk/internal/binance"
	"desk/internal/model"
	"desk/internal/store"
)

type agentClient interface {
	executionClient
	marketReader
	Account(context.Context) (model.Account, error)
	Positions(context.Context) ([]model.Position, error)
	Hedge(context.Context) (bool, error)
}

var _ agentClient = (*binance.Client)(nil)

type agentState struct {
	client       agentClient
	marketSource marketReader
	strategy     model.Strategy
	service      *Service
	decisionID   int64
	paper        bool
	account      model.Account
	positions    []model.Position
	marks        map[string]float64
	marginUsed   float64
	hedge        bool
	hedgeKnown   bool
	logs         []string
	decisions    []model.Decision
}

func (a *agentState) refresh(ctx context.Context) error {
	account, err := a.client.Account(ctx)
	if err != nil {
		return err
	}
	positions, err := a.client.Positions(ctx)
	if err != nil {
		return err
	}
	a.account = account
	a.positions = positions
	a.marginUsed = account.MarginUsed
	if a.marks == nil {
		a.marks = map[string]float64{}
	}
	for _, p := range positions {
		if p.Mark > 0 {
			a.marks[p.Symbol] = p.Mark
		}
	}
	return nil
}

func (a *agentState) call(ctx context.Context, call ai.ToolCall) string {
	if reason := a.modeDenial(call.Name, call.Arguments); reason != "" {
		return reason
	}
	if err := ctx.Err(); err != nil {
		return err.Error()
	}
	if store.IsFileTool(call.Name) {
		return a.fileTool(call.Name, call.Arguments)
	}
	if readOnlyTool(call.Name) {
		result := a.readTool(ctx, toolCall{Name: call.Name, Args: call.Arguments})
		a.applyRead(result)
		return result.text
	}
	switch call.Name {
	case "submit_decisions":
		return a.submit(ctx, call.Arguments)
	case "set_protection":
		return a.protect(ctx, call.Arguments)
	default:
		return "没有这个工具"
	}
}

func (a *agentState) submit(ctx context.Context, args string) string {
	if reason := a.modeDenial("submit_decisions", args); reason != "" {
		return reason
	}
	_, decisions, err := ParseResponse(args)
	if err != nil {
		return "决定无法解析，没有下单：" + err.Error()
	}
	if len(decisions) == 0 {
		return "没有收到交易决定"
	}
	// Each cloud reply creates a fresh agentState. Conversation history is not
	// executable account state; always reload it before evaluating real actions.
	needsExecution := false
	for _, d := range decisions {
		if tradable(d.Action) && allowedSymbol(a.strategy, d.Symbol) {
			needsExecution = true
			break
		}
	}
	if needsExecution {
		if err := a.refresh(ctx); err != nil {
			return "下单前刷新账户和持仓失败，本批次未提交订单：" + err.Error()
		}
	}
	if needsExecution && !a.hedgeKnown {
		hedge, err := a.client.Hedge(ctx)
		if err != nil {
			return "读取持仓模式失败，本批次未提交订单：" + err.Error()
		}
		a.hedge = hedge
		a.hedgeKnown = true
	}
	a.decisions = append(a.decisions, decisions...)
	var lines []string
	for _, d := range orderDecisions(decisions) {
		mark := a.marks[d.Symbol]
		plan := Apply(a.strategy, a.account.Equity, a.marginUsed, a.positions, mark, d)
		if plan.Skip == "" && (d.Action == "open_long" || d.Action == "open_short") && !a.paper {
			// A previous get_market may be in another turn or contain old candles.
			// Fetch a current price from the same exchange environment for sizing.
			candles, err := a.client.KlinesBefore(ctx, d.Symbol, "1m", 1, 0)
			if err != nil || len(candles) == 0 {
				line := d.Symbol + " 开仓前获取最新价格失败，未下单"
				if err != nil {
					line += "：" + err.Error()
				}
				lines = append(lines, line)
				a.logs = append(a.logs, line)
				continue
			}
			mark = candles[len(candles)-1].Close
			if mark <= 0 || math.IsNaN(mark) || math.IsInf(mark, 0) {
				line := d.Symbol + " 开仓前返回的最新价格无效，未下单"
				lines = append(lines, line)
				a.logs = append(a.logs, line)
				continue
			}
			a.marks[d.Symbol] = mark
			plan = Apply(a.strategy, a.account.Equity, a.marginUsed, a.positions, mark, d)
		}
		line, next, executed := executePlan(ctx, a.client, a.hedge, a.positions, mark, plan, a.paper)
		lines = append(lines, line)
		a.logs = append(a.logs, line)
		a.positions = next
		if executed {
			// Refresh released/used margin before another decision in this batch.
			if err := a.refresh(ctx); err != nil {
				line := "该笔订单已执行，但更新账户和持仓失败；本批次后续决定未执行，请先核对订单与持仓，勿重复提交：" + err.Error()
				lines = append(lines, line)
				a.logs = append(a.logs, line)
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

func toolLabel(name string) string {
	switch name {
	case "list_files":
		return "列出文件"
	case "read_file":
		return "读取 Markdown"
	case "write_file":
		return "保存 Markdown"
	case "create_directory":
		return "创建文件夹"
	case "get_indicators":
		return "连续技术指标"
	case "get_trading_history":
		return "成交与收益"
	case "get_funding":
		return "资金费率"
	case "get_open_interest":
		return "合约持仓量"
	case "get_order_flow":
		return "主动买卖量"
	case "get_order_book":
		return "盘口与滑点"
	case "get_account":
		return "查看账户"
	case "get_positions":
		return "查看持仓"
	case "get_market":
		return "查看行情"
	case "submit_decisions":
		return "提交下单"
	case "set_protection":
		return "设置止盈止损"
	case "wait":
		return "等待"
	default:
		return name
	}
}
