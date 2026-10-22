export interface MarketBlock {
  symbol: string
  timeframe: string
  stats: { label: string; value: string }[]
  rows: string[][]
  notes: string[]
}

// Accept current and persisted legacy tool results. Keep N/A, precise prices,
// second-resolution times, metadata, and the final candle marker intact.
export function parseMarket(text: string): MarketBlock[] {
  const head = /^([A-Z0-9]+)\s+(\S+)\s+(最新价|本页末根收盘价)\s+(\S+)\s+EMA20\s+(\S+)\s+MACD(?:\(12,26,9\))?\s+(\S+)\s+信号\s+(\S+)\s+柱\s+(\S+)\s+RSI7\s+(\S+)/
  const candle = /^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}(?::\d{2})?)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)(?:\s+<-\s*(?:current|本页最后一根))?$/
  const blocks: MarketBlock[] = []
  let active: MarketBlock | undefined
  let pending: string[] = []
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line) continue
    if (line.startsWith('symbol=')) {
      active = undefined
      pending.push(line)
      continue
    }
    const match = line.match(head)
    if (match) {
      active = {
        symbol: match[1], timeframe: match[2], rows: [], notes: pending,
        stats: ['本页末根收盘价', 'EMA20', 'MACD', '信号', '柱', 'RSI7'].map((label, i) => ({ label, value: match[i + 4] })),
      }
      pending = []
      blocks.push(active)
      continue
    }
    if (/^(?:OpenTime|Time)\(UTC\)/.test(line)) continue
    const row = line.match(candle)
    if (active && row) {
      active.rows.push([...row.slice(1, 7), line.includes('<-') ? '1' : ''])
    } else if (active) {
      active.notes.push(line)
    } else {
      pending.push(line)
    }
  }
  // A later symbol may have failed while earlier symbols returned data.
  if (pending.length && blocks.length) blocks[blocks.length - 1].notes.push(...pending)
  return blocks
}
