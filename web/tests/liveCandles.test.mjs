import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/liveCandles.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } })
const { mergeCandles, validCandle } = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'))
const candle = (time, close, extra = {}) => ({ time, open: 100, high: 120, low: 80, close, volume: 1, ...extra })

test('early websocket candle survives initial history and out-of-order events', () => {
  const live = candle(100, 110, { source: 'websocket', updatedAt: 10000, receivedAt: 10000 })
  assert.equal(mergeCandles([candle(40, 100), candle(100, 100)], [live]).at(-1).close, 110)
  assert.equal(mergeCandles([live], [candle(100, 90, { source: 'websocket', updatedAt: 9000 })]).at(-1).close, 110)
})
test('delayed REST cannot overwrite live tick but resumes after stream is stale', () => {
  const live = candle(100, 110, { source: 'websocket', updatedAt: 10000, receivedAt: 10000 })
  assert.equal(mergeCandles([live], [candle(100, 90)], 9000).at(-1).close, 110)
  assert.equal(mergeCandles([live], [candle(100, 105)], 16000).at(-1).close, 105)
  assert.equal(mergeCandles([live], [candle(160, 112)]).length, 2)
})
test('invalid ticks do not erase valid history', () => {
  assert.equal(validCandle(candle(100, NaN)), false)
  assert.equal(mergeCandles([candle(100, 101)], [candle(100, NaN)])[0].close, 101)
})
