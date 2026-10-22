import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/agentLog.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
})
const { mergeChatLines, groupLines } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)
const line = (id, role = 'tool', name = 'get_account') => ({ id, role, name, text: `line ${id}`, at: '2026-09-29T00:00:00Z' })

test('an older HTTP snapshot cannot remove a live tool call or its result', () => {
  const history = [line(1, 'agent'), line(2), line(3, 'result')]
  assert.strictEqual(mergeChatLines(history, [history[0]]), history)
  assert.strictEqual(mergeChatLines(history, []), history)
})

test('late snapshots fill gaps and out-of-order events are sorted by persistent ID', () => {
  const latest = line(4, 'result')
  const merged = mergeChatLines([latest], [line(3), line(1), line(2, 'result')])
  assert.deepEqual(merged.map((item) => item.id), [1, 2, 3, 4])
  assert.strictEqual(merged[3], latest)
})

test('repeated HTTP and WebSocket deliveries do not duplicate or remount cards', () => {
  const history = [line(1), line(2, 'result')]
  assert.strictEqual(mergeChatLines(history, history.map((item) => ({ ...item }))), history)
  assert.deepEqual(mergeChatLines([], [line(1), line(1)]), [line(1)])
})

test('a bounded snapshot does not trim already visible history', () => {
  const history = Array.from({ length: 501 }, (_, i) => line(i + 1))
  assert.strictEqual(mergeChatLines(history, history.slice(1)), history)
})

test('tool results attach across interleaved user and status messages', () => {
  const tool = line(1)
  const user = line(2, 'user', '')
  const status = line(3, 'status', '')
  const result = line(4, 'result')
  assert.deepEqual(groupLines([tool, user, status, result]), [
    { kind: 'call', call: tool, result },
    { kind: 'line', line: user },
    { kind: 'line', line: status },
  ])
})

test('different tools and repeated calls receive their own results', () => {
  const calls = [line(1), line(2, 'tool', 'get_positions'), line(3)]
  const results = [line(4, 'result', 'get_positions'), line(5, 'result'), line(6, 'result')]
  const blocks = groupLines([...calls, ...results])
  assert.deepEqual(blocks.map((block) => [block.call.id, block.result.id]), [[1, 5], [2, 4], [3, 6]])
})

test('an adjacent result with another tool name is not attached to the wrong card', () => {
  const blocks = groupLines([line(1), line(2, 'result', 'get_positions')])
  assert.equal(blocks.length, 2)
  assert.equal(blocks[0].result, undefined)
  assert.equal(blocks[1].call.role, 'result')
})

test('a result loaded before its call remains visible and rejoins it after reconciliation', () => {
  const result = line(2, 'result')
  assert.deepEqual(groupLines([result]), [{ kind: 'call', call: result }])
  const call = line(1)
  assert.deepEqual(groupLines(mergeChatLines([result], [call])), [{ kind: 'call', call, result }])
})

test('wait steps remain visible without consuming another tool result', () => {
  const wait = line(1, 'tool', 'wait')
  const call = line(2)
  const result = line(3, 'result')
  assert.deepEqual(groupLines([wait, call, result]), [
    { kind: 'call', call: wait },
    { kind: 'call', call, result },
  ])
})
