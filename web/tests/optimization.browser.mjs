import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'
const require = createRequire(import.meta.url)
const { chromium, expect } = require(process.env.DESK_PLAYWRIGHT_MODULE || '@playwright/test')

for (const width of [1440, 390]) test(`optimization switch isolates conversations and restores after refresh at ${width}px`, async () => {
  const browser = await chromium.launch({ headless: true })
  try {
    const page = await browser.newPage({ viewport: { width, height: 950 } })
    const state = { mode: 'trading', conversationId: 'trading', running: true, busy: false }
    const lines = { trading: [{ id: 1, conversationId: 'trading', role: 'agent', text: '模拟盘独有记录', at: new Date().toISOString() }], optimization: [] }
    const writes = [], asks = [], starts = []
    let socket, rejectSwitch = false
    await page.routeWebSocket('**/api/ws', ws => { socket = ws })
    await page.route('**/api/**', async route => {
      const req = route.request(), url = new URL(req.url()), path = url.pathname
      let json = []
      if (path === '/api/trader') json = state
      else if (path === '/api/agent/mode') {
        const body = req.postDataJSON(); writes.push(body)
        if (rejectSwitch) return route.fulfill({ status: 409, json: { error: '切换失败，请重试' } })
        assert.equal(body.conversationId, state.conversationId)
        state.mode = body.mode; state.conversationId = body.mode; state.running = false
        json = state
      } else if (path === '/api/agent/log') json = lines[url.searchParams.get('conversationId') || state.conversationId]
      else if (path === '/api/agent/ask') { const body = req.postDataJSON(); asks.push(body); lines[state.conversationId].push({ id: 20 + asks.length, conversationId: state.conversationId, role: 'user', text: body.text, at: new Date().toISOString() }); json = { ok: true } }
      else if (path === '/api/trader/start') { starts.push(req.postDataJSON()); state.running = true; json = state }
      else if (path === '/api/trader/stop') { state.running = false; json = state }
      else if (path === '/api/settings') json = { hasBinanceKey: false, modelName: 'Test', testnet: true }
      else if (path === '/api/strategy') json = { coins: ['BTCUSDT'], name: 'Test' }
      return route.fulfill({ json })
    })
    const errors = []; page.on('pageerror', e => errors.push(e.message))
    await page.goto(process.env.DESK_TEST_URL || 'http://127.0.0.1:3210')
    const rail = page.locator('.rail'), toggle = page.getByRole('switch', { name: '策略优化模式' })
    await expect(toggle).toBeEnabled()
    await expect(rail).toContainText('模拟盘独有记录')
    await toggle.click()
    await expect(toggle).toBeChecked()
    await expect(rail).not.toContainText('模拟盘独有记录')
    await expect(rail).toContainText('这是独立的策略优化对话')
    assert.equal(starts.length, 0)
    socket.send(JSON.stringify({ type: 'chat', id: 99, conversationId: 'trading', phase: 'agent', text: '迟到的模拟盘消息' }))
    await expect(rail).not.toContainText('迟到的模拟盘消息')
    await rail.locator('textarea').fill('检查BTC历史并记录修正原因')
    await rail.getByRole('button', { name: '发送', exact: true }).click()
    await expect.poll(() => asks.length).toBe(1)
    assert.equal(asks[0].conversationId, 'optimization')
    await page.reload()
    await expect(toggle).toBeChecked()
    await expect(rail).toContainText('检查BTC历史并记录修正原因')
    await expect(rail).not.toContainText('模拟盘独有记录')
    await page.screenshot({ path: `../engine/data/optimization-${width}.png`, fullPage: true })
    await toggle.click()
    await expect(rail).toContainText('模拟盘独有记录')
    await expect(rail).not.toContainText('检查BTC历史并记录修正原因')
    assert.equal(state.running, false)
    assert.equal(starts.length, 0)
    rejectSwitch = true
    await toggle.click()
    await expect(toggle).not.toBeChecked()
    await expect(page.getByText('切换失败，请重试')).toBeVisible()
    await expect(rail).toContainText('模拟盘独有记录')
    assert.deepEqual(errors, [])
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false)
  } finally { await browser.close() }
})
