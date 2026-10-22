import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ColorType,
  CrosshairMode,
  LineStyle,
  createChart,
  type AutoscaleInfo,
  type IChartApi,
  type IPriceLine,
  type ISeriesApi,
  type MouseEventParams,
  type UTCTimestamp,
} from 'lightweight-charts'
import { api, type Candle, type Level, type Position } from '../api'
import { px } from '../format'
import { mergeCandles, validCandle, type ReceivedCandle } from '../liveCandles'

const PAGE = 1500
const VIEW = 160

export function Chart({ symbol, interval, positions }: { symbol: string; interval: string; positions: Position[] }) {
  const ref = useRef<HTMLDivElement>(null)
  const chartRef = useRef<IChartApi | null>(null)
  const seriesRef = useRef<ISeriesApi<'Candlestick'> | null>(null)
  const volumeRef = useRef<ISeriesApi<'Histogram'> | null>(null)
  const lineRefs = useRef<IPriceLine[]>([])
  const linesRef = useRef<Level[]>([])
  const hoverRef = useRef<(candle: Candle | null) => void>(() => undefined)
  const [orders, setOrders] = useState<Level[]>([])
  const [orderNote, setOrderNote] = useState('')
  const [note, setNote] = useState('正在向币安读取K线')
  const [last, setLast] = useState<Candle | null>(null)
  const [hover, setHover] = useState<Candle | null>(null)
  const [streamStatus, setStreamStatus] = useState('connecting')
  const [updatedAt, setUpdatedAt] = useState(0)
  const [following, setFollowing] = useState(true)
  const followRef = useRef(true)
  hoverRef.current = setHover

  const overlays = useMemo(() => buildOverlays(symbol, positions, orders), [symbol, positions, orders])
  linesRef.current = overlays
  const shown = hover || last

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const chart = createChart(el, {
      width: el.clientWidth,
      height: el.clientHeight || 420,
      layout: {
        background: { type: ColorType.Solid, color: '#ffffff' },
        textColor: '#6e6e73',
        fontFamily: '-apple-system, "PingFang SC", "Microsoft YaHei", sans-serif',
        fontSize: 11,
      },
      grid: { vertLines: { color: '#f2f2f4' }, horzLines: { color: '#f2f2f4' } },
      rightPriceScale: { borderColor: '#e5e5ea' },
      crosshair: {
        mode: CrosshairMode.Normal,
        vertLine: { color: '#c7c7cc', labelBackgroundColor: '#1d1d1f' },
        horzLine: { color: '#c7c7cc', labelBackgroundColor: '#1d1d1f' },
      },
      localization: { locale: 'zh-CN' },
      timeScale: {
        borderColor: '#e5e5ea',
        timeVisible: true,
        secondsVisible: false,
        rightOffset: 8,
        barSpacing: 7,
        minBarSpacing: 0.5,
      },
    })
    const series = chart.addCandlestickSeries({
      upColor: '#0ecb81',
      downColor: '#f6465d',
      borderVisible: false,
      wickUpColor: '#0ecb81',
      wickDownColor: '#f6465d',
      priceLineColor: '#1d1d1f',
      autoscaleInfoProvider: (original: () => AutoscaleInfo | null) => includeNearbyLines(original(), linesRef.current),
    })
    const volume = chart.addHistogramSeries({
      priceFormat: { type: 'volume' },
      priceScaleId: 'volume',
    })
    chart.priceScale('right').applyOptions({ scaleMargins: { top: 0.08, bottom: 0.22 } })
    chart.priceScale('volume').applyOptions({
      scaleMargins: { top: 0.82, bottom: 0 },
      borderVisible: false,
      visible: false,
    })
    chartRef.current = chart
    seriesRef.current = series
    volumeRef.current = volume
    const fit = () => chart.applyOptions({ width: el.clientWidth, height: el.clientHeight || 420 })
    fit()
    const observer = new ResizeObserver(fit)
    observer.observe(el)
    const onMove = (param: MouseEventParams) => {
      if (typeof param.time !== 'number') {
        hoverRef.current(null)
        return
      }
      const bar = param.seriesData.get(series)
      const vol = param.seriesData.get(volume)
      if (!bar || !('open' in bar) || !('high' in bar) || !('low' in bar) || !('close' in bar)) {
        hoverRef.current(null)
        return
      }
      const amount = vol && 'value' in vol ? vol.value : 0
      hoverRef.current({
        time: param.time - clockShift(),
        open: bar.open,
        high: bar.high,
        low: bar.low,
        close: bar.close,
        volume: amount,
      })
    }
    chart.subscribeCrosshairMove(onMove)
    return () => {
      observer.disconnect()
      chart.unsubscribeCrosshairMove(onMove)
      lineRefs.current = []
      chart.remove()
      chartRef.current = null
      seriesRef.current = null
      volumeRef.current = null
    }
  }, [])

  useEffect(() => { chartRef.current?.applyOptions({ timeScale: { secondsVisible: interval.endsWith('s') } }) }, [interval])

  useEffect(() => {
    const chart = chartRef.current
    const series = seriesRef.current
    const volume = volumeRef.current
    if (!chart || !series || !volume || !symbol) return
    let dead = false, loading = false, done = false, refreshing = false
    let candles: ReceivedCandle[] = [], buffered: ReceivedCandle[] = []
    let lastStreamAt = 0, lastEventAt = 0
    const follow = (value: boolean) => { followRef.current = value; setFollowing(value) }
    const paint = (next: ReceivedCandle[], mode: 'replace' | 'prepend' | 'append') => {
      const range = mode === 'replace' ? null : chart.timeScale().getVisibleLogicalRange()
      const wasFollowing = followRef.current
      const added = mode === 'prepend' ? Math.max(0, next.length - candles.length) : 0
      candles = next
      const precision = pricePrecision(next[next.length - 1]?.close || 0)
      series.applyOptions({ priceFormat: { type: 'price', precision, minMove: 10 ** -precision } })
      series.setData(next.map(candlePoint))
      volume.setData(next.map(volumePoint))
      setLast(next[next.length - 1] || null)
      if (mode === 'replace') {
        chart.timeScale().setVisibleLogicalRange({ from: Math.max(0, next.length - VIEW), to: next.length + 8 })
        follow(true)
      } else if (mode === 'prepend' && range) {
        chart.timeScale().setVisibleLogicalRange({ from: range.from + added, to: range.to + added })
        follow(wasFollowing)
      } else if (wasFollowing) {
        chart.timeScale().scrollToPosition(8, false)
        follow(true)
      } else if (range) {
        chart.timeScale().setVisibleLogicalRange(range)
        follow(false)
      }
    }
    const loadOlder = async (end?: number) => {
      if (dead || loading || done) return
      loading = true
      const started = Date.now()
      try {
        const page = await api.klines(symbol, interval, end, PAGE)
        if (dead) return
        if (!end) { paint(mergeCandles(page, buffered), 'replace'); buffered = [] }
        else if (page.length) {
          const merged = mergeCandles(candles, page, started)
          if (merged.length > candles.length) paint(merged, 'prepend')
        }
        if (page.length < PAGE) {
          done = true
          setNote(page.length || candles.length ? '已经到这个合约在币安上的第一根K线' : '币安没有这个合约的K线')
        } else setNote('向左拖，可以一直查看更早的历史K线')
      } catch (err) { if (!dead) setNote(err instanceof Error ? err.message : 'K线没有读到') }
      finally { loading = false }
    }
    const onRange = () => {
      const range = chart.timeScale().getVisibleLogicalRange()
      if (!range || !candles.length) return
      follow(range.to >= candles.length - 1)
      if (!done && !loading && range.from < 50) void loadOlder(candles[0].time)
    }
    const refreshTail = async () => {
      if (dead || refreshing || Date.now() - lastStreamAt < 5000) return
      if (!candles.length) { void loadOlder(); return }
      refreshing = true
      const started = Date.now()
      try {
        const page = await api.klines(symbol, interval, undefined, 5)
        if (dead || !page.length) return
        const merged = mergeCandles(candles, page.map(c => ({ ...c, source: 'rest' })), started)
        paint(merged, 'append')
        if (Date.now() - lastStreamAt >= 5000) { setStreamStatus('fallback'); setUpdatedAt(Date.now()) }
      } catch { if (!dead) setStreamStatus('stale') }
      finally { refreshing = false }
    }
    const applyLive = (candle: ReceivedCandle) => {
      if (!candles.length) { buffered = mergeCandles(buffered, [candle]); setLast(candle); return }
      const tail = candles[candles.length - 1]
      if (candle.time < tail.time) return
      const merged = mergeCandles([tail], [candle])
      const next = merged[merged.length - 1]
      if (next === tail) return
      const wasFollowing = followRef.current
      const range = chart.timeScale().getVisibleLogicalRange()
      if (next.time === tail.time) candles[candles.length - 1] = next
      else candles.push(next)
      series.update(candlePoint(next)); volume.update(volumePoint(next)); setLast(next)
      if (wasFollowing) { chart.timeScale().scrollToPosition(8, false); follow(true) }
      else if (range) { chart.timeScale().setVisibleLogicalRange(range); follow(false) }
    }
    let socket: WebSocket | null = null
    let retry: number | undefined
    const connectLive = () => {
      if (dead) return
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      const ws = new WebSocket(`${proto}://${location.host}/api/klines/live?symbol=${encodeURIComponent(symbol)}&interval=${encodeURIComponent(interval)}`)
      socket = ws
      ws.onmessage = event => {
        if (dead) return
        try {
          const raw = JSON.parse(String(event.data)) as Candle
          if (!validCandle(raw)) return
          const now = Date.now()
          const candle = { ...raw, source: raw.source || 'websocket', receivedAt: now }
          applyLive(candle)
          if (candle.source === 'websocket') {
            lastStreamAt = now; lastEventAt = raw.updatedAt || now
            setStreamStatus(now - lastEventAt < 5000 ? 'live' : 'stale')
          } else setStreamStatus('fallback')
          setUpdatedAt(raw.updatedAt || now)
        } catch { /* Ignore a malformed frame without discarding history. */ }
      }
      ws.onerror = () => ws.close()
      ws.onclose = () => {
        if (dead) return
        setStreamStatus('connecting')
        retry = window.setTimeout(connectLive, 1000)
      }
    }
    series.setData([]); volume.setData([])
    setHover(null); setLast(null); setUpdatedAt(0); setStreamStatus('connecting'); follow(true)
    setNote('正在向币安读取K线')
    void loadOlder(); connectLive()
    chart.timeScale().subscribeVisibleLogicalRangeChange(onRange)
    const timer = window.setInterval(() => { void refreshTail() }, 5000)
    const health = window.setInterval(() => {
      if (lastStreamAt && (Date.now() - lastStreamAt > 5000 || Date.now() - lastEventAt > 5000)) {
        setStreamStatus(status => status === 'live' ? 'stale' : status)
      }
      if (lastStreamAt && Date.now() - lastStreamAt > 15000 && socket?.readyState === WebSocket.OPEN) socket.close()
    }, 1000)
    const onShow = () => { if (document.visibilityState === 'visible') void refreshTail() }
    document.addEventListener('visibilitychange', onShow)
    return () => {
      dead = true
      chart.timeScale().unsubscribeVisibleLogicalRangeChange(onRange)
      window.clearInterval(timer); window.clearInterval(health)
      if (retry) window.clearTimeout(retry)
      socket?.close()
      document.removeEventListener('visibilitychange', onShow)
    }
  }, [symbol, interval])

  useEffect(() => {
    let dead = false
    setOrders([])
    setOrderNote('')
    const load = () => {
      api.orders(symbol).then((rows) => {
        if (dead) return
        setOrders(rows)
        setOrderNote('')
      }).catch((err: Error) => {
        if (dead) return
        setOrders([])
        setOrderNote(err.message)
      })
    }
    load()
    const timer = window.setInterval(load, 4000)
    return () => {
      dead = true
      window.clearInterval(timer)
    }
  }, [symbol])

  useEffect(() => {
    const series = seriesRef.current
    if (!series) return
    for (const line of lineRefs.current) series.removePriceLine(line)
    lineRefs.current = overlays.filter((line) => line.price > 0).map((line) => series.createPriceLine({
      price: line.price,
      color: levelColor(line.kind),
      lineWidth: 1,
      lineStyle: line.kind === 'liq' || line.kind === 'trail' ? LineStyle.Dotted : LineStyle.Dashed,
      axisLabelVisible: true,
      title: levelTitle(line),
    }))
    chartRef.current?.priceScale('right').applyOptions({ autoScale: true })
  }, [overlays])

  return (
    <div>
      {overlays.length > 0 && (
        <div className="level-row">
          {overlays.map((line, index) => (
            <span key={`${line.kind}-${line.side}-${index}`}>
              <i style={{ background: levelColor(line.kind) }} />
              {levelTitle(line)} {px(line.price)}{line.workingType === 'MARK_PRICE' ? '（标记价触发）' : line.kind === 'stop' || line.kind === 'take' ? '（成交价触发）' : ''}
            </span>
          ))}
        </div>
      )}
      <div className="live-price-row">
        <span>最新成交价 <strong className={last && last.close < last.open ? 'down' : 'up'} data-testid="live-price">{last ? px(last.close) : '—'}</strong></span>
        <span className={`live-status ${streamStatus}`} data-testid="live-status">{streamStatus === 'live' ? '交易所实时推送' : streamStatus === 'fallback' ? '轮询补偿 · 推送重连中' : streamStatus === 'stale' ? '行情暂未更新 · 正在重连' : '正在连接实时行情'}</span>
        {updatedAt > 0 && <span>更新 {new Date(updatedAt).toLocaleTimeString('zh-CN', { hour12: false })}</span>}
        <button className="tiny" type="button" aria-pressed={following} onClick={() => { followRef.current = true; setFollowing(true); chartRef.current?.timeScale().scrollToPosition(8, false) }}>{following ? '跟随实时' : '返回实时'}</button>
      </div>
      <div className="chart-wrap">
        {(shown || last) && (
          <div className="ohlc">
            {last && <span>最新 <b className={last.close >= last.open ? 'up' : 'down'}>{px(last.close)}</b></span>}
            {shown && (
              <>
                <span>{formatClock(shown.time)}</span>
                <span>开 <b>{px(shown.open)}</b></span>
                <span>高 <b>{px(shown.high)}</b></span>
                <span>低 <b>{px(shown.low)}</b></span>
                <span>收 <b className={shown.close >= shown.open ? 'up' : 'down'}>{px(shown.close)}</b></span>
                <span>量 <b>{volumeText(shown.volume)}</b></span>
              </>
            )}
          </div>
        )}
        <div className="chart" ref={ref} />
      </div>
      <p className="chart-note">{note}{orderNote ? `。止盈止损没读到：${orderNote}` : ''}</p>
    </div>
  )
}

function buildOverlays(symbol: string, positions: Position[], orders: Level[]) {
  const lines: Level[] = []
  for (const position of positions || []) {
    if (!position || position.symbol !== symbol) continue
    if (position.protectionKnown) lines.push(...(position.protection || []))
    if (position.entry > 0) lines.push({ kind: 'entry', side: position.side, price: position.entry })
    if (position.liquidation > 0) lines.push({ kind: 'liq', side: position.side, price: position.liquidation })
  }
  return dedupe([...lines, ...(orders || [])])
}

function dedupe(lines: Level[]) {
  const seen = new Set<string>()
  const out: Level[] = []
  for (const line of lines) {
    const key = `${line.kind}|${line.side}|${line.price}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(line)
  }
  return out
}

function levelTitle(line: Level) {
  const side = line.side === 'short' ? '空' : line.side === 'long' ? '多' : ''
  const names: Record<string, string> = {
    entry: '开仓',
    stop: '止损',
    take: '止盈',
    liq: '强平',
    order: '挂单',
    trail: '移动止损',
  }
  const name = line.protective === false && ['stop', 'take', 'trail'].includes(line.kind) ? '开仓触发' : names[line.kind] || '价格'
  return side ? `${side} · ${name}` : name
}

function levelColor(kind: string) {
  switch (kind) {
    case 'entry':
      return '#f0b90b'
    case 'take':
      return '#0ecb81'
    case 'stop':
    case 'trail':
      return '#f6465d'
    case 'liq':
      return '#ff7a00'
    default:
      return '#0071e3'
  }
}

function includeNearbyLines(base: AutoscaleInfo | null, lines: Level[]): AutoscaleInfo | null {
  if (!base?.priceRange) return base
  let { minValue, maxValue } = base.priceRange
  const span = Math.max(maxValue - minValue, Math.abs(maxValue) * 0.002, 1e-12)
  for (const line of lines) {
    if (line.kind === 'liq' || !(line.price > 0)) continue
    if (!['stop', 'take', 'trail'].includes(line.kind) && (line.price < minValue - span * 3 || line.price > maxValue + span * 3)) continue
    minValue = Math.min(minValue, line.price)
    maxValue = Math.max(maxValue, line.price)
  }
  return { ...base, priceRange: { minValue, maxValue } }
}

// lightweight-charts prints a unix second as UTC. Shift it so the axis matches this computer's clock.
function clockShift() {
  return -new Date().getTimezoneOffset() * 60
}

function candlePoint(candle: Candle) {
  return {
    time: (candle.time + clockShift()) as UTCTimestamp,
    open: candle.open,
    high: candle.high,
    low: candle.low,
    close: candle.close,
  }
}

function volumePoint(candle: Candle) {
  return {
    time: (candle.time + clockShift()) as UTCTimestamp,
    value: candle.volume,
    color: candle.close >= candle.open ? 'rgba(14, 203, 129, 0.45)' : 'rgba(246, 70, 93, 0.45)',
  }
}

function pricePrecision(price: number) {
  const abs = Math.abs(price)
  if (abs >= 100) return 2
  if (abs >= 1) return 4
  if (abs >= 0.01) return 6
  return 8
}

function formatClock(sec: number) {
  return new Date(sec * 1000).toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function volumeText(value: number) {
  if (!Number.isFinite(value)) return '—'
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(2)}K`
  return value.toFixed(2)
}
