import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, type Account, type ChatLine, type Position, type ScanEvent, type Settings, type Strategy, type TraderState } from '../api'
import { AgentRail, readRailWidth } from '../components/AgentRail'
import { Chart } from '../components/Chart'
import { RealizedPnl } from '../components/RealizedPnl'
import { PositionProtection, ProtectionValue } from '../components/PositionProtection'
import { mergeChatLines } from '../agentLog'
import { tradingMode } from '../tradingMode'
import { px, sideLabel, usd } from '../format'

const FRAMES = [
  ['1s', '1秒'],
  ['1m', '1分'], ['3m', '3分'], ['5m', '5分'], ['15m', '15分'], ['30m', '30分'],
  ['1h', '1时'], ['2h', '2时'], ['4h', '4时'], ['6h', '6时'], ['8h', '8时'], ['12h', '12时'],
  ['1d', '1日'], ['3d', '3日'], ['1w', '1周'], ['1M', '1月'],
] as const

export function Dashboard({ onOpenExchange }: { onOpenExchange: () => void }) {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [strategy, setStrategy] = useState<Strategy | null>(null)
  const [trader, setTrader] = useState<TraderState | null>(null)
	const conversation = trader?.conversationId || 'trading'
	const optimizing = trader?.mode === 'optimization'
	const conversationRef = useRef('trading')
	const selectionVersion = useRef(0)
	const applyTrader = useCallback((next: TraderState) => {
		const id = next.conversationId || 'trading'
		if (conversationRef.current !== id) { conversationRef.current = id; setLines([]) }
		setTrader(next)
	}, [])
  const [account, setAccount] = useState<Account | null>(null)
  const [positions, setPositions] = useState<Position[]>([])
  const [positionsKnown, setPositionsKnown] = useState(false)
  const refreshing = useRef(false)
  const [lines, setLines] = useState<ChatLine[]>([])
  const [symbol, setSymbol] = useState('BTCUSDT')
  const [frame, setFrame] = useState('')
  const [coinQuery, setCoinQuery] = useState('')
  const [chartScope, setChartScope] = useState<'strategy' | 'all'>('strategy')
  const [catalog, setCatalog] = useState<string[]>([])
  const [catalogError, setCatalogError] = useState('')
  const [error, setError] = useState('')
  const [railOpen, setRailOpen] = useState(true)
  const [railWidth, setRailWidth] = useState(readRailWidth)
  const [narrow, setNarrow] = useState(() => window.matchMedia('(max-width: 960px)').matches)
  const [busy, setBusy] = useState('')
  const [protecting, setProtecting] = useState('')
  const [notice, setNotice] = useState('')

  const reload = useCallback(async () => {
    if (refreshing.current) return
    refreshing.current = true
	const version = selectionVersion.current
    try {
    const [nextSettings, nextStrategy, nextTrader] = await Promise.all([api.settings(), api.strategy(), api.trader()])
	if (version !== selectionVersion.current) return
    setSettings(nextSettings)
    setStrategy(nextStrategy)
    applyTrader(nextTrader)
    if (nextSettings.hasBinanceKey) {
      try {
        const [nextAccount, nextPositions] = await Promise.all([api.account(), api.positions()])
        setAccount(nextAccount)
        setPositions(nextPositions)
        setPositionsKnown(true)
        setError('')
      } catch (err) {
        setAccount(null)
        setPositions([])
        setPositionsKnown(false)
        setError(err instanceof Error ? err.message : '账户读取失败')
      }
    } else {
      setAccount(null); setPositions([]); setPositionsKnown(false)
    }
    } finally { refreshing.current = false }
  }, [applyTrader])

  useEffect(() => {
    reload().catch((err: Error) => setError(err.message))
    const timer = window.setInterval(() => {
      reload().catch((err: Error) => setError(err.message))
    }, 4000)
    return () => window.clearInterval(timer)
  }, [reload])

  useEffect(() => {
    let disposed = false
    const refreshLog = async () => {
      try {
        const snapshot = await api.agentLog(conversation)
        if (!disposed && conversationRef.current === conversation) setLines((prev) => mergeChatLines(prev, snapshot.filter(line => (line.conversationId || 'trading') === conversation)))
      } catch {
        // Keep the last good transcript during temporary network failures.
      }
    }
    void refreshLog()
    const timer = window.setInterval(refreshLog, 4000)
    return () => {
      disposed = true
      window.clearInterval(timer)
    }
  }, [conversation])

  useEffect(() => {
    const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/ws`)
    socket.onmessage = (event) => {
      const msg = JSON.parse(String(event.data)) as ScanEvent
      if (msg.type === 'chat' && msg.id && msg.text) {
        if ((msg.conversationId || 'trading') !== conversationRef.current) return
        const line: ChatLine = { id: msg.id, role: msg.phase || 'status', name: msg.name, text: msg.text, at: msg.at || new Date().toISOString(), conversationId: msg.conversationId }
        setLines((prev) => mergeChatLines(prev, [line]))
      }
      if (msg.type === 'status') reload().catch(() => undefined)
    }
    return () => socket.close()
  }, [reload])

  useEffect(() => {
    api.symbols().then((next) => { setCatalog(next); setCatalogError('') })
      .catch(() => setCatalogError('完整合约列表暂不可用，当前显示策略合约和持仓。'))
  }, [settings?.binanceConnection, settings?.testnet])

  const coins = useMemo(() => chartScope === 'all' ? chartCoins(strategy, positions, catalog) : strategy?.coins ?? [], [chartScope, strategy, positions, catalog])
  const interval = frame || '15m'
  const needle = coinQuery.trim().toUpperCase()
  const shown = coins.filter((coin) => !needle || coin.includes(needle) || coin.replace(/USDT$/i, '').includes(needle))

  useEffect(() => {
    if (coins.length && !coins.includes(symbol)) setSymbol(coins[0])
  }, [coins, symbol])

  useEffect(() => {
    const media = window.matchMedia('(max-width: 960px)')
    const apply = () => setNarrow(media.matches)
    apply()
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [])

  const resizeRail = useCallback((width: number) => {
    setRailWidth(width)
    localStorage.setItem('desk-rail-width', String(width))
  }, [])

  async function run(label: string, action: () => Promise<unknown>) {
    setBusy(label)
    setError('')
    try {
      await action()
      await reload()
    } catch (err) {
      setError(err instanceof Error ? err.message : '操作失败')
    } finally {
      setBusy('')
    }
  }

  const mode = tradingMode(settings)
  const protectionPosition = positions.find(p => `${p.symbol}-${p.side}` === protecting)

  return (
    <div
      className={railOpen ? 'board' : 'board rail-off'}
      style={railOpen && !narrow ? { gridTemplateColumns: `minmax(0, 1fr) ${railWidth}px` } : undefined}
    >
      <div className="page">
        <div className="page-head">
          <h1>看板</h1>
          <div className="row gap wrap">
            <span className={optimizing ? 'pill optimization' : mode.className}>{optimizing ? '策略优化 · 禁止下单' : mode.label}</span>
            <button className="btn" type="button" onClick={onOpenExchange}>交易模式</button>
            <span className={trader?.running ? 'pill run' : 'pill'}>{trader?.running ? '运行中' : '已停止'}</span>
            {settings?.modelName && <span className="pill">{settings.modelName}</span>}
          </div>
        </div>
        {error && <p className="err">{error}</p>}
        {notice && <p className="err" role="status">{notice}</p>}
        {trader?.lastError && <p className="err">上次代理错误{trader.stoppedAt ? `（${new Date(trader.stoppedAt).toLocaleString()}）` : ''}：{trader.lastError}</p>}
        {optimizing && <p className="sub">当前为策略优化模式，使用独立对话研究历史、复盘与记录修正原因。下方账户信息仅供查看，交易操作已禁用。</p>}
        {!optimizing && mode.id === 'paper' && <p className="sub">当前只验证代理决策，不发送订单。下方余额和持仓读取自币安正式账户，不是虚拟账户的模拟收益。</p>}
        {!optimizing && mode.id === 'demo' && <p className="sub">当前为币安模拟盘：余额、持仓和订单来自模拟账户。</p>}
        <div className="stat-row">
          <div className="stat"><span>净值</span><strong>{account ? usd(account.equity) : '—'}</strong></div>
          <div className="stat"><span>可用</span><strong>{account ? usd(account.available) : '—'}</strong></div>
          <div className="stat"><span>未实现盈亏</span><strong className={account && account.unrealized < 0 ? 'down' : 'up'}>{account ? usd(account.unrealized) : '—'}</strong></div>
          <div className="stat"><span>已用保证金</span><strong>{account ? usd(account.marginUsed) : '—'}</strong></div>
        </div>
        <RealizedPnl key={`${settings?.binanceConnection}:${mode.id}:${settings?.keyHint}`} enabled={!!settings?.hasBinanceKey} />
        <div className="card">
          <div className="row gap" style={{ marginBottom: 8 }}>
            <h2 style={{ margin: 0 }}>持仓</h2>
          </div>
          {!positionsKnown ? <div className="empty">持仓尚未确认，请等待账户数据读取成功。</div> : positions.length === 0 ? <div className="empty">没有持仓</div> : (
            <div className="positions-scroll"><table className="positions-table">
              <thead>
                <tr>
                  <th>币种</th><th>方向</th><th className="right">数量</th><th className="right">开仓价</th><th className="right">标记价</th><th className="right">止损触发价</th><th className="right">止盈触发价</th><th className="right">未实现盈亏</th><th></th>
                </tr>
              </thead>
              <tbody>
                {positions.map((item) => (
                  <tr key={`${item.symbol}-${item.side}`}>
                    <td>{item.symbol}</td>
                    <td className={item.side === 'short' ? 'down' : 'up'}>{sideLabel(item.side)} {item.leverage}x</td>
                    <td className="right">{item.amount}</td>
                    <td className="right">{px(item.entry)}</td>
                    <td className="right">{px(item.mark)}</td>
                    <td className="right"><ProtectionValue position={item} kind="stop" /></td>
                    <td className="right"><ProtectionValue position={item} kind="take" /></td>
                    <td className={item.pnl < 0 ? 'down right' : 'up right'}>{usd(item.pnl)}</td>
                    <td className="right"><div className="row gap"><button className="tiny" disabled={optimizing || busy !== '' || mode.id === 'paper'} onClick={() => setProtecting(`${item.symbol}-${item.side}`)}>补挂保护单</button><button className="tiny" disabled={optimizing || busy !== ''} onClick={() => run('平仓', async () => { const result = await api.closePosition(item.symbol, item.side); setNotice(result.warning || '') })}>平仓</button></div></td>
                  </tr>
                ))}
              </tbody>
            </table></div>
          )}
          {protectionPosition && <PositionProtection key={protecting} position={protectionPosition} disabled={optimizing || busy !== '' || mode.id === 'paper'} onRefresh={reload} onClose={() => setProtecting('')} />}
        </div>
        <div className="card">
          <div className="row gap wrap" style={{ marginBottom: 8 }}>
            <h2 style={{ margin: 0 }}>走势</h2>
            <div className="frames">
              {FRAMES.map(([id, label]) => (
                <button key={id} type="button" className={interval === id ? 'symbol on' : 'symbol'} onClick={() => setFrame(id)}>{label}</button>
              ))}
            </div>
          </div>
          <div className="row gap wrap" role="group" aria-label="行情查看范围" style={{ marginBottom: 8 }}>
            <button type="button" className={chartScope === 'strategy' ? 'symbol on' : 'symbol'} aria-pressed={chartScope === 'strategy'} onClick={() => { setChartScope('strategy'); setCoinQuery('') }}>策略币种</button>
            <button type="button" className={chartScope === 'all' ? 'symbol on' : 'symbol'} aria-pressed={chartScope === 'all'} onClick={() => { setChartScope('all'); setCoinQuery('') }}>全部行情</button>
          </div>
          <p className="sub">{chartScope === 'strategy' ? '当前仅显示策略启用的币种。切换“全部行情”可查看其他合约，不会启用交易。' : '全部合约仅供行情查看；代理仍只能交易策略中启用的币种。'}</p>
          {chartScope === 'all' && catalogError && <p className="sub">{catalogError}</p>}
          {(chartScope === 'all' || coins.length > 8) && (
            <input className="symbol-search" type="search" aria-label="搜索行情合约" value={coinQuery} placeholder={chartScope === 'all' ? '搜索全部合约' : '搜索策略币种'} onChange={(event) => setCoinQuery(event.target.value)} />
          )}
          <div className="symbol-row">
            {shown.map((coin) => (
              <button key={coin} type="button" className={coin === symbol ? 'symbol on' : 'symbol'} onClick={() => setSymbol(coin)}>{coin.replace(/USDT$/i, '')}</button>
            ))}
            {shown.length === 0 && <span className="sub">{chartScope === 'strategy' && !strategy ? '正在读取策略币种…' : chartScope === 'strategy' && coins.length === 0 ? '策略尚未启用币种，可在策略页面配置，或切换到全部行情查看。' : '没有匹配的合约'}</span>}
          </div>
          {chartScope === 'all' && strategy && coins.includes(symbol) && !strategy.coins.includes(symbol) && <p className="sub">{symbol}：仅查看行情，未在策略中启用交易。</p>}
          {coins.includes(symbol) && <Chart key={`${settings?.binanceConnection}:${mode.id}`} symbol={symbol} interval={interval} positions={positions} />}
        </div>
      </div>
      <AgentRail
		key={conversation}
		optimizing={optimizing}
		modeReady={!!trader?.conversationId}
		switching={!!trader?.switching || busy === '切换模式'}
		onModeChange={(enabled) => run('切换模式', async () => {
			selectionVersion.current++
			const next = await api.agentMode(enabled ? 'optimization' : 'trading', conversation)
			selectionVersion.current++
			applyTrader(next)
			setProtecting('')
		})}
        open={railOpen}
        running={!!trader?.running}
        stopKind={trader?.stopKind}
        stopReason={trader?.stopReason}
        stoppedAt={trader?.stoppedAt}
        speaking={!!trader?.busy}
        pending={busy}
        lines={lines}
        width={railWidth}
        onWidth={railOpen && !narrow ? resizeRail : undefined}
        onToggle={() => setRailOpen((value) => !value)}
        onStart={() => run('启动', () => api.start(conversation))}
        onStop={() => run('停止', api.stop)}
        onSend={(text) => api.ask(text, conversation)}
      />
    </div>
  )
}

function chartCoins(strategy: Strategy | null, positions: Position[], catalog: string[]) {
  const coins = [...(strategy?.coins ?? [])]
  const seen = new Set(coins)
  for (const item of positions) {
    if (item.symbol && !seen.has(item.symbol)) {
      seen.add(item.symbol)
      coins.push(item.symbol)
    }
  }
  return [...new Set([...coins, ...catalog])]
}
