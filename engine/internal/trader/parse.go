package trader

import (
	"encoding/json"
	"strings"

	"desk/internal/model"
)

func ParseResponse(raw string) (reasoning string, decisions []model.Decision, err error) {
	raw = strings.TrimSpace(raw)
	block, prose := extractJSON(raw)
	if prose != "" {
		reasoning = strings.TrimSpace(prose)
	}
	if block == "" {
		if reasoning == "" {
			reasoning = raw
		}
		return reasoning, nil, nil
	}
	var payload any
	if err := json.Unmarshal([]byte(block), &payload); err != nil {
		return reasoning, nil, err
	}
	switch v := payload.(type) {
	case []any:
		decisions = decodeList(v)
	case map[string]any:
		if text := asString(v["reasoning"]); text != "" {
			if reasoning != "" {
				reasoning = reasoning + "\n" + text
			} else {
				reasoning = text
			}
		}
		if text := asString(v["analysis"]); text != "" && !strings.Contains(reasoning, text) {
			if reasoning != "" {
				reasoning += "\n" + text
			} else {
				reasoning = text
			}
		}
		if list, ok := v["decisions"].([]any); ok {
			decisions = decodeList(list)
		} else if _, ok := v["action"]; ok {
			decisions = decodeList([]any{v})
		}
	}
	return reasoning, decisions, nil
}

func extractJSON(raw string) (block, prose string) {
	const fence = "```"
	if i := strings.Index(raw, fence); i >= 0 {
		prose = strings.TrimSpace(raw[:i])
		rest := raw[i+len(fence):]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			head := strings.TrimSpace(rest[:nl])
			if !strings.HasPrefix(head, "{") && !strings.HasPrefix(head, "[") {
				rest = rest[nl+1:]
			}
		}
		if j := strings.Index(rest, fence); j >= 0 {
			return strings.TrimSpace(rest[:j]), prose
		}
		return strings.TrimSpace(rest), prose
	}
	startObj := strings.Index(raw, "{")
	startArr := strings.Index(raw, "[")
	start := -1
	closer := byte('}')
	if startObj >= 0 && (startArr < 0 || startObj < startArr) {
		start = startObj
	} else if startArr >= 0 {
		start = startArr
		closer = ']'
	}
	if start < 0 {
		return "", raw
	}
	end := strings.LastIndexByte(raw, closer)
	if end <= start {
		return "", raw
	}
	return strings.TrimSpace(raw[start : end+1]), strings.TrimSpace(raw[:start])
}

func decodeList(list []any) []model.Decision {
	out := make([]model.Decision, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		action := normalizeAction(firstString(m, "action", "decision"))
		if action == "" {
			continue
		}
		symbol := strings.ToUpper(strings.TrimSpace(firstString(m, "symbol", "coin")))
		out = append(out, model.Decision{
			Symbol:          symbol,
			Action:          action,
			Leverage:        int(firstFloat(m, "leverage")),
			PositionSizeUSD: firstFloat(m, "position_size_usd", "positionSizeUsd", "notional", "size_usd"),
			StopLoss:        firstFloat(m, "stop_loss", "stopLoss"),
			TakeProfit:      firstFloat(m, "take_profit", "takeProfit"),
			Confidence:      int(firstFloat(m, "confidence")),
			Reasoning:       firstString(m, "reasoning", "reason"),
		})
	}
	return out
}

func normalizeAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long", "openlong", "long", "buy_long", "开多", "做多":
		return "open_long"
	case "open_short", "openshort", "short", "开空", "做空":
		return "open_short"
	case "close_long", "closelong", "平多":
		return "close_long"
	case "close_short", "closeshort", "平空":
		return "close_short"
	case "hold", "wait", "观望", "持有", "等待":
		return "hold"
	default:
		return ""
	}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := asString(m[k]); s != "" {
			return s
		}
	}
	return ""
}

func firstFloat(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return v
		case json.Number:
			f, _ := v.Float64()
			return f
		case string:
			var f float64
			if json.Unmarshal([]byte(v), &f) == nil {
				return f
			}
		}
	}
	return 0
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func actionLabel(action string) string {
	switch action {
	case "open_long":
		return "开多"
	case "open_short":
		return "开空"
	case "close_long":
		return "平多"
	case "close_short":
		return "平空"
	default:
		return "观望"
	}
}
