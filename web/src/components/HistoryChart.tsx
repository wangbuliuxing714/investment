import { useEffect, useRef, useState } from 'react'
import { ColorType, createChart, type UTCTimestamp } from 'lightweight-charts'
import { api, type Candle } from '../api'

export function HistoryChart({ market, interval, end, overview, revision }: { market: string; interval: string; end: number; overview: boolean; revision: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [note, setNote] = useState('正在读取本地历史库…')
  const [hover, setHover] = useState<Candle | null>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    let dead = false, loading = false, done = false, candles: Candle[] = []
    const chart = createChart(el, {
      width: el.clientWidth, height: 480,
      layout: { background: { type: ColorType.Solid, color: '#fff' }, textColor: '#6e6e73' },
      grid: { vertLines: { color: '#f2f2f4' }, horzLines: { color: '#f2f2f4' } },
      localization: { locale: 'zh-CN' }, timeScale: { timeVisible: true, secondsVisible: false, minBarSpacing: 0.1 },
    })
    const series = chart.addCandlestickSeries({ upColor: '#0ecb81', downColor: '#f6465d', borderVisible: false, wickUpColor: '#0ecb81', wickDownColor: '#f6465d', priceFormat: { type: 'price', precision: 2, minMove: 0.01 } })
    const volume = chart.addHistogramSeries({ priceFormat: { type: 'volume' }, priceScaleId: 'volume' })
    chart.priceScale('right').applyOptions({ scaleMargins: { top: 0.08, bottom: 0.23 } })
    chart.priceScale('volume').applyOptions({ scaleMargins: { top: 0.83, bottom: 0 }, visible: false })
    const resize = new ResizeObserver(() => chart.applyOptions({ width: el.clientWidth }))
    resize.observe(el)
    const load = async (cursor: number, initial = false) => {
      if (dead || loading || done) return
      loading = true
      setNote('正在读取本地历史库…')
      try {
        const page = await api.historyKlines(market, interval, cursor, 1500)
        if (dead) return
        const range = chart.timeScale().getVisibleLogicalRange()
        const oldCount = candles.length
        const merged = new Map(candles.map(c => [c.time, c]))
        for (const c of page.candles) merged.set(c.time, c)
        candles = [...merged.values()].sort((a, b) => a.time - b.time)
        series.setData(candles.map(c => ({ ...c, time: c.time as UTCTimestamp })))
        volume.setData(candles.map(c => ({ time: c.time as UTCTimestamp, value: c.volume, color: c.close >= c.open ? '#0ecb8155' : '#f6465d55' })))
        done = !page.hasMore || !page.candles.length
        if (initial) {
          setHover(candles.at(-1) || null)
          if (overview) chart.timeScale().fitContent()
          else chart.timeScale().setVisibleLogicalRange({ from: Math.max(0, candles.length - 160), to: candles.length + 5 })
        } else if (range) {
          const added = candles.length - oldCount
          chart.timeScale().setVisibleLogicalRange({ from: range.from + added, to: range.to + added })
        }
        setNote(!candles.length ? '这个日期之前尚无本地数据；请检查下载状态与行情起点。' : done ? '已到本地历史起点。' : '向左拖动加载更早历史；滚轮缩放，十字光标查看价格与成交量。')
      } catch (e) { if (!dead) setNote(e instanceof Error ? e.message : '历史读取失败') }
      finally { loading = false }
    }
    chart.timeScale().subscribeVisibleLogicalRangeChange(range => {
      if (range && range.from < 40 && candles.length && !loading && !done) void load(candles[0].time)
    })
    chart.subscribeCrosshairMove(p => {
      if (typeof p.time === 'number') setHover(candles.find(c => c.time === p.time) || null)
    })
    setHover(null)
    void load(end, true)
    return () => { dead = true; resize.disconnect(); chart.remove() }
  }, [market, interval, end, overview, revision])
  return <div data-testid="history-chart">
    <p className="chart-note">时间：UTC{hover && <> · {new Date(hover.time * 1000).toISOString().replace('T', ' ').slice(0, 16)} · 开 {hover.open.toFixed(2)} · 高 {hover.high.toFixed(2)} · 低 {hover.low.toFixed(2)} · 收 {hover.close.toFixed(2)} · 量 {hover.volume.toFixed(4)} BTC{hover.closed === false && <strong> · 周期未结束或分钟样本不全</strong>}</>}</p>
    <div ref={ref} style={{ width: '100%', height: 480 }} />
    <p className="chart-note" role="status">{note}</p>
  </div>
}
