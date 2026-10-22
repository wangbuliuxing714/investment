package trader

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"desk/internal/market"
	"desk/internal/model"
)

type coinPack struct {
	Symbol   string
	Interval string
	Candles  []model.Candle
}

var beijingTime = time.FixedZone("UTC+08:00", 8*60*60)

func candleTime(seconds int64, zone *time.Location) string {
	return time.Unix(seconds, 0).In(zone).Format("2006-01-02 15:04:05")
}

func marketNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func formatCoin(pack coinPack) string {
	var b strings.Builder
	closes := make([]float64, len(pack.Candles))
	for i, c := range pack.Candles {
		closes[i] = c.Close
	}
	last := 0.0
	if n := len(closes); n > 0 {
		last = closes[n-1]
	}
	ema, macd, signal, hist, rsi := "N/A（至少20根）", "N/A（至少34根）", "N/A（至少34根）", "N/A（至少34根）", "N/A（至少8根）"
	if len(closes) >= 20 {
		ema = marketNumber(market.Last(market.EMA(closes, 20)))
	}
	if len(closes) >= 34 {
		m, s, h := market.MACD(closes)
		macd, signal, hist = marketNumber(m), marketNumber(s), marketNumber(h)
	}
	if len(closes) >= 8 {
		rsi = fmt.Sprintf("%.2f", market.RSI(closes, 7))
	}
	fmt.Fprintf(&b, "%s %s 本页末根收盘价 %s EMA20 %s MACD(12,26,9) %s 信号 %s 柱 %s RSI7 %s\n", pack.Symbol, pack.Interval, marketNumber(last), ema, macd, signal, hist, rsi)
	b.WriteString("指标仅基于本页数据；N/A 表示样本不足，不是零值或交易信号。最新页末根可能尚未收盘。\n")
	b.WriteString("表格为每根K线的开盘时间（UTC），不是当天市场开盘时间；引用时间请注明时区，北京时间为 UTC+8。\n")
	b.WriteString("OpenTime(UTC) Open High Low Close Volume\n")
	for i, c := range pack.Candles {
		line := fmt.Sprintf("%s %s %s %s %s %s",
			candleTime(c.Time, time.UTC), marketNumber(c.Open), marketNumber(c.High), marketNumber(c.Low), marketNumber(c.Close), marketNumber(c.Volume))
		if i == len(pack.Candles)-1 {
			line += "  <- 本页最后一根"
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
