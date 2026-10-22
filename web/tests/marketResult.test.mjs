import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/marketResult.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } })
const { parseMarket } = await import('data:text/javascript;base64,' + Buffer.from(outputText).toString('base64'))

test('market parser preserves range, unavailable indicators, second timestamps and final candle', () => {
  const blocks = parseMarket([
    'symbol=BTCUSDT interval=1s requested=2 count=2 nextEnd=1790640000',
    'K线开盘时间范围 UTC：2026-09-29 00:00:00 → 2026-09-29 00:00:01；北京时间 UTC+08:00：08:00:00 → 08:00:01',
    'BTCUSDT 1s 本页末根收盘价 0.00001235 EMA20 N/A（至少20根） MACD(12,26,9) N/A（至少34根） 信号 N/A 柱 N/A RSI7 N/A',
    '指标仅基于本页数据；N/A 不是零值。',
    'OpenTime(UTC) Open High Low Close Volume',
    '2026-09-29 00:00:00 0.00001234 0.00001236 0.00001231 0.00001234 0.0000123',
    '2026-09-29 00:00:01 0.00001234 0.00001236 0.00001231 0.00001235 0.0000123  <- 本页最后一根',
  ].join('\n'))
  assert.equal(blocks.length, 1)
  assert.equal(blocks[0].rows.length, 2)
  assert.equal(blocks[0].rows[1][0], '2026-09-29 00:00:01')
  assert.equal(blocks[0].rows[1][4], '0.00001235')
  assert.equal(blocks[0].rows[1][6], '1')
  assert.equal(blocks[0].stats[2].value, 'N/A（至少34根）')
  assert.ok(blocks[0].notes.some(note => note.includes('UTC+08:00')))
  assert.ok(blocks[0].notes.some(note => note.includes('nextEnd=')))
})

test('legacy market results retain current markers and multiple symbols', () => {
  const blocks = parseMarket(['BTCUSDT', 'ETHUSDT'].map(symbol => [
    symbol + ' 15m 最新价 100 EMA20 99 MACD 1 信号 1 柱 0 RSI7 50',
    'Time(UTC) Open High Low Close Volume',
    '2026-09-29 00:00 99 101 98 100 10  <- current',
  ].join('\n')).join('\n\n'))
  assert.deepEqual(blocks.map(block => block.symbol), ['BTCUSDT', 'ETHUSDT'])
  assert.deepEqual(blocks.map(block => block.rows.length), [1, 1])
  assert.ok(blocks.every(block => block.rows[0][6] === '1'))
})

test('a later symbol error remains visible beside successful market data', () => {
  const blocks = parseMarket('BTCUSDT 15m 最新价 100 EMA20 99 MACD 1 信号 1 柱 0 RSI7 50\n2026-09-29 00:00 99 101 98 100 10\nETHUSDT 行情读取失败：test error')
  assert.ok(blocks[0].notes.some(note => note.includes('test error')))
})
