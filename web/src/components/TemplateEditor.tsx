import { useRef, useState } from 'react'
import type { PromptParameter } from '../api'

export function TemplateEditor({ label, value, onChange, parameters, disabled = false, maxLength = 30000, onGoStrategy }: {
  label: string; value: string; onChange: (value: string) => void; parameters: PromptParameter[]
  disabled?: boolean; maxLength?: number; onGoStrategy: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  const area = useRef<HTMLTextAreaElement>(null)
  const parameter = parameters.find((p) => p.key === selected)
  function insert(key: string) {
    const start = area.current?.selectionStart ?? value.length
    const end = area.current?.selectionEnd ?? value.length
    const token = '{{' + key + '}}'
    const next = value.slice(0, start) + token + value.slice(end)
    if (next.length > maxLength) return
    onChange(next); setEditing(true)
    requestAnimationFrame(() => { area.current?.focus(); area.current?.setSelectionRange(start + token.length, start + token.length) })
  }
  return <div className="template-editor">
    <div className="editor-controls">
      <div className="segmented" role="group" aria-label={label + '显示方式'}>
        <button type="button" aria-pressed={!editing} onClick={() => setEditing(false)}>参数视图</button>
        <button type="button" aria-pressed={editing} onClick={() => setEditing(true)}>编辑原文</button>
      </div>
      <span className="sub">{value.length.toLocaleString()} 字</span>
    </div>
    {editing ? <textarea ref={area} aria-label={label} className="template-source" value={value} maxLength={maxLength} disabled={disabled} spellCheck={false} onChange={(e) => onChange(e.target.value)} /> :
      <div className="template-surface" aria-label={label + '参数视图'}>
        {value ? value.split(/(\{\{[a-zA-Z][a-zA-Z0-9]*\}\})/g).map((part, index) => {
          const key = part.startsWith('{{') && part.endsWith('}}') ? part.slice(2, -2) : ''
          const p = parameters.find((item) => item.key === key)
          return p ? <button type="button" className="parameter-token" key={index} aria-label={p.label + '，' + p.value + p.unit} title={'来自策略 · ' + p.label} onClick={() => setSelected(selected === p.key ? null : p.key)}>{p.label}<span>{p.value}</span></button> : <span key={index}>{part}</span>
        }) : <span className="sub">暂无内容，点击「编辑原文」添加。</span>}
      </div>}
    {parameter && <div className="parameter-inspector" role="status">
      <span><strong>{parameter.label}</strong> · 当前为 {parameter.value}{parameter.unit}，发送时会替换成实际策略值。</span>
      <button className="btn" type="button" onClick={onGoStrategy}>前往策略设置</button>
      <button className="icon-btn" type="button" aria-label="关闭参数说明" onClick={() => setSelected(null)}>×</button>
    </div>}
    <details className="parameter-library"><summary>插入策略参数<span>中文标签 · 自动取值</span></summary>
      <div className="parameter-buttons">{parameters.map((p) => <button className="parameter-token" type="button" key={p.key} disabled={disabled} onMouseDown={(e) => e.preventDefault()} onClick={() => insert(p.key)} title={'插入 {{' + p.key + '}}'}>{p.label}<span>{p.value}{p.unit}</span></button>)}</div>
    </details>
  </div>
}
