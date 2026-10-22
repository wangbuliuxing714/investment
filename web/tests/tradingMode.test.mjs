import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'

const source = readFileSync(new URL('../src/tradingMode.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
})
const { tradingMode } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`)

test('reading production data with dryRun enabled is paper mode, not live trading', () => {
  const mode = tradingMode({ dryRun: true, testnet: false, keySource: 'freqtrade' })
  assert.equal(mode.id, 'paper')
  assert.equal(mode.label, '模拟（不发单）')
  assert.equal(mode.className, 'pill sim')
})

test('order blocking takes precedence over the selected exchange environment', () => {
  assert.equal(tradingMode({ dryRun: true, testnet: true }).id, 'paper')
})

test('Binance demo mode is distinct from paper mode', () => {
  assert.equal(tradingMode({ dryRun: false, testnet: true }).id, 'demo')
})

test('live mode requires production environment and no dry-run block', () => {
  assert.equal(tradingMode({ dryRun: false, testnet: false }).id, 'live')
})

test('unloaded settings must not display an assumed trading mode', () => {
  assert.equal(tradingMode(null).id, 'loading')
})
