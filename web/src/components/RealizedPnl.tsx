import { useEffect, useState } from 'react'
import { api, type PnLSummary } from '../api'
import { usd } from '../format'

export function RealizedPnl({ enabled }: { enabled: boolean }) {
  const [period, setPeriod] = useState('day')
  const [data, setData] = useState<PnLSummary | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let dead = false
    let pending = false
    setData(null)
    setError('')
    const refresh = async () => {
      if (!enabled || pending) return
      pending = true
      try {
        const next = await api.pnl(period)
        if (!dead) { setData(next); setError('') }
      } catch (err) {
        if (!dead) { setData(null); setError(err instanceof Error ? err.message : '已实现盈亏读取失败') }
      } finally { pending = false }
    }
    void refresh()
    const timer = window.setInterval(refresh, 15000)
    return () => { dead = true; window.clearInterval(timer) }
  }, [period, enabled])
  const rows = [['已实现盈亏', data?.realized], ['交易手续费', data?.commission], ['资金费', data?.funding], ['净已实现收益', data?.net]] as const
  return <section className="card pnl-card" aria-label="已实现盈亏汇总">
    <div className="row gap wrap">
      <h2>已实现盈亏 · USDT</h2>
      <select aria-label="盈亏统计周期" value={period} onChange={event => setPeriod(event.target.value)}>
        <option value="day">今日（UTC+8）</option><option value="7d">近7天</option><option value="30d">近30天</option>
      </select>
    </div>
    {error && <p className="err" role="alert">{error}</p>}
    <div className="stat-row">{rows.map(([label, value]) => <div className="stat" key={label}><span>{label}</span><strong className={value == null ? '' : value < 0 ? 'down' : 'up'}>{value == null ? '—' : usd(value)}</strong></div>)}</div>
    <p className="sub">净已实现收益 = 已实现盈亏 + 手续费 + 资金费（按流水正负号计）。仅统计当前账户 USDT，不含充值、划转及未实现盈亏。{data ? ` 更新于 ${new Date(data.updatedAt).toLocaleTimeString('zh-CN', { hour12: false })}` : enabled && !error ? ' 正在读取…' : ''}</p>
  </section>
}
