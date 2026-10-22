import { StrictMode, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Dashboard } from './pages/Dashboard'
import { Exchange } from './pages/Exchange'
import { Models } from './pages/Models'
import { Strategy } from './pages/Strategy'
import { Tools } from './pages/Tools'
import { Files } from './pages/Files'
import { History } from './pages/History'
import './styles.css'
import './workspace.css'

type Page = 'board' | 'exchange' | 'models' | 'strategy' | 'tools' | 'files' | 'history'

function App() {
  const [page, setPage] = useState<Page>('board')
  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand"><span className="brand-mark">◈</span><span>交易台<small>Investment desk</small></span></div>
        <nav>
          <button className={page === 'board' ? 'nav on' : 'nav'} onClick={() => setPage('board')}><span className="nav-glyph" aria-hidden="true">◈</span>看板</button>
          <button className={page === 'history' ? 'nav on' : 'nav'} onClick={() => setPage('history')}><span className="nav-glyph" aria-hidden="true">↶</span>历史行情</button>
          <button className={page === 'exchange' ? 'nav on' : 'nav'} onClick={() => setPage('exchange')}><span className="nav-glyph" aria-hidden="true">⇄</span>交易所</button>
          <button className={page === 'models' ? 'nav on' : 'nav'} onClick={() => setPage('models')}><span className="nav-glyph" aria-hidden="true">✧</span>模型</button>
          <button className={page === 'tools' ? 'nav on' : 'nav'} onClick={() => setPage('tools')}><span className="nav-glyph" aria-hidden="true">⌘</span>Tools 管理</button>
          <button className={page === 'strategy' ? 'nav on' : 'nav'} onClick={() => setPage('strategy')}><span className="nav-glyph" aria-hidden="true">≋</span>策略</button>
          <button className={page === 'files' ? 'nav on' : 'nav'} onClick={() => setPage('files')}><span className="nav-glyph" aria-hidden="true">▤</span>文件</button>
        </nav>
        <div className="side-note"><span className="status-orb" />本机工作空间</div>
      </aside>
      <main>
        {page === 'history' && <History />}
        {page === 'files' && <Files />}
        {page === 'board' && <Dashboard onOpenExchange={() => setPage('exchange')} />}
        {page === 'exchange' && <Exchange />}
        {page === 'models' && <Models onOpenTools={() => setPage('tools')} onOpenStrategy={() => setPage('strategy')} />}
        {page === 'tools' && <Tools onOpenStrategy={() => setPage('strategy')} />}
        {page === 'strategy' && <Strategy />}
      </main>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
