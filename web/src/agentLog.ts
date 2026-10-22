import type { ChatLine } from './api'

// HTTP snapshots can arrive after newer WebSocket events. Transcript lines are
// immutable: reconcile by ID instead of replacing the visible history.
export function mergeChatLines(current: ChatLine[], incoming: ChatLine[]) {
  const byID = new Map(current.map((line) => [line.id, line]))
  for (const line of incoming) {
    if (!byID.has(line.id)) byID.set(line.id, line)
  }
  if (byID.size === current.length) return current
  return [...byID.values()].sort((a, b) => a.id - b.id)
}

export type ChatBlock =
  | { kind: 'line'; line: ChatLine }
  | { kind: 'call'; call: ChatLine; result?: ChatLine }

export function groupLines(lines: ChatLine[]): ChatBlock[] {
  const blocks: ChatBlock[] = []
  const pending = new Map<string, Extract<ChatBlock, { kind: 'call' }>[]>()
  for (const line of lines) {
    if (line.role === 'tool') {
      const block: Extract<ChatBlock, { kind: 'call' }> = { kind: 'call', call: line }
      blocks.push(block)
      // wait is a completed step with no separate result event.
      if (line.name !== 'wait') {
        const queue = pending.get(line.name || '') || []
        queue.push(block)
        pending.set(line.name || '', queue)
      }
    } else if (line.role === 'result') {
      // User/status messages can be inserted while a tool is running.
      const call = pending.get(line.name || '')?.shift()
      if (call) call.result = line
      else blocks.push({ kind: 'call', call: line })
    } else {
      blocks.push({ kind: 'line', line })
    }
  }
  return blocks
}
