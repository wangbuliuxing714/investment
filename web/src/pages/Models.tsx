import { useEffect, useState } from 'react'
import { PromptSettings } from '../components/PromptSettings'
import { api, type AdminModel, type Settings } from '../api'

export function Models({ onOpenTools, onOpenStrategy }: { onOpenTools: () => void; onOpenStrategy: () => void }) {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [models, setModels] = useState<AdminModel[]>([])
  const [query, setQuery] = useState('')
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [choosing, setChoosing] = useState(false)
  useEffect(() => {
    api.settings().then(setSettings).catch((err: Error) => setError(err.message))
    api.models().then(setModels).catch((err: Error) => setError(err.message)).finally(() => setLoading(false))
  }, [])
  async function choose(model: AdminModel) {
    setError(''); setMessage(''); setChoosing(true)
    try { setSettings(await api.saveModel(model)); setMessage('已使用 ' + model.name) }
    catch (err) { setError(err instanceof Error ? err.message : '保存模型失败') }
    finally { setChoosing(false) }
  }
  const filtered = models.filter((m) => (m.name + ' ' + m.providerId).toLowerCase().includes(query.toLowerCase()))
  return <div className="page models-page">
    <div className="page-head"><div><span className="eyebrow">INTELLIGENCE</span><h1>模型与指令</h1><p className="sub">选择模型，定义代理的判断方式。</p></div><span className="pill subtle">配置本地保存</span></div>
    <div className="models-layout">
      <aside className="card model-catalog">
        <div className="panel-heading"><h2>模型库</h2><span className="count-tag">{models.length}</span></div>
        <div className="active-model"><span className="status-orb" /><div><small>当前模型</small><strong>{settings?.modelName || '尚未选择'}</strong></div></div>
        <input className="search-input" type="search" aria-label="搜索模型" placeholder="搜索模型或通道" value={query} onChange={(e) => setQuery(e.target.value)} />
        {error && <p className="err" role="alert">{error}</p>}
        {loading && <p className="sub">正在读取模型…</p>}
        <div className="models">{filtered.map((model) => <button type="button" aria-label={model.name} aria-pressed={settings?.modelId === model.id} disabled={choosing} key={model.id} className={settings?.modelId === model.id ? 'model on' : 'model'} onClick={() => choose(model)}><span className="model-glyph">✧</span><span><strong>{model.name}</strong><small>{model.providerId}</small></span><i className="selection-dot" /></button>)}</div>
        {!loading && !filtered.length && <p className="sub">没有匹配的模型</p>}
        {message && <p className="ok">{message}</p>}
        <div className="catalog-footnote">切换模型保留全部指令。通道地址和密钥由引擎管理。</div>
      </aside>
      <PromptSettings onOpenTools={onOpenTools} onOpenStrategy={onOpenStrategy} />
    </div>
  </div>
}
