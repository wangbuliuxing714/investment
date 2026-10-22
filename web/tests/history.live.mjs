// Explicit opt-in smoke test against the downloaded local database. No orders.
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'
const require = createRequire(import.meta.url)
const { chromium, expect } = require(process.env.DESK_PLAYWRIGHT_MODULE || '@playwright/test')
const base = process.env.DESK_TEST_URL || 'http://127.0.0.1:3210'

test('downloaded spot and perpetual history renders and pages locally', { skip: process.env.DESK_HISTORY_LIVE !== '1' }, async () => {
  const browser = await chromium.launch({ headless: true })
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } })
    const errors = [], mutations = []
    page.on('pageerror', e => errors.push(e.message))
    const response = await page.request.get(`${base}/api/history`)
    assert.equal(response.status(), 200)
    const { datasets } = await response.json()
    for (const d of datasets) {
      assert.equal(d.status, 'ready', d.error)
      assert.equal(d.first, d.market === 'spot' ? 1502942400 : 1567965420)
      assert.equal(d.first, d.availableFirst)
      assert.equal(d.rows + d.missingMinutes, (d.last - d.first) / 60 + 1)
      const url = `${base}/api/history/klines?market=${d.market}&interval=1m&limit=1500`
      const recent = await (await page.request.get(url)).json()
      const previous = await (await page.request.get(`${url}&end=${recent.nextEnd}`)).json()
      assert.equal(recent.candles.length, 1500)
      assert.equal(previous.candles.length, 1500)
      assert(previous.candles.at(-1).time < recent.candles[0].time)
      assert.equal(recent.candles.at(-1).time, d.last)
    }
    await page.route('**/api/**', async route => {
      const req = route.request(), u = new URL(req.url())
      if (req.method() !== 'GET') mutations.push(u.pathname)
      if (u.pathname.startsWith('/api/history')) return route.continue()
      return route.fulfill({ json: /log|positions|orders|klines|decisions|symbols/.test(u.pathname) ? [] : {} })
    })
    await page.goto(base)
    await page.getByRole('button', { name: '历史行情', exact: true }).click()
    await expect(page.getByLabel('历史库覆盖状态')).toContainText('已完成同步')
    await expect(page.getByTestId('history-chart')).toContainText('已到本地历史起点', { timeout: 30000 })
    await page.locator('.history-page').evaluate(el => { el.scrollTop = 0 })
    await page.screenshot({ path: '../engine/data/history-live-spot.png', fullPage: true })
    await page.getByRole('button', { name: 'BTC USDT 永续', exact: true }).click()
    await expect(page.getByLabel('历史库覆盖状态')).toContainText('2019-09-08 17:57')
    await expect(page.getByTestId('history-chart')).toContainText('已到本地历史起点', { timeout: 30000 })
    await page.locator('.history-page').evaluate(el => { el.scrollTop = 0 })
    await page.screenshot({ path: '../engine/data/history-live-um.png', fullPage: true })
    assert.deepEqual(mutations, [])
    assert.deepEqual(errors, [])
  } finally { await browser.close() }
})
