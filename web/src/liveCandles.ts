import type { Candle } from './api'

export type ReceivedCandle = Candle & { receivedAt?: number }

export function validCandle(c: Candle): boolean {
  return [c.time, c.open, c.high, c.low, c.close, c.volume].every(Number.isFinite)
    && c.time > 0 && c.open > 0 && c.high > 0 && c.low > 0 && c.close > 0 && c.volume >= 0
}

// REST has no event timestamp. Do not let a delayed snapshot replace a newer
// stream tick, or a healthy stream's still-forming candle.
export function mergeCandles(current: ReceivedCandle[], incoming: ReceivedCandle[], restStartedAt = Infinity) {
  const byTime = new Map(current.map(c => [c.time, c]))
  for (const candle of incoming) {
    if (!validCandle(candle)) continue
    const old = byTime.get(candle.time)
    if (old?.source === 'websocket') {
      if (candle.source !== 'websocket' && (old.receivedAt ?? 0) > restStartedAt - 5000) continue
      if (candle.source === 'websocket' && (candle.updatedAt ?? 0) < (old.updatedAt ?? 0)) continue
    }
    byTime.set(candle.time, candle)
  }
  return [...byTime.values()].sort((a, b) => a.time - b.time)
}
