import { useEffect, useState } from 'react'
import { api, type PromptSettingsView } from '../api'
import { TemplateEditor } from './TemplateEditor'
import { MarkdownDocument } from './MarkdownDocument'

export function PromptSettings({ onOpenTools, onOpenStrategy }: { onOpenTools: () => void; onOpenStrategy: () => void }) {
  const [config, setConfig] = useState<PromptSettingsView | null>(null)
  const [values, setValues] = useState<Record<string, string>>({})
  const [investText, setInvestText] = useState('')
  const [active, setActive] = useState('system')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  useEffect(() => {
    api.prompts().then((next) => {
      if (!next.prompts || !next.invest || !Array.isArray(next.definitions) || !['system', 'trading', 'optimization'].every((key) => next.definitions.some((d) => d.key === key))) throw new Error('请更新引擎以使用分模式指令。')
      setConfig(next); setValues(next.prompts); setInvestText(next.invest.content)
    }).catch((err: Error) => setError(err.message))
  }, [])
  const promptsDirty = !!config && Object.keys(values).some((key) => values[key] !== config.prompts[key])
  const investDirty = !!config && investText !== config.invest.content
  const dirty = promptsDirty || investDirty
  const field = config?.definitions.find((item) => item.key === active)
  function navigate(action: () => void) { if (dirty) { setError('请先保存提示词，再切换设置页面。'); return }; action() }
  async function save() {
    setSaving(true); setError(''); setMessage('')
    try { const next = await api.savePrompts(values); setConfig((prev) => ({ ...next, invest: prev?.invest ?? next.invest })); setValues(next.prompts); setMessage('提示词已保存，将在下一次请求或对应事件触发时生效。') }
    catch (err) { setError(err instanceof Error ? err.message : '保存失败') }
    finally { setSaving(false) }
  }
  async function saveInvest() {
    if (!config) return
    setSaving(true); setError(''); setMessage('')
    try {
      const next = await api.writeFile('INVEST.MD', investText, config.invest.revision)
      setConfig({ ...config, invest: next }); setInvestText(next.content)
      setMessage('INVEST.MD 已保存，下一次模型请求自动加载。')
    } catch (err) { setError(err instanceof Error ? err.message : '保存失败') }
    finally { setSaving(false) }
  }
  async function reloadInvest() {
    if (investDirty && !window.confirm('放弃 INVEST.MD 草稿并读取最新版本？')) return
    setSaving(true); setError(''); setMessage('')
    try {
      const next = await api.readFile('INVEST.MD')
      setConfig((prev) => prev && ({ ...prev, invest: next })); setInvestText(next.content)
    } catch (err) { setError(err instanceof Error ? err.message : '读取失败') }
    finally { setSaving(false) }
  }
  function loadPreset(prompt: string, name: string) {
    setInvestText((prev) => prev + '\n\n## 策略档案：' + name + '\n' + prompt)
    setActive('invest')
    setError('')
    setMessage('已载入「' + name + '」草稿；点击保存后才会生效。')
  }
  return <section className="card prompt-settings">
    <div className="panel-heading"><div><span className="eyebrow">AGENT INSTRUCTIONS</span><h2>系统与模式指令</h2></div><button className="btn" type="button" onClick={() => navigate(onOpenTools)}>打开 Tools 管理</button></div>
    {error && <p className="err" role="alert">{error}</p>}
    {!config && !error && <div className="loading-surface">正在读取提示词…</div>}
    {config && <>
      <div className="context-strip"><span className="status-orb" /><span>每次加载系统提示词，再按策略优化开关只加载对应模式指令。<strong>INVEST.MD</strong> 只放策略。</span></div>
      {(config.presets ?? []).length > 0 && <details className="strategy-presets">
        <summary>策略档案 <span className="sub">仅载入草稿，不会自动启用</span></summary>
        <div className="strategy-preset-list">{(config.presets ?? []).map((preset) => <div className="strategy-preset" key={preset.id}>
          <div><strong>{preset.name}</strong><p className="sub">{preset.description}</p></div>
          <button type="button" className="btn" disabled={saving} onClick={() => loadPreset(preset.prompt, preset.name)}>追加到 INVEST.MD 草稿</button>
        </div>)}</div>
      </details>}
      <div className="prompt-workbench">
        <nav className="prompt-index" aria-label="提示词场景">
          <button type="button" data-prompt-key="invest" className={active === 'invest' ? 'instruction-tab on' : 'instruction-tab'} aria-pressed={active === 'invest'} onClick={() => { setActive('invest'); setError(''); setMessage('') }}><span className="instruction-icon">▤</span><span><strong>INVEST.MD</strong><small>仅策略 · 每次请求</small></span>{investDirty && <i className="draft-dot" />}</button>
          {config.definitions.map((d) => <button type="button" key={d.key} data-prompt-key={d.key} className={active === d.key ? 'instruction-tab on' : 'instruction-tab'} aria-pressed={active === d.key} onClick={() => { setActive(d.key); setError('') }}>
            <span className="instruction-icon">{d.key === 'system' ? '◈' : '↳'}</span><span><strong>{d.label}</strong><small>{d.key === 'system' ? '两种模式共用 · SYSTEM' : d.key === 'trading' ? '开关关闭时加载' : d.key === 'optimization' ? '开关开启时加载' : d.key === 'chat' ? '面板对话' : '按事件触发'}</small></span>{values[d.key] !== config.prompts[d.key] && <i className="draft-dot" />}
          </button>)}
          <div className="index-note">通用要求放系统提示词；交易和优化流程各放对应模式。开关关闭＝交易，开启＝优化。INVEST.MD 只维护策略。</div>
        </nav>
        {field && <div className="prompt-document">
          <div className="document-title"><div><h3>{field.label}</h3><p className="sub">{field.description}</p></div><span className={'role-tag ' + (field.role === 'system' ? 'system' : '')}>{field.role === 'system' ? 'SYSTEM' : '场景指令'}</span></div>
          <details className="usage-details"><summary>使用场景与生效时机</summary><dl className="prompt-metadata"><dt>使用场景</dt><dd>{field.scenario}</dd><dt>发送位置</dt><dd>{field.placement}</dd><dt>生效时机</dt><dd>{field.effectiveWhen}</dd></dl></details>
          <TemplateEditor key={field.key} label={field.label} value={values[field.key] ?? ''} parameters={config.parameters ?? []} disabled={saving} maxLength={field.key === 'system' ? 120000 : 30000} onGoStrategy={() => navigate(onOpenStrategy)} onChange={(value) => { setValues((prev) => ({ ...prev, [field.key]: value })); setMessage('') }} />
          <div className="editor-footer"><button type="button" className="btn" disabled={saving} onClick={() => { setValues((prev) => ({ ...prev, [field.key]: field.default })); setMessage('') }}>填入默认文本（待保存）</button><div className="row gap"><span className="sub">{promptsDirty ? '有未保存的修改' : '已与引擎同步'}</span><button type="button" className="btn primary" disabled={saving || !promptsDirty} onClick={save}>{saving ? '正在保存…' : '保存全部提示词'}</button></div></div>
          {message && <p className="ok" role="status">{message}</p>}
        </div>}
        {active === 'invest' && <div className="prompt-document">
          <div className="document-title"><div><h3>INVEST.MD</h3><p className="sub">只放买卖条件、仓位、风控参数和研究草案。与「文件」共享文档，版本 {config.invest.revision}；保存后下次请求自动注入。</p></div><span className="role-tag system">策略</span></div>
          <TemplateEditor label="INVEST.MD 正文" value={investText} parameters={config.parameters ?? []} disabled={saving} maxLength={128 * 1024} onGoStrategy={() => navigate(onOpenStrategy)} onChange={(value) => { setInvestText(value); setMessage('') }} />
          <article className="instruction-markdown-preview" aria-label="INVEST.MD 渲染预览"><MarkdownDocument content={investText} /></article>
          <div className="editor-footer"><button className="btn" disabled={saving} onClick={reloadInvest}>读取最新 / 放弃草稿</button><div className="row gap"><span className="sub">{investDirty ? '有未保存的修改' : '已与文件同步'}</span><button className="btn primary" disabled={saving || !investDirty} onClick={saveInvest}>{saving ? '正在保存…' : '保存 INVEST.MD'}</button></div></div>
          {message && <p className="ok" role="status">{message}</p>}
          <p className="sub">与 AI 同时编辑发生版本冲突时会保留草稿。可先复制草稿，再读取最新版本合并。</p>
        </div>}
      </div>
      <details className="prompt-saved-preview"><summary>已保存的注入内容预览<span className="sub">参数已替换为实际值</span></summary><p className="sub">系统提示词共用，模式指令按开关二选一；还会加载最新策略和 Tools。面板规则两种模式均生效。</p><h4>系统提示词（共用）</h4><article className="instruction-markdown-preview" data-preview="system"><MarkdownDocument content={config.preview.system} /></article><h4>开关关闭：交易模式</h4><article className="instruction-markdown-preview" data-preview="trading"><MarkdownDocument content={config.preview.trading} /></article><h4>开关开启：策略优化模式</h4><article className="instruction-markdown-preview" data-preview="optimization"><MarkdownDocument content={config.preview.optimization} /></article></details>
    </>}
  </section>
}
