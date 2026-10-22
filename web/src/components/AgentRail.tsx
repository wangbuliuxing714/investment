import { useEffect, useMemo, useRef, useState } from 'react'
import type { ChatLine } from '../api'
import { groupLines } from '../agentLog'
import { parseMarket } from '../marketResult'
import { AnalyticsResult, analyticsHint } from './AnalyticsResult'
import { MarkdownDocument } from './MarkdownDocument'
import { RawToolData, ResearchToolResult, ToolParameters, parseToolValue, researchError, researchHint, researchTool, toolArgs } from './ResearchToolResult'

const ANALYTICS_TOOLS = ['get_indicators','get_trading_history','get_funding','get_open_interest','get_order_flow','get_order_book']
import { actionLabel, sideLabel, when } from '../format'

const TOOL_NAMES: Record<string, string> = {
	get_history: '本地历史行情',
  list_files: '列出文件',
  read_file: '读取 Markdown',
  write_file: '保存 Markdown',
  create_directory: '创建文件夹',
  get_indicators: '连续技术指标',
  get_trading_history: '成交与收益',
  get_funding: '资金费率',
  get_open_interest: '合约持仓量',
  get_order_flow: '主动买卖量',
  get_order_book: '盘口与滑点',
  get_account: '查看账户',
  get_positions: '查看持仓',
  get_market: '查看行情',
  submit_decisions: '提交下单',
  set_protection: '补挂止盈止损',
  wait: '等待',
}

const FIELD_NAMES: Record<string, string> = {
  symbol: '币种',
  action: '动作',
  leverage: '杠杆',
  position_size_usd: '名义价值',
  stop_loss: '止损',
  take_profit: '止盈',
  confidence: '把握',
  reasoning: '原因',
  decisions: '决定',
  seconds: '秒数',
  reason: '原因',
  name: '名称',
  arguments: '参数',
}

export function AgentRail({
	optimizing = false,
	modeReady = false,
	switching = false,
	onModeChange,
  open,
  running,
  stopKind,
  stopReason,
  stoppedAt,
  speaking,
  pending,
  lines,
  width,
  onWidth,
  onToggle,
  onStart,
  onStop,
  onSend,
}: {
	optimizing?: boolean
	modeReady?: boolean
	switching?: boolean
	onModeChange?: (enabled: boolean) => void
  open: boolean
  running: boolean
  stopKind?: string
  stopReason?: string
  stoppedAt?: string
  speaking: boolean
  pending: string
  lines: ChatLine[]
  width: number
  onWidth?: (width: number) => void
  onToggle: () => void
  onStart: () => void
  onStop: () => void
  onSend: (text: string) => Promise<unknown>
}) {
  const bodyRef = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const [sendError, setSendError] = useState('')

  useEffect(() => {
    const el = bodyRef.current
    if (!el || !stick.current) return
    el.scrollTop = el.scrollHeight
  }, [lines])

  if (!open) {
    return (
      <aside className="rail rail-closed">
        <button className="rail-tab" onClick={onToggle}>{running ? '代理 · 运行中' : '代理'}</button>
      </aside>
    )
  }

  return (
    <aside className="rail">
      {onWidth && <RailResize width={width} onWidth={onWidth} />}
      <div className="rail-head">
        <div className="rail-title">
          <strong>{optimizing ? '策略优化' : '代理'}</strong>
          <span className={running ? 'dot on' : 'dot'} />
          <span className="sub">{running ? (speaking ? '正在说' : '运行中') : '已停止'}</span>
        </div>
        <div className="rail-actions">
          <button className="btn primary" disabled={switching || pending !== '' || running} onClick={onStart}>启动</button>
          <button className="btn" disabled={switching || pending !== '' || !running} onClick={onStop}>停止</button>
          <button className="tiny" onClick={onToggle}>收起</button>
        </div>
      </div>
      {onModeChange && <div className="rail-mode">
        <label className="optimization-toggle"><span>策略优化模式</span><input type="checkbox" role="switch" aria-label="策略优化模式" checked={optimizing} disabled={!modeReady || pending !== '' || switching || sending} onChange={e => onModeChange(e.target.checked)} /><i aria-hidden="true" /></label>
        <p>{switching ? '正在停止当前执行并切换对话…' : optimizing ? '独立优化对话 · 历史研究与策略复盘，不下单' : '交易对话 · 开启优化模式将停止当前运行并切换对话'}</p>
      </div>}
      {!running && stopReason && <div className="rail-stop-reason" role="status"><strong>{stopKind === 'completed' ? '本轮完成' : stopKind === 'maintenance' ? '维护停止' : stopKind === 'error' ? '异常停止' : '手动停止'}</strong>{stoppedAt && <span> · {new Date(stoppedAt).toLocaleString('zh-CN', { hour12: false })}</span>}<p>{stopReason}</p></div>}
      <div
        className="rail-body"
        ref={bodyRef}
        onScroll={(event) => {
          const el = event.currentTarget
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
        }}
      >
        {lines.length === 0 && <div className="empty">{optimizing ? '这是独立的策略优化对话。可以先让代理分析 BTC 历史、检查当前策略并建立优化记录。' : '在下面跟代理说话。点开工具，可以看它这次拿到的结果。'}</div>}
        {groupLines(lines).map((block) => (
          block.kind === 'call'
            ? <ToolCall key={block.call.id} call={block.call} result={block.result} onInspect={() => { stick.current = false }} />
            : <ChatItem key={block.line.id} line={block.line} />
        ))}
      </div>
      <form
        className="composer"
        onSubmit={(event) => {
          event.preventDefault()
          const text = draft.trim()
          if (!text || sending || switching) return
          setSending(true)
          setSendError('')
          onSend(text).then(() => setDraft('')).catch((err: Error) => setSendError(err.message || '没送出去')).finally(() => setSending(false))
        }}
      >
        <textarea
          value={draft}
          rows={2}
          disabled={switching}
          placeholder={optimizing ? '讨论策略、历史检验与修正原因' : running ? '跟正在运行的代理说一句' : '跟代理说一句，它会回答'}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault()
              event.currentTarget.form?.requestSubmit()
            }
          }}
        />
        <button className="btn primary" type="submit" disabled={switching || sending || draft.trim() === ''}>{sending ? '发送中' : '发送'}</button>
        {sendError && <span className="err">{sendError}</span>}
      </form>
    </aside>
  )
}

export function RailResize({
  width,
  onWidth,
}: {
  width: number
  onWidth: (width: number) => void
}) {
  const drag = useRef<{ x: number; width: number } | null>(null)
  const [dragging, setDragging] = useState(false)

  useEffect(() => {
    if (!dragging) return
    function move(event: PointerEvent) {
      const start = drag.current
      if (!start) return
      onWidth(clampRail(start.width - (event.clientX - start.x)))
    }
    function up() {
      drag.current = null
      setDragging(false)
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    return () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
    }
  }, [dragging, onWidth])

  return (
    <div
      className={dragging ? 'rail-resize dragging' : 'rail-resize'}
      role="separator"
      aria-orientation="vertical"
      aria-label="拖动调整代理栏宽度"
      aria-valuenow={width}
      onPointerDown={(event) => {
        drag.current = { x: event.clientX, width }
        setDragging(true)
        event.currentTarget.setPointerCapture(event.pointerId)
      }}
      onDoubleClick={() => onWidth(380)}
    >
      <span />
    </div>
  )
}

export function readRailWidth() {
  const saved = Number(localStorage.getItem('desk-rail-width'))
  return clampRail(Number.isFinite(saved) ? saved : 380)
}

export function clampRail(width: number) {
  const max = Math.min(720, Math.max(420, window.innerWidth - 280))
  return Math.min(max, Math.max(300, Math.round(width)))
}

function ChatItem({ line }: { line: ChatLine }) {
  if (line.role === 'status') return <div className="chat-status">{line.text}</div>
  if (line.role === 'user') {
    return (
      <div className="bubble mine">
        <div className="bubble-meta">我 · {when(line.at)}</div>
        <div className="bubble-text">{line.text}</div>
      </div>
    )
  }
  const prose = line.text.trim()
  if (!prose) return null
  return (
    <div className="bubble">
      <div className="bubble-meta">{when(line.at)}</div>
      <div className="bubble-text bubble-markdown"><MarkdownDocument content={prose} /></div>
    </div>
  )
}

function ToolCall({ call, result, onInspect }: { call: ChatLine; result?: ChatLine; onInspect: () => void }) {
  const [open, setOpen] = useState(false)
  const name = call.name || ''
  const title = TOOL_NAMES[name] || call.text.split('{')[0].trim() || '工具'
  const resultText = result?.text || (call.role === 'result' || name === 'wait' ? call.text : '')
  const args = useMemo(() => call.role === 'result' ? {} : toolArgs(call.text), [call.text, call.role])
  const data = useMemo(() => parseToolValue(resultText), [resultText])
  const isResearch = researchTool(name)
  const error = researchError(name, resultText, data)
  const hint = isResearch ? researchHint(name, args, data) : toolHint(call, result)
  const state = error ? '未成功' : name === 'wait' ? '已安排' : resultText ? '已返回' : '等待结果'
  const decisions = name === 'submit_decisions' ? extractArgs(call.text) : ''
  const submitted = decisions ? tryParse(decisions) : null
  const decisionList = isRecord(submitted) && Array.isArray(submitted.decisions) ? submitted.decisions : null
  return (
    <div className={(open ? 'call tool open' : 'call tool') + (error ? ' tool-failed' : '')} data-tool={name}>
      <button type="button" className="call-head" aria-expanded={open} onClick={() => { if (!open) onInspect(); setOpen((value) => !value) }}>
        <i />
        <b>{title}</b>
        {!isResearch && hint && <span>{hint}</span>}
        <em className={error ? 'tool-state error' : 'tool-state'}>{state}</em>
      </button>
      {isResearch && <div className="call-preview">{hint}</div>}
      {open && (
        <div className="call-body">
          {isResearch ? resultText ? <ResearchToolResult name={name} text={resultText} data={data} args={args} /> : <p className="detail-text">等待工具返回结果…</p> : <ResultView name={name} text={resultText} />}
          <ToolParameters args={args} />
          {decisionList ? (
            <section className="detail">
              <h3>提交的决定</h3>
              <JsonView value={decisionList} />
            </section>
          ) : decisions ? <Detail label="提交的决定" text={decisions} /> : null}
          {isResearch && resultText && <RawToolData text={resultText} />}
        </div>
      )}
    </div>
  )
}

function ResultView({ name, text }: { name: string; text: string }) {
  if (!text) return <p className="detail-text">还没有结果</p>
  if (ANALYTICS_TOOLS.includes(name)) return <AnalyticsResult text={text}/>
  if (name === 'get_account') {
    const stats = statPairs(text)
    return stats ? <StatGrid pairs={stats} /> : <pre className="detail-text">{text}</pre>
  }
  if (name === 'get_positions') {
    if (text.includes('无持仓')) return <p className="detail-text">没有持仓</p>
    const rows = parsePositions(text)
    if (rows.length === 0) return <pre className="detail-text">{text}</pre>
    return (
      <div className="detail-stack">
        {text.split('\n').filter(line => line.includes('保护单')).map((line, i) => <p className="detail-text" key={i}>{line}</p>)}
        {rows.map((row) => (
          <div className="record" key={`${row.symbol}-${row.side}`}>
            <strong>{row.symbol.replace(/USDT$/i, '')} · {sideLabel(row.side)} {row.leverage}x</strong>
            <dl>
              <div><dt>数量</dt><dd>{row.amount}</dd></div>
              <div><dt>开仓价</dt><dd>{row.entry}</dd></div>
              <div><dt>标记价</dt><dd>{row.mark}</dd></div>
              <div><dt>盈亏</dt><dd className={row.pnl.startsWith('-') ? 'down' : 'up'}>{row.pnl}</dd></div>
            </dl>
          </div>
        ))}
      </div>
    )
  }
  if (name === 'get_market') {
    const blocks = parseMarket(text)
    if (blocks.length === 0) return <pre className="detail-text">{text}</pre>
    return (
      <div className="detail-stack">
        {blocks.map((block, blockIndex) => (
          <section className="market-block" key={block.symbol + ':' + blockIndex}>
            <h4>{block.symbol.replace(/USDT$/i, '')} · {timeframeLabel(block.timeframe)} · {block.rows.length} 根</h4>
            {block.notes.length > 0 && <p className="detail-text">{block.notes.join('\n')}</p>}
            <StatGrid pairs={block.stats} />
            <p className="sub">K线按由新到旧展示；时间为每根K线的开盘时间（UTC）。</p>
            {block.rows.length > 0 && (
              <div className="candle-wrap">
                <table className="candle-table">
                  <thead>
                    <tr><th>开盘时间（UTC）</th><th>开</th><th>高</th><th>低</th><th>收</th><th>量</th></tr>
                  </thead>
                  <tbody>
                    {[...block.rows].reverse().map((row) => (
                      <tr key={row[0]} className={row[6] ? 'current' : undefined}>
                        {row.slice(0, 6).map((cell, index) => <td key={index}>{cell}</td>)}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        ))}
        <details><summary>查看原始行情结果</summary><pre className="detail-text">{text}</pre></details>
      </div>
    )
  }
  return <pre className="detail-text">{text}</pre>
}

function Detail({ label, text }: { label: string; text: string }) {
  const parsed = tryParse(text)
  const stats = parsed ? null : statPairs(text)
  return (
    <section className="detail">
      <h3>{label}</h3>
      {parsed ? <JsonView value={parsed} /> : stats ? <StatGrid pairs={stats} /> : <pre className="detail-text">{text}</pre>}
    </section>
  )
}

function JsonView({ value }: { value: unknown }) {
  if (Array.isArray(value)) {
    if (value.length === 0) return <p className="detail-text">空</p>
    if (value.every(isRecord)) {
      return <div className="detail-stack">{value.map((item, index) => <RecordCard key={index} value={item as Record<string, unknown>} />)}</div>
    }
    return <ul className="detail-list">{value.map((item, index) => <li key={index}>{formatScalar(item)}</li>)}</ul>
  }
  if (isRecord(value)) return <RecordCard value={value} />
  return <p className="detail-text">{formatScalar(value)}</p>
}

function RecordCard({ value }: { value: Record<string, unknown> }) {
  const symbol = typeof value.symbol === 'string' ? value.symbol.replace(/USDT$/i, '') : ''
  const action = typeof value.action === 'string' ? actionLabel(value.action) : ''
  const entries = Object.entries(value).filter(([key]) => key !== 'symbol' && key !== 'action')
  return (
    <div className="record">
      {(symbol || action) && <strong>{[symbol, action].filter(Boolean).join(' · ')}</strong>}
      {entries.length > 0 && (
        <dl>
          {entries.map(([key, item]) => (
            <div key={key}>
              <dt>{FIELD_NAMES[key] || key}</dt>
              <dd>{isRecord(item) || Array.isArray(item) ? <JsonView value={item} /> : formatField(key, item)}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  )
}

function StatGrid({ pairs }: { pairs: { label: string; value: string }[] }) {
  return (
    <div className="stat-grid">
      {pairs.map((pair) => (
        <div key={pair.label}>
          <span>{pair.label}</span>
          <b>{pair.value}</b>
        </div>
      ))}
    </div>
  )
}

function toolHint(line: ChatLine, result?: ChatLine) {
  if (ANALYTICS_TOOLS.includes(line.name || '')) return analyticsHint(result?.text || line.text) || readField(line.text, 'symbol')
  if (line.name === 'wait') return line.text.replace(/^等待\s*/, '').trim()
  if (line.name === 'get_positions') {
    const text = result?.text || ''
    if (text.includes('无持仓')) return '没有持仓'
    const count = text.split('\n').filter((item) => /\s(long|short)\s/.test(item)).length
    return count ? `${count} 个持仓` : ''
  }
  if (line.name === 'get_market') {
    const symbols = [...(result?.text || '').matchAll(/^([A-Z0-9]+)\s/gm)].map((item) => item[1].replace(/USDT$/i, ''))
    if (symbols.length) return symbols.join(' · ')
    const symbol = readField(line.text, 'symbol')
    return symbol ? symbol.replace(/USDT$/i, '') : '策略币种'
  }
  if (line.name === 'submit_decisions') {
    const actions = [...line.text.matchAll(/"action"\s*:\s*"([^"]+)"/g)].map((item) => actionLabel(item[1]))
    const symbols = [...line.text.matchAll(/"symbol"\s*:\s*"([^"]+)"/g)].map((item) => item[1].replace(/USDT$/i, ''))
    if (actions.length === 0) return '查看决定'
    return actions.map((action, index) => (symbols[index] ? `${symbols[index]} ${action}` : action)).join(' · ')
  }
  return ''
}

function parsePositions(text: string) {
  const rows: { symbol: string; side: string; amount: string; entry: string; mark: string; pnl: string; leverage: string }[] = []
  const pattern = /^(\S+)\s+(long|short)\s+数量\s+(\S+)\s+开仓价\s+(\S+)\s+标记价\s+(\S+)\s+盈亏\s+(\S+)\s+杠杆\s+(\d+)\s*$/
  for (const line of text.split('\n')) {
    const match = line.trim().match(pattern)
    if (!match) continue
    rows.push({
      symbol: match[1],
      side: match[2],
      amount: match[3],
      entry: match[4],
      mark: match[5],
      pnl: match[6],
      leverage: match[7],
    })
  }
  return rows
}


function timeframeLabel(value: string) {
  switch (value) {
    case '1m':
      return '1 分钟'
    case '5m':
      return '5 分钟'
    case '15m':
      return '15 分钟'
    case '1h':
      return '1 小时'
    case '4h':
      return '4 小时'
    default:
      return value
  }
}

function extractArgs(text: string) {
  const start = text.indexOf('{')
  if (start < 0) return ''
  return text.slice(start).trim()
}

function readField(text: string, key: string) {
  const match = text.match(new RegExp(`"${key}"\\s*:\\s*"([^"]*)"`))
  return match?.[1]?.trim() || ''
}

function tryParse(text: string): unknown | null {
  const start = text.indexOf('{')
  const list = text.indexOf('[')
  const at = start < 0 ? list : list < 0 ? start : Math.min(start, list)
  if (at < 0) return null
  try {
    const value = JSON.parse(text.slice(at).replace(/…$/, ''))
    return value && typeof value === 'object' ? value : null
  } catch {
    return null
  }
}

function statPairs(text: string) {
  if (text.includes('\n') && text.split('\n').length > 3) return null
  const pairs = [...text.matchAll(/([\u4e00-\u9fa5]{2,8})\s+(-?\d+(?:\.\d+)?)/g)].map((item) => ({
    label: item[1],
    value: item[2],
  }))
  return pairs.length >= 2 ? pairs : null
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function formatField(key: string, value: unknown) {
  if (key === 'action' && typeof value === 'string') return actionLabel(value)
  if (typeof value === 'number') return value.toLocaleString('zh-CN')
  if (value == null || value === '') return '—'
  return String(value)
}

function formatScalar(value: unknown) {
  if (typeof value === 'number') return value.toLocaleString('zh-CN')
  if (value == null) return '—'
  return String(value)
}
