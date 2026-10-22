import type { BinanceProfile } from './api'

export function tradingMode(settings: BinanceProfile | null) {
  if (!settings) return { id: 'loading', label: '读取模式中', className: 'pill' }
  // dryRun uses the live data endpoint but blocks orders. It is not live trading.
  if (settings.dryRun) return { id: 'paper', label: '模拟（不发单）', className: 'pill sim' }
  if (settings.testnet) return { id: 'demo', label: '币安模拟盘', className: 'pill sim' }
  return { id: 'live', label: '实盘', className: 'pill live' }
}
