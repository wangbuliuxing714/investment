import { useState } from 'react'
import { api, type Position, type ProtectionResult } from '../api'
import { px, sideLabel } from '../format'

export function ProtectionValue({ position, kind }: { position: Position; kind: 'stop' | 'take' }) {
  if (!position.protectionKnown) return <span className="protection-missing" title={position.protectionError || '尚未查询交易所'}>{position.protectionError ? '读取失败' : '未知'}</span>
  const levels = position.protection?.filter(line => line.kind === kind && line.protective) ?? []
  return levels.length ? <>{levels.map((line, i) => <div key={i} title={line.workingType === 'MARK_PRICE' ? '标记价触发' : '合约成交价触发'}>{px(line.price)}<small className="trigger-label">{line.workingType === 'MARK_PRICE' ? '标记价' : '成交价'}</small></div>)}</> : <span className="protection-missing">未设置</span>
}

export function PositionProtection({ position, disabled, onRefresh, onClose }: { position: Position; disabled: boolean; onRefresh: () => Promise<unknown>; onClose: () => void }) {
  const [stop, setStop] = useState('')
  const [take, setTake] = useState('')
  const [pending, setPending] = useState(false)
  const [result, setResult] = useState<ProtectionResult | null>(null)
  const [error, setError] = useState('')
  const hasStop = !!position.protection?.some(line => line.protective && line.kind === 'stop')
  const hasTake = !!position.protection?.some(line => line.protective && line.kind === 'take')
  async function submit(event: React.FormEvent) {
    event.preventDefault()
    if (pending || disabled) return
    setPending(true); setError(''); setResult(null)
    try {
      const s = hasStop ? 0 : Number(stop), t = hasTake ? 0 : Number(take)
      if (![s, t].every(Number.isFinite) || s < 0 || t < 0 || (!s && !t)) throw new Error('请填写至少一个缺失的止盈或止损价格。')
      if (s > 0 && (position.side === 'long' ? s >= position.mark : s <= position.mark)) throw new Error('止损价必须位于当前标记价的亏损方向。')
      if (t > 0 && (position.side === 'long' ? t <= position.mark : t >= position.mark)) throw new Error('止盈价必须位于当前标记价的盈利方向。')
      const next = await api.setProtection({ symbol: position.symbol, side: position.side, stop_loss: s, take_profit: t })
      setResult(next)
      await onRefresh()
    } catch (err) { setError(err instanceof Error ? err.message : '补挂失败，请刷新核对交易所状态') }
    finally { setPending(false) }
  }
  return <form className="protection-form" onSubmit={submit} aria-label="补挂持仓保护单">
    <div className="row gap wrap"><strong>{position.symbol} · {sideLabel(position.side)} · 补挂止盈止损</strong><button className="tiny" type="button" disabled={pending} onClick={onClose}>关闭</button></div>
    <p className="sub">当前标记价 {px(position.mark)}。按标记价触发，平掉该方向全部持仓；保留已生效保护单，仅补缺失项。</p>
    <div className="protection-inputs">
      <label>止损触发价{hasStop ? <ProtectionValue position={position} kind="stop" /> : <input aria-label="止损触发价" type="number" min="0" step="any" value={stop} onChange={e => setStop(e.target.value)} placeholder="未设置，可填写" disabled={pending} />}</label>
      <label>止盈触发价{hasTake ? <ProtectionValue position={position} kind="take" /> : <input aria-label="止盈触发价" type="number" min="0" step="any" value={take} onChange={e => setTake(e.target.value)} placeholder="未设置，可填写" disabled={pending} />}</label>
      <button className="btn" type="submit" disabled={disabled || pending || !position.protectionKnown || (hasStop && hasTake)}>{pending ? '正在向交易所提交…' : '确认补挂保护单'}</button>
    </div>
    {!position.protectionKnown && <p className="err">保护状态尚未确认，请先刷新持仓。</p>}
    {error && <p className="err" role="alert">{error}</p>}
    {result && <p className={result.confirmed ? 'up' : 'err'} role="status">{result.message}</p>}
  </form>
}
