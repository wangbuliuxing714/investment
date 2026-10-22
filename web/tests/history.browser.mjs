import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'
const require = createRequire(import.meta.url)
const { chromium, expect } = require(process.env.DESK_PLAYWRIGHT_MODULE || '@playwright/test')

for (const width of [1440, 390]) test(`local history navigation without trading actions at ${width}px`, async () => {
  const browser = await chromium.launch({ headless: true })
  try {
    const page = await browser.newPage({ viewport: { width, height: 1000 } })
    const requests = [], mutations = []
    const datasets = ['spot', 'um'].map(market => ({ market, symbol: 'BTCUSDT', source: 'Binance', status: 'ready', rows: 4000000, first: 1502942400, last: 1792303140, availableFirst: 1502942400, syncedThrough: 1792303200, archivesDone: 100, archivesTotal: 100, gapCount: 1, missingMinutes: 2, error: '' }))
    await page.route('**/api/**', async route => {
      const req = route.request(), u = new URL(req.url())
      if (req.method() !== 'GET') mutations.push(u.pathname)
      if (u.pathname === '/api/history') return route.fulfill({ json: { datasets, gaps: { spot: [{ from: 1502943000, to: 1502943060, minutes: 2 }], um: [] } } })
      if (u.pathname === '/api/history/klines') {
        requests.push(Object.fromEntries(u.searchParams))
        const start = Number(u.searchParams.get('end')) || 1792303200
        return route.fulfill({ json: { candles: Array.from({ length: 100 }, (_, i) => ({ time: start - (100 - i) * 604800, open: 100 + i, high: 110 + i, low: 90 + i, close: 105 + i, volume: 20 })), dataset: datasets[0], hasMore: false, nextEnd: 0 } })
      }
      return route.fulfill({ json: /log|positions|orders|klines|decisions|symbols/.test(u.pathname) ? [] : {} })
    })
    const errors = []
    page.on('pageerror', e => errors.push(e.message))
    await page.goto(process.env.DESK_TEST_URL || 'http://127.0.0.1:3210')
    await page.getByRole('button', { name: '历史行情', exact: true }).click()
    await expect(page.getByRole('heading', { name: '历史行情' })).toBeVisible()
    if (width < 600) assert(await page.locator('.sidebar nav').evaluate(el => el.clientWidth <= innerWidth))
    await expect(page.getByLabel('历史库覆盖状态')).toContainText('4,000,000')
    await expect(page.locator('[data-testid="history-chart"] canvas').first()).toBeVisible()
    await page.getByRole('button', { name: 'BTC USDT 永续', exact: true }).click()
    await expect.poll(() => requests.at(-1)?.market).toBe('um')
    await page.getByLabel('历史K线周期').selectOption('1m')
    await expect.poll(() => requests.at(-1)?.interval).toBe('1m')
    await page.getByLabel('历史日期').fill('2020-03-12')
    await page.getByRole('button', { name: '查看', exact: true }).click()
    await expect.poll(() => requests.at(-1)?.end).toBe(String(Date.parse('2020-03-13T00:00:00Z') / 1000))
    await page.getByRole('button', { name: '最早行情', exact: true }).click()
    await expect.poll(() => requests.at(-1)?.end).toBe(String(1502942400 + 160 * 60))
    await page.getByRole('button', { name: '全部历史总览', exact: true }).click()
    await expect.poll(() => requests.at(-1)?.interval).toBe('1w')
    await page.getByText('查看数据缺口（1 处）').click()
    await expect(page.locator('details').getByRole('table')).toBeVisible()
    assert.deepEqual(mutations, [])
    assert.deepEqual(errors, [])
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false)
    await page.screenshot({ path: `../engine/data/history-${width}.png`, fullPage: true })
  } finally { await browser.close() }
})
