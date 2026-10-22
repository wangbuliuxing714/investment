import { useEffect, useState } from 'react'
import { api, type HistoryDataset, type HistoryGap } from '../api'
import { HistoryChart } from '../components/HistoryChart'
import './history.css'

const intervals = ['1m', '3m', '5m', '15m', '30m', '1h', '2h', '4h', '6h', '8h', '12h', '1d', '3d', '1w', '1M']
const utc = (s: number) => s ? new Date(s * 1000).toISOString().replace('T', ' ').slice(0, 16) : '待下载'
const names: Record<string, string> = { pending: '等待下载', syncing: '正在同步', ready: '已完成同步', error: '同步中断，可继续' }

export function History() {
  const [market, setMarket] = useState<'spot' | 'um'>('spot')
  const [datasets, setDatasets] = useState<HistoryDataset[]>([])
  const [gaps, setGaps] = useState<Record<string, HistoryGap[]>>({})
  const [interval, setIntervalValue] = useState('1w')
  const [overview, setOverview] = useState(true)
  const [date, setDate] = useState('')
  const [end, setEnd] = useState(0)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    let dead = false
    const load = () => api.history().then(data => { if (!dead) { setDatasets(data.datasets); setGaps(data.gaps); setError('') } }).catch(e => { if (!dead) setError(e.message) })
    void load()
    const timer = window.setInterval(load, 5000)
    return () => { dead = true; window.clearInterval(timer) }
  }, [])
  const d = datasets.find(row => row.market === market)
  function latest() { setEnd(0); setOverview(false); setRefresh(v => v + 1) }
  function first() {
    if (!d?.first) return
    const units: Record<string, number> = { m: 60, h: 3600, d: 86400, w: 604800 }
    const t = new Date(d.first * 1000)
    if (interval === '1M') t.setUTCMonth(t.getUTCMonth() + 160)
    else t.setTime(t.getTime() + Number.parseInt(interval) * units[interval.slice(-1)] * 160000)
    setEnd(Math.floor(t.getTime() / 1000)); setOverview(false); setRefresh(v => v + 1)
  }
  return <div className="page history-page">
    <header><p className="eyebrow">MARKET HISTORY</p><h1>历史行情</h1><p>BTCUSDT 完整历史库 · 管理员图表、AI 研究与后续回测共用同一份数据</p></header>
    <div className="history-markets" role="group" aria-label="行情市场">{(['spot', 'um'] as const).map(m => <button key={m} className={m === market ? 'primary' : ''} aria-pressed={m === market} onClick={() => { setMarket(m); setEnd(0); setDate(''); setMessage('') }}>{m === 'spot' ? 'BTC 现货' : 'BTC USDT 永续'}</button>)}</div>
    {error && <p role="alert" className="down">{error}</p>}
    {d && <section className="history-card" aria-label="历史库覆盖状态">
      <div className="history-status"><strong>{names[d.status] || d.status}</strong><span>{d.rows.toLocaleString()} 根原始 1 分钟 K 线</span><span>月归档 {d.archivesDone} / {d.archivesTotal || '待确认'}</span>
        <button onClick={() => { setMessage('正在请求同步…'); api.syncHistory(market).then(r => setMessage(r.started ? '已开始同步，进度会自动更新。' : '同步正在进行。')).catch(e => setMessage(e.message)) }}>{d.status === 'error' ? '继续下载' : '同步最新数据'}</button></div>
      <progress max={d.archivesTotal || 1} value={d.archivesDone} aria-label="月归档进度" />
      <dl><div><dt>本地覆盖 · UTC</dt><dd>{utc(d.first)} → {utc(d.last)}</dd></div><div><dt>交易所可用起点 · UTC</dt><dd>{utc(d.availableFirst)}</dd></div><div><dt>缺口检查</dt><dd>{d.syncedThrough ? `${d.gapCount} 处 / ${d.missingMinutes.toLocaleString()} 分钟` : '全量下载完成后校验'}</dd></div></dl>
      <p>来源：币安正式市场 {market === 'spot' ? '现货' : 'USD-M 永续合约'}公开数据（data.binance.vision + 官方行情接口），始于该交易对上线。聚合按 UTC，周线从周一开始。</p>
      {d.status !== 'ready' && <p>下载与补齐完成前，图表只显示当前已入库部分。</p>}
      {d.error && <p className="down" role="alert">{d.error}</p>}{message && <p role="status">{message}</p>}
    </section>}
    <section className="history-card">
      <div className="history-controls"><label>周期 <select aria-label="历史K线周期" value={interval} onChange={e => { setIntervalValue(e.target.value); setOverview(false) }}>{intervals.map(i => <option key={i}>{i}</option>)}</select></label>
        <button onClick={() => { setIntervalValue('1w'); setEnd(0); setOverview(true); setRefresh(v => v + 1) }}>全部历史总览</button>
        <button onClick={first} disabled={!d?.first}>最早行情</button><button onClick={latest}>最新入库</button>
        <form onSubmit={e => { e.preventDefault(); if (date) { setEnd(Date.parse(`${date}T00:00:00Z`) / 1000 + 86400); setOverview(false); setRefresh(v => v + 1) } }}><label>跳到日期（UTC） <input type="date" required value={date} onChange={e => setDate(e.target.value)} aria-label="历史日期" /></label><button type="submit">查看</button></form>
      </div>
      <HistoryChart market={market} interval={interval} end={end} overview={overview} revision={`${refresh}:${d?.rows ? 'has-data' : 'empty'}:${d?.status === 'ready' ? d.syncedThrough : 0}`} />
    </section>
    {!!d?.gapCount && <details className="history-card"><summary>查看数据缺口（{d.gapCount} 处）</summary><p>已向官方接口重试补齐；仍无数据的分钟保留为空，不生成虚构 K 线。缺少分钟的聚合 K 线会标记为不完整。最多展示前 500 处。</p><table><thead><tr><th>开始 UTC</th><th>结束 UTC</th><th>分钟</th></tr></thead><tbody>{gaps[market]?.map(g => <tr key={g.from}><td>{utc(g.from)}</td><td>{utc(g.to)}</td><td>{g.minutes}</td></tr>)}</tbody></table></details>}
  </div>
}
