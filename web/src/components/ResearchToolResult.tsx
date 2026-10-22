import { useMemo, useState } from 'react'
import { MarkdownDocument } from './MarkdownDocument'

type Value = Record<string, unknown>
const record = (v: unknown): v is Value => !!v && typeof v === 'object' && !Array.isArray(v)
export const researchTool = (name: string) => ['get_history', 'list_files', 'read_file', 'write_file', 'create_directory'].includes(name)
export function parseToolValue(text: string): unknown { try { return JSON.parse(text) } catch { return undefined } }
export function toolArgs(text: string): Value {
  const start = text.indexOf('{')
  const value = start < 0 ? undefined : parseToolValue(text.slice(start))
  return record(value) ? value : {}
}
const market = (v: unknown) => v === 'spot' ? '现货' : v === 'um' ? 'USDT 永续' : String(v || '')
const number = (v: unknown) => typeof v === 'number' && Number.isFinite(v) ? v.toLocaleString('zh-CN', { maximumFractionDigits: 12 }) : '—'
export function toolTime(v: unknown, dateOnly = false): string {
  if (v == null || v === '' || v === 0) return '—'
  const date = new Date(typeof v === 'number' ? (v < 1e12 ? v * 1000 : v) : String(v))
  if (!Number.isFinite(date.getTime())) return '—'
  return date.toISOString().slice(0, dateOnly ? 10 : 19).replace('T', ' ')
}
export function researchHint(name: string, args: Value, data: unknown): string {
  if (name === 'get_history') {
    const dataset = record(data) && record(data.dataset) ? data.dataset : {}
    const count = record(data) && Array.isArray(data.candles) ? `${data.candles.length} 根` : args.limit ? `请求 ${args.limit} 根` : ''
    return [args.symbol || dataset.symbol || 'BTCUSDT', market(args.market || dataset.market), args.interval || '1d', count].filter(Boolean).join(' · ')
  }
  const path = String(args.path ?? (record(data) ? data.path || '' : ''))
  return [path || (name === 'list_files' ? '工作区根目录' : '路径未提供'), name === 'list_files' && Array.isArray(data) ? `${data.length} 项` : ''].filter(Boolean).join(' · ')
}
export function researchError(name: string, text: string, data: unknown): string {
  if (record(data) && data.error) return String(data.error)
  if (text && researchTool(name) && data === undefined) return text
  return ''
}

export function RawToolData({ text, label = '原始数据' }: { text: string; label?: string }) {
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState('')
  return <details className="tool-raw" onToggle={e => setOpen(e.currentTarget.open)}>
    <summary>{label}</summary>
    {open && <><button type="button" className="tiny" onClick={async () => {
      try { await navigator.clipboard.writeText(text); setCopied('已复制') } catch { setCopied('复制失败，可选中下方文本复制') }
    }}>复制原文</button>{copied && <span className="sub" role="status">{copied}</span>}<pre>{(() => { const value = parseToolValue(text); return value === undefined ? text : JSON.stringify(value, null, 2) })()}</pre></>}
  </details>
}

export function ToolParameters({ args }: { args: Value }) {
  const labels: Record<string, string> = { symbol: '币种', market: '市场', interval: '周期', limit: '请求根数', end: '截止时间（UTC）', path: '路径', revision: '写入前版本', content: '提交正文', seconds: '等待秒数', reason: '原因' }
  const entries = Object.entries(args)
  if (!entries.length) return null
  return <details className="tool-parameters"><summary>调用参数</summary><dl className="tool-fields">{entries.map(([key, value]) => <div key={key}><dt>{labels[key] || key}</dt><dd>{key === 'content' ? `${String(value).length.toLocaleString()} 字符` : key === 'market' ? market(value) : key === 'end' ? value ? toolTime(value) : '最新数据' : record(value) || Array.isArray(value) ? JSON.stringify(value) : String(value ?? '—')}</dd></div>)}</dl></details>
}

export function ResearchToolResult({ name, text, data, args }: { name: string; text: string; data: unknown; args: Value }) {
  const error = researchError(name, text, data)
  if (error) return <div className="tool-error" role="alert"><strong>调用未成功</strong><p>{error}</p></div>
  if (name === 'get_history' && record(data) && Array.isArray(data.candles)) return <HistoryToolResult data={data} args={args} />
  if (name === 'list_files' && Array.isArray(data)) return <div className="tool-file-result">
    <p className="tool-caption">{String(args.path || '工作区根目录')} · {data.length} 项</p>
    {data.length === 0 ? <p className="detail-text">此目录暂无文件。</p> : <ul className="tool-file-list">{data.filter(record).map((file, i) => <li key={String(file.path || i)}><span className="tool-file-icon" aria-hidden="true">{file.directory ? '▸' : '≡'}</span><span>{String(file.path || '未命名')}<small>{file.directory ? '文件夹' : `Markdown · v${file.revision ?? '—'}`}</small></span></li>)}</ul>}
  </div>
  if (record(data) && typeof data.path === 'string') return <div className="tool-file-result">
    <div className="tool-file-heading"><strong>{data.path}</strong><span>{name === 'write_file' ? '已保存' : name === 'create_directory' ? '目录已就绪' : '读取成功'}</span></div>
    <p className="tool-caption">版本 {String(data.revision ?? '—')} · 更新于 {toolTime(data.updatedAt)} UTC</p>
    {typeof data.content === 'string' && !data.directory && <><p className="tool-caption">正文 · {data.content.length.toLocaleString()} 字符</p><div className="tool-document" tabIndex={0} aria-label="Markdown 正文预览"><MarkdownDocument content={data.content} /></div></>}
  </div>
  return <p className="detail-text">返回结果格式无法解析，请展开原始数据查看。</p>
}

function HistoryToolResult({ data, args }: { data: Value; args: Value }) {
  const [page, setPage] = useState(0)
  const rows = useMemo(() => Array.isArray(data.candles) ? data.candles.filter(record).slice().sort((a, b) => Number(b.time) - Number(a.time)) : [], [data])
  const dataset = record(data.dataset) ? data.dataset : {}
  const size = 20
  const pages = Math.max(1, Math.ceil(rows.length / size))
  const current = Math.min(page, pages - 1)
  const incomplete = rows.filter(row => row.closed === false).length
  const statuses: Record<string, string> = { pending: '等待下载', downloading: '下载中', syncing: '同步中', ready: '已就绪', complete: '已完成下载', partial: '部分数据可用', error: '同步异常' }
  return <div className="history-tool-result">
    <div className="tool-file-heading"><strong>{String(dataset.symbol || args.symbol || 'BTCUSDT')} · {market(dataset.market || args.market)} · {String(args.interval || '1d')}</strong><span>本地历史库</span></div>
    <div className="tool-metrics"><div><span>本次返回</span><strong>{rows.length.toLocaleString()} 根 K 线</strong></div><div><span>库内分钟数据</span><strong>{number(dataset.rows)} 根</strong></div></div>
    <dl className="tool-fields">
      <div><dt>本次时间范围</dt><dd>{rows.length ? `${toolTime(rows[rows.length - 1].time)} → ${toolTime(rows[0].time)}` : '本次查询没有返回 K 线'}</dd></div>
      <div><dt>本地库覆盖</dt><dd>{toolTime(dataset.first, true)} → {toolTime(dataset.last, true)}</dd></div>
      <div><dt>下载状态</dt><dd>{statuses[String(dataset.status)] || String(dataset.status || '未提供')} · {number(dataset.gapCount)} 处缺口 / {number(dataset.missingMinutes)} 分钟</dd></div>
      <div><dt>同步至</dt><dd>{toolTime(dataset.syncedThrough)}</dd></div>
      {typeof dataset.source === 'string' && <div><dt>数据来源</dt><dd>{dataset.source}</dd></div>}
    </dl>
    <p className="tool-caption">时间均为 UTC。K 线由新到旧排列；历史收盘价不代表当前报价。</p>
    {dataset.error ? <p className="tool-warning">同步提示：{String(dataset.error)}</p> : null}
    {incomplete > 0 && <p className="tool-warning">有 {incomplete} 根未收盘或分钟样本不完整，已标记。</p>}
    {rows.length > 0 && <details className="tool-candles"><summary>查看 K 线明细 · {rows.length.toLocaleString()} 根</summary><p className="tool-caption">表格可左右滚动，每页 20 根。</p>
      <div className="candle-wrap tool-history-table" tabIndex={0} aria-label="历史K线明细，可横向滚动"><table className="candle-table"><thead><tr><th>开盘时间（UTC）</th><th>开盘</th><th>最高</th><th>最低</th><th>收盘</th><th>成交量</th><th>状态</th></tr></thead><tbody>
        {rows.slice(current * size, (current + 1) * size).map((row, i) => <tr key={String(row.time) + i} className={row.closed === false ? 'current' : undefined}><td>{toolTime(row.time)}</td>{['open', 'high', 'low', 'close', 'volume'].map(key => <td key={key}>{number(row[key])}</td>)}<td>{row.closed === false ? '未收盘 / 不完整' : row.closed === true ? '已收盘' : '未提供'}</td></tr>)}
      </tbody></table></div>
      <div className="tool-pagination"><span>{current * size + 1}–{Math.min((current + 1) * size, rows.length)} / {rows.length} 根</span><button type="button" className="btn" disabled={current === 0} onClick={() => setPage(current - 1)}>上一页</button><button type="button" className="btn" disabled={current + 1 >= pages} onClick={() => setPage(current + 1)}>下一页</button></div>
    </details>}
    <p className="tool-caption">{data.hasMore === true ? '本地库还有更早数据；本卡片仅显示这次工具返回的内容。' : '该查询范围内没有更早的本地数据。'}</p>
  </div>
}
