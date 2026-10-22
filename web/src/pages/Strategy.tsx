import { useEffect, useRef, useState } from 'react'
import { api, type Strategy as StrategyConfig } from '../api'

const empty: StrategyConfig = {
  name: '币安默认',
  coins: ['BTCUSDT', 'ETHUSDT'],
  btcEthLeverage: 5,
  altLeverage: 3,
  maxPositions: 2,
  minConfidence: 75,
  positionValueRatio: 1,
  maxMarginUsage: 0.5,
  scanIntervalSec: 900,
  prompt: '',
  timeframe: '15m',
  klineCount: 30,
}

export function Strategy() {
  const [form, setForm] = useState<StrategyConfig>(empty)
  const [coins, setCoins] = useState<string[]>(empty.coins)
  const [catalog, setCatalog] = useState<string[]>([])
  const [query, setQuery] = useState('')
  const [loadingSymbols, setLoadingSymbols] = useState(true)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const hydrated = useRef(false)
  const skipCoinSave = useRef(true)
  const formRef = useRef(form)
  formRef.current = form

  useEffect(() => {
    api.strategy().then((strategy) => {
      setForm(strategy)
      setCoins(strategy.coins)
      skipCoinSave.current = true
      hydrated.current = true
    }).catch((err: Error) => setError(err.message))
    api.symbols().then(setCatalog).catch((err: Error) => setError(err.message)).finally(() => setLoadingSymbols(false))
  }, [])

  useEffect(() => {
    if (!hydrated.current) return
    if (skipCoinSave.current) {
      skipCoinSave.current = false
      return
    }
    const timer = window.setTimeout(() => {
      const current = formRef.current
      api.saveStrategy({
        ...current,
        coins,
        scanIntervalSec: Math.round(Number(current.scanIntervalSec)),
      }).then((saved) => {
        setForm(saved)
        setMessage('合约已保存，看板上会显示这些走势。')
        setError('')
      }).catch((err: Error) => setError(err.message))
    }, 400)
    return () => window.clearTimeout(timer)
  }, [coins])

  function patch(part: Partial<StrategyConfig>) {
    setForm((prev) => ({ ...prev, ...part }))
  }

  async function save() {
    setSaving(true)
    setError('')
    setMessage('')
    try {
      const saved = await api.saveStrategy({
        ...form,
        coins,
        scanIntervalSec: Math.round(Number(form.scanIntervalSec)),
      })
      setForm(saved)
      setCoins(saved.coins)
      setMessage('已保存。正在运行的代理下一轮会用这份策略。')
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="page strategy-page">
      <div className="page-head"><div><span className="eyebrow">STRATEGY</span><h1>策略</h1><p className="sub">选择交易范围，设定仓位与风险边界。</p></div></div>
      <p className="sub">这些限制会写进发给服务器代理的内容。杠杆、持仓数、单笔金额和保证金由程序强制执行。全部提示词在模型页面管理。代理启动后自己决定何时再看，不再按固定间隔轮询。</p>
      <div className="card form strategy-form">
        <label><span>名称</span><input value={form.name} onChange={(e) => patch({ name: e.target.value })} /></label>
        <CoinPicker
          coins={coins}
          catalog={catalog}
          query={query}
          loading={loadingSymbols}
          onQuery={setQuery}
          onToggle={(symbol) => setCoins((prev) => prev.includes(symbol) ? prev.filter((item) => item !== symbol) : [...prev, symbol])}
        />
        <div className="grid-2">
          <label><span>BTC / ETH 杠杆</span><input type="number" min={1} max={125} value={form.btcEthLeverage} onChange={(e) => patch({ btcEthLeverage: Number(e.target.value) })} /></label>
          <label><span>其他币杠杆</span><input type="number" min={1} max={125} value={form.altLeverage} onChange={(e) => patch({ altLeverage: Number(e.target.value) })} /></label>
          <label><span>最多持仓</span><input type="number" min={1} max={10} value={form.maxPositions} onChange={(e) => patch({ maxPositions: Number(e.target.value) })} /></label>
          <label><span>最低把握</span><input type="number" min={0} max={100} value={form.minConfidence} onChange={(e) => patch({ minConfidence: Number(e.target.value) })} /></label>
          <label><span>单笔名义价值上限（净值倍数）</span><input type="number" min={0.05} max={20} step={0.1} value={form.positionValueRatio} onChange={(e) => patch({ positionValueRatio: Number(e.target.value) })} /></label>
          <label><span>保证金上限（%）</span><input type="number" min={5} max={100} value={Math.round(form.maxMarginUsage * 100)} onChange={(e) => patch({ maxMarginUsage: Number(e.target.value) / 100 })} /></label>

        </div>
        <p className="sub">单笔名义价值上限 = 账户净值 × {form.positionValueRatio}。这是仓位总金额，不是保证金；例如净值 1,000 USDT、上限 1 倍时，单笔最多 1,000 USDT，5 倍杠杆对应约 200 USDT 保证金，仍受保证金上限约束。</p>
        <p className="sub">K 线由代理自行选择币种、周期、根数及历史范围；这些策略参数只限制下单。全部提示词请到“模型”页面管理。</p>
        <div className="row gap">
          <button className="btn primary" disabled={saving} onClick={save}>保存</button>
          {message && <span className="ok">{message}</span>}
          {error && <span className="err">{error}</span>}
        </div>
      </div>
    </div>
  )
}

function CoinPicker({
  coins,
  catalog,
  query,
  loading,
  onQuery,
  onToggle,
}: {
  coins: string[]
  catalog: string[]
  query: string
  loading: boolean
  onQuery: (value: string) => void
  onToggle: (symbol: string) => void
}) {
  const needle = query.trim().toUpperCase()
  const known = new Set(catalog)
  const extra = loading ? [] : coins.filter((symbol) => !known.has(symbol))
  const matched = catalog.filter((symbol) => !needle || symbol.includes(needle) || symbol.replace(/USDT$/, '').includes(needle))
  const ordered = [...matched].sort((a, b) => rankCoin(a, coins) - rankCoin(b, coins) || a.localeCompare(b))
  return (
    <div className="picker">
      <span className="field-label">合约</span>
      <p className="sub">从币安正在交易的 U 本位永续合约里选。这份名单限定允许下单的合约；代理仍可查看其他合约行情。</p>
      <div className="chips">
        {coins.length === 0 && <span className="sub">还没选</span>}
        {coins.map((symbol) => (
          <button key={symbol} type="button" className="chip" onClick={() => onToggle(symbol)}>
            {symbol.replace(/USDT$/, '')} ×
          </button>
        ))}
      </div>
      <input type="text" value={query} placeholder="搜索，例如 BTC" onChange={(event) => onQuery(event.target.value)} />
      <div className="picker-list">
        {loading && <div className="empty">正在读取币安合约…</div>}
        {!loading && catalog.length === 0 && <div className="empty">没有读到合约列表</div>}
        {!loading && catalog.length > 0 && ordered.length === 0 && <div className="empty">没有这个合约</div>}
        {extra.filter((symbol) => !needle || symbol.includes(needle)).map((symbol) => (
          <PickRow key={symbol} symbol={symbol} on={coins.includes(symbol)} missing onToggle={onToggle} />
        ))}
        {ordered.map((symbol) => (
          <PickRow key={symbol} symbol={symbol} on={coins.includes(symbol)} onToggle={onToggle} />
        ))}
      </div>
      {!loading && catalog.length > 0 && <p className="sub">共 {catalog.length} 个可交易合约，已选 {coins.length} 个</p>}
    </div>
  )
}

function PickRow({ symbol, on, missing, onToggle }: { symbol: string; on: boolean; missing?: boolean; onToggle: (symbol: string) => void }) {
  return (
    <button type="button" className={on ? 'pick on' : 'pick'} onClick={() => onToggle(symbol)}>
      <i />
      <b>{symbol.replace(/USDT$/, '')}</b>
      <small>{missing ? '列表里没有' : symbol}</small>
    </button>
  )
}

function rankCoin(symbol: string, selected: string[]) {
  if (selected.includes(symbol)) return 0
  if (symbol === 'BTCUSDT') return 1
  if (symbol === 'ETHUSDT') return 2
  return 3
}
