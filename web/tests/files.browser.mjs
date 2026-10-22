import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { test } from 'node:test'
const require = createRequire(import.meta.url)
const { chromium, expect } = require(process.env.DESK_PLAYWRIGHT_MODULE || '@playwright/test')

for (const width of [1440, 390]) test(`root files, Markdown, links and protected drafts at ${width}px`, async () => {
  const browser = await chromium.launch({ headless: true })
  try {
    const page = await browser.newPage({ viewport: { width, height: 950 } })
    const body = '# 策略和规则\n\n**先测试再改**\n\n| 项目 | 状态 |\n| --- | --- |\n| 回测 | 还没做 |\n\n```json\n{"verified":false}\n```\n\n[验证](%E9%AA%8C%E8%AF%81.md) · [复盘](复盘.md)\n\n[外部](https://example.com/guide.md)\n\n[不安全](javascript:alert(1))\n\n[越界](../outside.md)\n\n<script>window.markdownExecuted=true</script>'
    const entries = new Map(['INVEST.MD', '验证.md', '复盘.md'].map(path => [path, { path, content: path === 'INVEST.MD' ? body : '# ' + path.slice(0, -3) + '\n\n[回到策略](INVEST.MD)', directory: false, revision: 1, updatedAt: '2026-10-21T00:00:00Z' }]))
    const reads = [], writes = []
    await page.route('**/api/**', async route => {
      const url = new URL(route.request().url())
      if (url.pathname !== '/api/files') return route.fulfill({ json: /log|positions|orders|klines|decisions/.test(url.pathname) ? [] : {} })
      const path = url.searchParams.get('path') || ''
      if (route.request().method() === 'GET') {
        if (url.searchParams.get('read')) {
          reads.push(path)
          return entries.has(path) ? route.fulfill({ json: entries.get(path) }) : route.fulfill({ status: 404, json: { error: '文件不存在' } })
        }
        assert.equal(path, '')
        return route.fulfill({ json: [...entries.values()] })
      }
      const { name, arguments: args } = route.request().postDataJSON()
      writes.push({ name, ...args })
      assert.equal(name, 'write_file')
      assert.equal(args.path.includes('/'), false)
      const old = entries.get(args.path)
      if (old && old.revision !== args.revision) return route.fulfill({ status: 409, json: { error: '文件已存在或版本已变化，请重新读取后再保存' } })
      const next = { path: args.path, directory: false, content: args.content, revision: (old?.revision || 0) + 1, updatedAt: new Date().toISOString() }
      entries.set(args.path, next)
      return route.fulfill({ json: next })
    })
    await page.goto(process.env.DESK_TEST_URL || 'http://127.0.0.1:3210')
    await page.getByRole('button', { name: '文件', exact: true }).click()
    const rendered = page.getByRole('article', { name: 'Markdown 文档' })
    await expect(rendered.getByRole('heading', { name: '策略和规则' })).toBeVisible()
    await expect(page.getByRole('treeitem')).toHaveCount(3)
    await expect(page.getByRole('button', { name: '新建文件夹', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '流程首页', exact: true })).toHaveCount(0)
    await expect(rendered.getByRole('table')).toContainText('还没做')
    await expect(rendered.locator('pre code')).toContainText('"verified":false')
    assert.equal(await rendered.locator('script').count(), 0)
    await expect(rendered.getByRole('link', { name: '不安全', exact: true })).toHaveAttribute('href', '')
    await expect(rendered.getByRole('link', { name: '外部', exact: true })).toHaveAttribute('href', 'https://example.com/guide.md')
    const count = reads.length
    await rendered.getByRole('link', { name: '越界', exact: true }).click({ force: true })
    assert.equal(reads.length, count)
    await rendered.getByRole('link', { name: '验证', exact: true }).click()
    await expect(rendered.getByRole('heading', { name: '验证', exact: true })).toBeVisible()
    assert.equal(reads.at(-1), '验证.md')
    await rendered.getByRole('link', { name: '回到策略', exact: true }).click()
    await page.getByRole('button', { name: '编辑', exact: true }).click()
    const editor = page.getByLabel('Markdown 正文', { exact: true })
    await editor.fill(body + '\n\n我的补充')
    await expect(rendered).toContainText('我的补充')
    const beforeBlockedRead = reads.length
    await rendered.getByRole('link', { name: '复盘', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('请先保存或放弃')
    await page.getByRole('treeitem', { name: '验证.md', exact: true }).click()
    assert.equal(reads.length, beforeBlockedRead)
    await page.getByRole('button', { name: '保存文件', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('文件已保存')
    assert.equal(writes.at(-1).revision, 1)
    entries.get('INVEST.MD').revision = 3
    entries.get('INVEST.MD').content = '# 别人刚改的规则'
    await editor.fill('# 我的未保存草稿')
    await page.getByRole('button', { name: '保存文件', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('版本已变化')
    await expect(editor).toHaveValue('# 我的未保存草稿')
    page.on('dialog', dialog => dialog.accept())
    await page.getByRole('button', { name: '读取最新 / 放弃修改', exact: true }).click()
    await expect(rendered.getByRole('heading', { name: '别人刚改的规则' })).toBeVisible()
    await page.getByRole('button', { name: '收起编辑器', exact: true }).click()
    await page.getByRole('button', { name: '新建', exact: true }).click()
    await page.getByLabel('新建名称').fill('invest.md')
    await page.getByRole('button', { name: '新建 Markdown', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('只允许编辑已有的根目录 INVEST.MD')
    await page.getByLabel('新建名称').fill('目录/重复.md')
    await page.getByRole('button', { name: '新建 Markdown', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('不要填写文件夹路径')
    await page.getByLabel('新建名称').fill('临时说明')
    await page.getByRole('button', { name: '新建 Markdown', exact: true }).click()
    await expect(page.getByRole('treeitem', { name: '临时说明.md', exact: true })).toBeVisible()
    assert.equal(writes.at(-1).revision, 0)
    await page.getByRole('button', { name: '刷新文件列表', exact: true }).click()
    assert.equal(entries.size, 4)
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false)
    await page.screenshot({ path: `../engine/data/files-flat-test-${width}.png`, fullPage: true })
  } finally { await browser.close() }
})
