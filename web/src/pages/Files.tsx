import { useEffect, useState } from 'react'
import { api, type WorkspaceFile } from '../api'
import { MarkdownDocument } from '../components/MarkdownDocument'

const message = (error: unknown) => error instanceof Error ? error.message : '文件操作失败'

export function Files() {
  const [rows, setRows] = useState<WorkspaceFile[]>([])
  const [file, setFile] = useState<WorkspaceFile | null>(null)
  const [text, setText] = useState('')
  const [editing, setEditing] = useState(false)
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const dirty = file !== null && text !== file.content

  useEffect(() => {
    let active = true
    async function load() {
      const nextRows = await api.files('')
      if (!active) return
      setRows(nextRows)
      if (!nextRows.some(row => row.path === 'INVEST.MD' && !row.directory)) return
      const next = await api.readFile('INVEST.MD')
      if (active) { setFile(next); setText(next.content) }
    }
    load().catch(err => { if (active) setError(message(err)) }).finally(() => { if (active) setBusy(false) })
    return () => { active = false }
  }, [])

  async function run(action: () => Promise<void>) {
    setBusy(true); setError(''); setNotice('')
    try { await action() } catch (err) { setError(message(err)) } finally { setBusy(false) }
  }
  async function open(path: string) {
    if (dirty) { setError('请先保存或放弃当前文件的修改。'); return }
    if (path.includes('/')) { setError('请打开根目录里的文件。'); return }
    await run(async () => {
      const next = await api.readFile(path)
      setFile(next); setText(next.content); setEditing(false)
    })
  }
  async function create() {
    if (dirty) { setError('请先保存或放弃当前文件的修改。'); return }
    const value = name.trim()
    if (!value || value.includes('/') || value.includes('\\')) { setError('只填写文件名，不要填写文件夹路径。'); return }
    if (value.toLowerCase() === 'invest.md') { setError('只允许编辑已有的根目录 INVEST.MD。'); return }
    const path = value.toLowerCase().endsWith('.md') ? value : value + '.md'
    await run(async () => {
      const next = await api.writeFile(path, '', 0)
      setFile(next); setText(''); setEditing(true); setRows(await api.files(''))
      setName(''); setCreating(false)
    })
  }

  return <div className="page files-page">
    <div className="page-head"><div><span className="eyebrow">WORKSPACE</span><h1>文件</h1><p className="sub">INVEST.MD 写策略，验证.md 写测试，复盘.md 写问题和修改原因。</p></div></div>
    {error && <p className="err" role="alert">{error}</p>}{notice && <p className="ok" role="status">{notice}</p>}
    <div className="files-layout">
      <section className="card files-browser" aria-label="文件浏览器">
        <div className="files-tree-toolbar"><strong>全部文件</strong><div className="row gap">
          <button className="tiny" aria-label="新建" aria-expanded={creating} disabled={busy} onClick={() => setCreating(!creating)}>＋</button>
          <button className="tiny" aria-label="刷新文件列表" disabled={busy} onClick={() => run(async () => { setRows(await api.files('')); setNotice('文件列表已刷新') })}>↻</button>
        </div></div>
        {creating && <div className="file-create"><label htmlFor="file-name">新建名称</label><input id="file-name" autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="文件名" disabled={busy} />
          <button className="btn" disabled={busy || !name.trim()} onClick={create}>新建 Markdown</button></div>}
        <div className="file-entries" role="tree" aria-label="工作区文件" onKeyDown={e => {
          if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) return
          const items = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="treeitem"]:not(:disabled)')]
          const index = items.indexOf(e.target as HTMLButtonElement)
          const next = e.key === 'Home' ? 0 : e.key === 'End' ? items.length - 1 : Math.max(0, Math.min(items.length - 1, index + (e.key === 'ArrowDown' ? 1 : -1)))
          e.preventDefault(); items[next]?.focus()
        }}>{rows.filter(row => !row.directory).map(row => <button key={row.path} type="button" role="treeitem" aria-level={1} aria-selected={file?.path === row.path}
          className={'file-entry ' + (file?.path === row.path ? 'selected' : '')} disabled={busy} onClick={() => open(row.path)}>
          <span className="file-tree-icon" aria-hidden="true">≡</span><span>{row.path}</span>
        </button>)}{!busy && !rows.length && <p className="sub">暂无文件。</p>}</div>
        <p className="sub files-tree-note">直接修改已有文件，不用另建版本。</p>
      </section>
      <section className="card files-editor">
        {file ? <>
          <div className="panel-heading"><div><h2>{file.path}</h2><p className="sub">根目录{dirty ? ' · 未保存' : ''}</p></div><button className="btn" disabled={busy} aria-pressed={editing} onClick={() => setEditing(!editing)}>{editing ? '收起编辑器' : '编辑'}</button></div>
          <div className={'file-document-layout ' + (editing ? 'editing' : '')}>
            {editing && <textarea aria-label="Markdown 正文" value={text} onChange={e => setText(e.target.value)} disabled={busy} spellCheck={false} />}
            <article className="file-rendered" aria-label="Markdown 文档"><MarkdownDocument content={text} currentPath={file.path} onOpenFile={path => { void open(path) }} /></article>
          </div>
          <div className="editor-footer"><div className="row gap"><button className="tiny" disabled={busy} onClick={() => {
            const url = URL.createObjectURL(new Blob([text], { type: 'text/markdown;charset=utf-8' })); const a = document.createElement('a'); a.href = url; a.download = file.path; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000)
          }}>下载 .md</button><button className="tiny" disabled={busy} onClick={() => {
            if (dirty && !window.confirm('放弃草稿并读取最新版本？')) return
            void run(async () => { const next = await api.readFile(file.path); setFile(next); setText(next.content) })
          }}>读取最新 / 放弃修改</button></div>{editing && <button className="btn primary" disabled={busy || !dirty} onClick={() => run(async () => {
            const next = await api.writeFile(file.path, text, file.revision); setFile(next); setText(next.content); setNotice('文件已保存'); setRows(await api.files(''))
          })}>保存文件</button>}</div>
        </> : <div className="files-empty"><h2>选择一份文档</h2><p className="sub">点击左侧文件阅读。</p></div>}
      </section>
    </div>
  </div>
}
