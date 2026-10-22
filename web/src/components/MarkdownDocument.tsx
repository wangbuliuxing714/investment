import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

// Only relative Markdown paths are workspace links; normal web/anchor links keep
// ReactMarkdown's default URL sanitization. Never resolve above the workspace.
function workspaceLink(href: string, currentPath: string): string | null {
  if (!href || /^[a-z][a-z\d+.-]*:/i.test(href) || /^[/#?]/.test(href)) return null
  let decoded: string
  try { decoded = decodeURIComponent(href.split(/[?#]/)[0]) } catch { return null }
  if (!/\.md$/i.test(decoded) || /[\\:\x00-\x1f]/.test(decoded) || decoded.startsWith('/')) return null
  const parts = currentPath.split('/').slice(0, -1)
  for (const part of decoded.split('/')) {
    if (!part || part === '.') continue
    if (part === '..') { if (!parts.length) return null; parts.pop() }
    else parts.push(part)
  }
  return parts.join('/')
}

export function MarkdownDocument({ content, currentPath = '', onOpenFile }: { content: string; currentPath?: string; onOpenFile?: (path: string) => void }) {
  return <div className="markdown-document"><ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={onOpenFile ? {
    a: ({ href, children, node: _node, ...props }) => {
      const path = workspaceLink(href || '', currentPath)
      const invalidPath = !path && !!href && !/^([a-z][a-z\d+.-]*:|[/#?])/i.test(href) && /\.md(?:[?#]|$)/i.test(href)
      return <a {...props} href={href} aria-disabled={invalidPath || undefined} onClick={path || invalidPath ? e => { e.preventDefault(); if (path) onOpenFile(path) } : undefined}>{children}</a>
    },
  } : undefined}>{content || '*暂无内容。*'}</ReactMarkdown></div>
}
