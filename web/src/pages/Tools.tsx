import { useEffect, useState } from 'react'
import { api, type ToolsSettingsView } from '../api'
import { TemplateEditor } from '../components/TemplateEditor'

export function Tools({ onOpenStrategy }: { onOpenStrategy: () => void }) {
  const [config, setConfig] = useState<ToolsSettingsView | null>(null)
  const [instructions, setInstructions] = useState('')
  const [query, setQuery] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const dirty = !!config && instructions !== config.config.instructions
  useEffect(() => { api.tools().then((next) => {
    if (!next.config || !Array.isArray(next.definitions)) throw new Error('当前引擎尚不支持 Tools 管理。')
    setConfig(next); setInstructions(next.config.instructions)
  }).catch((err: Error) => setError(err.message)) }, [])
  async function save() {
    setSaving(true); setError(''); setMessage('')
    try { const next = await api.saveTools(instructions); setConfig(next); setInstructions(next.config.instructions); setMessage('工具说明已独立保存，下一次请求刷新系统上下文。') }
    catch (err) { setError(err instanceof Error ? err.message : '保存失败') }
    finally { setSaving(false) }
  }
  return <div className="page tools-page">
    <div className="page-head"><div><span className="eyebrow">CAPABILITIES</span><h1>Tools 管理</h1><p className="sub">工具协议独立维护，执行能力一目了然。</p></div><span className="pill subtle">{config?.definitions.length ?? '—'} 个内置工具</span></div>
    {error && <p className="err" role="alert">{error}</p>}
    {!config && !error && <div className="card loading-surface">正在读取工具配置…</div>}
    {config && <div className="tools-layout">
      <section className="card tools-settings" aria-labelledby="tools-instructions-heading">
        <div className="panel-heading"><div><span className="eyebrow">TOOL CONTEXT</span><h2 id="tools-instructions-heading">工具说明与调用规范</h2></div><span className="role-tag system">SYSTEM 上下文</span></div>
        <div className="context-strip"><span className="status-orb" /><span>每次请求自动加入系统上下文。输出协议在这里维护，交易原则在模型页面管理。</span></div>
        <TemplateEditor label="工具说明" value={instructions} parameters={config.parameters ?? []} disabled={saving} onChange={(value) => { setInstructions(value); setMessage('') }} onGoStrategy={() => { if (dirty) { setError('请先保存工具说明，再前往策略设置。'); return }; onOpenStrategy() }} />
        <div className="editor-footer"><button type="button" className="btn" disabled={saving} onClick={() => { setInstructions(config.defaults.instructions); setMessage('') }}>填入默认工具说明（待保存）</button><button type="button" className="btn primary" disabled={saving || !dirty} onClick={save}>{saving ? '正在保存…' : '保存工具说明'}</button></div>
        {dirty && <p className="sub">有未保存的修改</p>}{message && <p className="ok" role="status">{message}</p>}
        <details className="tools-preview"><summary>已保存的工具上下文预览</summary><pre className="prompt-preview">{config.preview}</pre></details>
      </section>
      <section className="card tools-catalog" aria-labelledby="tools-catalog-heading">
        <div className="panel-heading"><h2 id="tools-catalog-heading">执行接口</h2><span className="count-tag">{config.definitions.length}</span></div>
        <input type="search" className="search-input" aria-label="搜索工具" placeholder="搜索工具名称或用途" value={query} onChange={(e) => setQuery(e.target.value)} />
        <p className="sub">参数与权限由程序定义。这里仅查看，不执行工具。</p>
        <div className="tool-directory">{config.definitions.filter((t) => (t.name + t.label + t.description).toLowerCase().includes(query.toLowerCase())).map((tool) => <details key={tool.name} data-tool-name={tool.name}>
          <summary><span className="tool-summary"><strong>{tool.label}</strong><code>{tool.name}</code></span><span className={'role-tag ' + (tool.effect === '可能下单' ? 'caution' : '')}>{tool.effect}</span></summary>
          <p>{tool.description}</p><p className="sub">{tool.scope}</p>
          {tool.parameters.length ? <dl className="tool-parameters">{tool.parameters.map((p) => <div key={p.name}><dt><code>{p.name}</code><span className="sub">{p.type} · {p.required ? '必填' : '可选'}</span></dt><dd>{p.description}</dd></div>)}</dl> : <p className="sub">无需参数，arguments 为 {'{}'}。</p>}
          <h3>调用示例</h3><pre className="prompt-preview">{tool.example}</pre>
        </details>)}</div>
      </section>
    </div>}
  </div>
}
