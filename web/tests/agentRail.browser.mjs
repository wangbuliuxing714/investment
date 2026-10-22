import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { after, before, test } from 'node:test'

const require = createRequire(import.meta.url)
const { chromium, expect } = require(process.env.DESK_PLAYWRIGHT_MODULE || '@playwright/test')
const baseURL = process.env.DESK_TEST_URL || 'http://127.0.0.1:3210'

for (const width of [1440, 390]) test(`research tool cards summarize history, paginate candles and format file results at ${width}px`, async t => {
  const candles = Array.from({ length: 205 }, (_, i) => ({ time: 1790640000 + i * 86400, open: 100 + i, high: 120 + i, low: 90 + i, close: 110 + i, volume: 12345.12345678, closed: i !== 204 }))
  const dataset = { market: 'um', symbol: 'BTCUSDT', status: 'ready', rows: 3740000, first: 1567965420, last: 1790640000, syncedThrough: 1790640000, gapCount: 1, missingMinutes: 1, source: 'Binance official archives' }
  const history = JSON.stringify({ candles, dataset, hasMore: true, nextEnd: candles[0].time })
  const entries = [
    line(1, 'tool', 'get_history', '本地历史行情 {"market":"um","symbol":"BTCUSDT","interval":"1d","limit":205}'), line(2, 'result', 'get_history', history),
    line(3, 'tool', 'list_files', '列出文件 {"path":"优化"}'), line(4, 'result', 'list_files', JSON.stringify([{ path: '优化/INVEST.MD', directory: false, revision: 2 }, { path: '优化/版本', directory: true, revision: 1 }])),
    line(5, 'tool', 'read_file', '读取 Markdown {"path":"优化/策略.md"}'), line(6, 'result', 'read_file', JSON.stringify({ path: '优化/策略.md', content: '# 策略版本 v1\n\n修正原因：核对历史缺口后重新验证。\n'.repeat(40), revision: 3, updatedAt: '2026-10-19T07:00:00Z' })),
    line(7, 'tool', 'read_file', '读取 Markdown {"path":"优化/不存在.md"}'), line(8, 'result', 'read_file', JSON.stringify({ error: '文件不存在，请核对路径' })),
    line(9, 'tool', 'get_history', '本地历史行情 {"market":"um","symbol":"ETHUSDT","interval":"1d"}'), line(10, 'result', 'get_history', '历史库支持 BTCUSDT 现货/永续'),
    line(11, 'tool', 'get_history', '本地历史行情 {"market":"spot","symbol":"BTCUSDT","interval":"1m"}'),
  ]
  const { page } = await setup(t, entries, { viewport: { width, height: 1000 } })
  const card = page.locator('.call[data-tool="get_history"]').first()
  await expect(card.locator('.call-preview')).toContainText('BTCUSDT · USDT 永续 · 1d · 205 根')
  await expect(card.locator('.tool-state')).toHaveText('已返回')
  await card.locator('.call-head').click()
  await expect(card.locator('.tool-metrics')).toContainText('3,740,000')
  await expect(card.locator('.history-tool-result > .tool-fields')).toContainText('1 处缺口 / 1 分钟')
  await expect(card.locator('.tool-warning')).toContainText('1 根未收盘或分钟样本不完整')
  await expect(card.locator('.tool-history-table')).not.toBeVisible()
  await card.locator('.tool-candles summary').click()
  await expect(card.locator('tbody tr')).toHaveCount(20)
  await expect(card.locator('tbody tr').first()).toContainText('未收盘 / 不完整')
  await expect(card.locator('.tool-raw pre')).toHaveCount(0)
  await card.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(card.locator('.tool-pagination')).toContainText('21–40 / 205 根')
  await expect(card.locator('tbody tr').first()).toContainText('284')
  await card.getByRole('button', { name: '上一页', exact: true }).click()
  await card.getByText('调用参数', { exact: true }).click()
  await expect(card.locator('.tool-parameters')).toContainText('请求根数205')
  await card.getByText('原始数据', { exact: true }).click()
  await expect(card.locator('.tool-raw pre')).toContainText('"candles"')
  assert.ok(await card.locator('.tool-raw pre').evaluate(el => el.clientHeight <= 240))
  await card.getByText('原始数据', { exact: true }).click()
  await expect(card.locator('.tool-raw pre')).toHaveCount(0)
  await card.getByText('调用参数', { exact: true }).click()
  assert.ok(await card.evaluate(el => el.scrollWidth <= el.clientWidth + 1), 'history card fits rail')
  assert.ok(await card.locator('.tool-history-table').evaluate(el => el.clientHeight <= 286 && el.scrollWidth > el.clientWidth), 'large table scrolls inside card')
  await card.locator('.tool-candles summary').click()
  await card.locator('.call-head').scrollIntoViewIfNeeded()
  await page.screenshot({ path: `../engine/data/tool-history-${width}.png`, fullPage: true })
  const files = page.locator('.call[data-tool="list_files"]')
  await expect(files.locator('.call-preview')).toHaveText('优化 · 2 项')
  await files.locator('.call-head').click()
  await expect(files.locator('.tool-file-list li')).toHaveCount(2)
  await expect(files.locator('.tool-file-list')).toContainText('优化/INVEST.MD')
  const document = page.locator('.call[data-tool="read_file"]').first()
  await document.locator('.call-head').click()
  await expect(document.locator('.tool-document')).toContainText('修正原因')
  assert.ok(await document.locator('.tool-document').evaluate(el => el.clientHeight <= 340 && el.scrollHeight > el.clientHeight), 'document has its own scroll area')
  const failed = page.locator('.call[data-tool="read_file"]').last()
  await expect(failed.locator('.tool-state')).toHaveText('未成功')
  await failed.locator('.call-head').click()
  await expect(failed.getByRole('alert')).toContainText('文件不存在')
  await expect(page.locator('.call[data-tool="get_history"]').nth(1).locator('.tool-state')).toHaveText('未成功')
  await expect(page.locator('.call[data-tool="get_history"]').last().locator('.tool-state')).toHaveText('等待结果')
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false)
})

let browser
before(async () => { browser = await chromium.launch({ headless: true }) })
after(async () => { await browser?.close() })

function line(id, role, name = '', text = `Message ${id}`) {
  return { id, role, name, text, at: '2026-09-29T00:00:00Z' }
}

for (const width of [1440, 390]) test(`AI replies preserve and render Markdown at ${width}px`, async t => {
  const markdown = [
    '## 策略复核', '', '**重点结论**：优先复用文档。', '',
    '1. 查询历史', '2. 记录修正原因', '', '> 尚未执行回测。', '',
    '| 指标 | 结果 |', '| --- | --- |', '| 数据完整性 | 已核对 |', '',
    '```json', '{"verified":true,"note":"' + 'x'.repeat(400) + '"}', '```', '',
    'English explanation remains visible.', '',
    '[安全链接](javascript:alert(1))', '<script>window.markdownExecuted=true</script>',
  ].join('\n')
  const { page } = await setup(t, [line(1, 'agent', '', markdown)], { viewport: { width, height: 950 } })
  const reply = page.locator('.bubble:not(.mine)').first()
  await expect(reply.getByRole('heading', { name: '策略复核' })).toBeVisible()
  await expect(reply.locator('strong')).toHaveText('重点结论')
  await expect(reply.locator('ol li')).toHaveText(['查询历史', '记录修正原因'])
  await expect(reply.locator('blockquote')).toContainText('尚未执行回测')
  await expect(reply.getByRole('table')).toContainText('已核对')
  await expect(reply.locator('pre code')).toContainText('"verified":true')
  await expect(reply).toContainText('English explanation remains visible.')
  assert.equal(await reply.locator('script').count(), 0)
  assert.equal(await reply.locator('a').getAttribute('href'), '')
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1), false)
  const code = await reply.locator('pre').evaluate(el => ({ width: el.clientWidth, scroll: el.scrollWidth }))
  assert.ok(code.scroll > code.width, 'long code scrolls inside reply')
  await reply.scrollIntoViewIfNeeded()
  await page.screenshot({ path: `../engine/data/ai-markdown-${width}.png`, fullPage: true })
  await page.reload()
  await expect(page.locator('.bubble:not(.mine)').first().getByRole('heading', { name: '策略复核' })).toBeVisible()
})

function conversation(count) {
  return Array.from({ length: count }, (_, i) => [
    line(i * 3 + 1, 'agent', '', 'Market analysis with enough text to fill the transcript. '.repeat(8)),
    line(i * 3 + 2, 'tool', 'get_account', 'get_account {}'),
    line(i * 3 + 3, 'result', 'get_account', `Account result ${i + 1}`),
  ]).flat()
}

function toolsFixture() {
  return { config: { instructions: "saved independent tools" }, defaults: { instructions: "default independent tools" }, variables: ["coins"], preview: "saved independent tools", definitions: [{"name":"get_account","label":"查看账户","description":"读取净值、钱包余额、可用资金、未实现盈亏和已用保证金。","effect":"只读","scope":"持续运行、面板对话","parameters":[],"example":"{\n  \"name\": \"get_account\",\n  \"arguments\": {}\n}"},{"name":"get_positions","label":"查看持仓","description":"读取当前持仓的方向、数量、开仓价、标记价、盈亏和杠杆。","effect":"只读","scope":"持续运行、面板对话","parameters":[],"example":"{\n  \"name\": \"get_positions\",\n  \"arguments\": {}\n}"},{"name":"get_market","label":"查看行情","description":"代理自行选择合约、周期和历史范围；允许查看交易名单之外的合约。","effect":"只读","scope":"持续运行、面板对话","parameters":[{"name":"symbol","type":"string","required":false,"description":"合约名，如 BTCUSDT；省略时查看策略币种。"},{"name":"interval","type":"string","required":false,"description":"K 线周期，默认 15m；实际支持范围由行情接口决定。"},{"name":"limit","type":"integer","required":false,"description":"本次读取根数，默认 200，范围 1–1500；需要更多历史时继续分页。"},{"name":"end","type":"integer","required":false,"description":"历史截止时间，Unix 秒或毫秒；省略或 0 表示最新，使用返回的 nextEnd 继续读取更早历史。"}],"example":"{\n  \"name\": \"get_market\",\n  \"arguments\": {\n    \"symbol\": \"BTCUSDT\",\n    \"interval\": \"1h\",\n    \"limit\": 200,\n    \"end\": 0\n  }\n}"},{"name":"submit_decisions","label":"提交交易决定","description":"提交 open_long、open_short、close_long、close_short 或 hold。交易币种、杠杆、仓位与保证金继续由程序校验。","effect":"可能下单","scope":"持续运行、面板对话；取决于当前账户模式和风控","parameters":[{"name":"decisions","type":"array","required":true,"description":"决定列表，每项包含 symbol 和 action；可填写 leverage、position_size_usd（名义价值，单位 USDT）、stop_loss、take_profit、confidence、reasoning。"}],"example":"{\n  \"name\": \"submit_decisions\",\n  \"arguments\": {\n    \"decisions\": [\n      {\n        \"symbol\": \"BTCUSDT\",\n        \"action\": \"hold\",\n        \"reasoning\": \"等待更多数据\"\n      }\n    ]\n  }\n}"},{"name":"wait","label":"等待","description":"安排持续运行的下一次查看时间；有新用户消息时可提前唤醒。","effect":"调度","scope":"仅持续运行；面板对话不会执行等待","parameters":[{"name":"seconds","type":"integer","required":false,"description":"等待秒数，程序限制为 15–900 秒；省略或低于下限时按 15 秒处理。"},{"name":"reason","type":"string","required":false,"description":"等待原因。"}],"example":"{\n  \"name\": \"wait\",\n  \"arguments\": {\n    \"seconds\": 120,\n    \"reason\": \"等待下一次行情变化\"\n  }\n}"}] }
}

function parametersFixture() {
  return [
    { key: 'coins', label: '交易币种', value: 'BTCUSDT, ETHUSDT', unit: '' },
    { key: 'maxPositions', label: '最多持仓', value: '2', unit: '个' },
  ]
}
function renderFixture(template) {
  return parametersFixture().reduce((result, p) => result.replaceAll('{{' + p.key + '}}', p.value), template)
}
function promptFixture() {
  const entries = [['system', '系统提示词'], ['trading', '交易模式指令'], ['optimization', '策略优化模式指令'], ['chat', '面板对话指令'], ['resume', '等待结束指令']]
  const prompts = Object.fromEntries(entries.map(([key]) => [key, 'saved ' + key]))
  prompts.system = 'saved system\n交易范围 {{coins}}，最多 {{maxPositions}} 仓。'.replaceAll('\\n', '\n')
  const scenarios = { trading: 'Trading switch off only', optimization: 'Optimization switch on only', system: '每次模型调用：持续运行与面板对话均使用。', chat: '持续运行未开启时，用户在面板发送消息。', start: '每次启动持续运行时，仅追加一次。', noTool: '持续运行未解析出任何工具调用时提醒。', resume: '等待结束且没有待处理的新用户消息时使用。' }
  return { prompts, invest: { path: 'INVEST.MD', directory: false, content: '# INVEST.MD\nexisting policy {{coins}}', revision: 1, updatedAt: '2026-10-19T00:00:00Z' }, parameters: parametersFixture(), presets: [{ id: 'sepa-vcp-experimental', name: 'SEPA / VCP 实验策略（冠军方法启发版）', description: 'draft preset', prompt: 'PRESET {{coins}}' }], definitions: entries.map(([key, label]) => ({ key, label, description: label, category: key === 'system' ? 'system' : key === 'chat' ? 'chat' : 'automation', role: key === 'system' ? 'system' : key === 'chat' ? 'request' : 'event', scenario: scenarios[key], placement: key === 'system' ? '独立 system 配置。' : key === 'chat' ? '请求正文。' : '写入运行历史。', effectiveWhen: key === 'start' ? '下一次启动持续运行。' : '下一轮读取配置，按场景使用。', default: 'default ' + key, required: true })), variables: ['coins', 'maxPositions'], preview: { system: renderFixture(prompts.system), instructions: '', chat: prompts.chat, trading: prompts.trading, optimization: prompts.optimization } }
}

for (const width of [1440, 390]) {
  test('unified system, scenario metadata and parameter buttons fit at ' + width + 'px', async (t) => {
    const { page, state } = await setup(t, [], { viewport: { width, height: 900 } })
    await page.getByRole('button', { name: '模型', exact: true }).click()
    const panel = page.locator('.prompt-settings')
    await expect(panel.locator('[data-prompt-key]')).toHaveCount(6)
    await expect(panel.locator('[data-prompt-key="strategy"], [data-prompt-key="history"], [data-prompt-key="wait"]')).toHaveCount(0)
    for (const field of state.promptConfig.definitions) {
      await panel.locator('[data-prompt-key="' + field.key + '"]').click()
      await panel.locator('.usage-details').evaluate(el => { el.open = true })
      await expect(panel.locator('.prompt-metadata')).toContainText(field.scenario)
      await expect(panel.locator('.prompt-metadata')).toContainText(field.effectiveWhen)
    }
    await panel.locator('[data-prompt-key="system"]').click()
    const token = panel.locator('.template-surface').getByRole('button', { name: '交易币种，BTCUSDT, ETHUSDT', exact: true })
    await expect(token).toBeVisible()
    await expect(panel.locator('.template-surface')).not.toContainText('{{coins}}')
    assert.equal(await token.evaluate(el => getComputedStyle(el).color), 'rgb(35, 104, 190)')
    await token.click()
    await expect(panel.locator('.parameter-inspector')).toContainText('BTCUSDT, ETHUSDT')
    await panel.getByRole('button', { name: '关闭参数说明' }).click()
    await panel.getByRole('button', { name: '编辑原文', exact: true }).click()
    await expect(page.getByLabel('系统提示词', { exact: true })).toHaveValue(state.promptConfig.prompts.system)
    await page.getByLabel('系统提示词', { exact: true }).fill('unsaved system draft')
    await panel.locator('.prompt-saved-preview summary').click()
    await expect(panel.locator('[data-preview="system"]')).toHaveText(renderFixture(state.promptConfig.prompts.system))
    assert.equal(state.promptWrites.length, 0, 'viewing and editing drafts never saves or starts trading')
    assert.ok(await panel.evaluate(el => el.scrollWidth <= el.clientWidth + 1), 'prompt editor fits viewport')
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'page fits viewport')
    if (width === 1440) assert.ok((await page.locator('.models-layout').boundingBox()).width > 1100, 'model workspace fills desktop')
  })
}

test('parameter insertion preserves tokens, cursor and saved rendering across reload', async (t) => {
  const { page, state } = await setup(t, [])
  await page.getByRole('button', { name: '模型', exact: true }).click()
  await page.getByRole('button', { name: '编辑原文', exact: true }).click()
  const input = page.getByLabel('系统提示词', { exact: true })
  await input.fill('before after')
  await input.evaluate(el => { el.focus(); el.setSelectionRange(7, 7) })
  await page.locator('.parameter-library summary').click()
  await page.locator('.parameter-buttons').getByRole('button', { name: '交易币种', exact: false }).click()
  await expect(input).toHaveValue('before {{coins}}after')
  await page.getByRole('button', { name: '保存全部提示词', exact: true }).click()
  await expect(page.locator('.prompt-settings .ok')).toContainText('提示词已保存')
  assert.deepEqual(Object.keys(state.promptWrites[0].prompts).sort(), ['chat', 'optimization', 'resume', 'system', 'trading'])
  assert.equal(state.promptConfig.preview.system, 'before BTCUSDT, ETHUSDTafter')
  await page.getByRole('button', { name: 'Model B', exact: true }).click()
  await expect(input).toHaveValue('before {{coins}}after')
  await page.reload()
  await page.getByRole('button', { name: '模型', exact: true }).click()
  await expect(page.locator('.template-surface')).toContainText('BTCUSDT, ETHUSDT')
  await page.getByRole('button', { name: '编辑原文', exact: true }).click()
  await expect(page.getByLabel('系统提示词', { exact: true })).toHaveValue('before {{coins}}after')
})

test('mode instructions edit and save independently with Markdown previews', async (t) => {
  const { page, state } = await setup(t, [])
  await page.getByRole('button', { name: '模型', exact: true }).click()
  const originalSystem = state.promptConfig.prompts.system
  const originalOptimization = state.promptConfig.prompts.optimization
  await page.locator('[data-prompt-key="trading"]').click()
  await page.getByRole('button', { name: '编辑原文', exact: true }).click()
  await page.getByLabel('交易模式指令', { exact: true }).fill('## 交易流程\n\n**先处理风险**')
  await page.getByRole('button', { name: '保存全部提示词', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('提示词已保存')
  assert.equal(state.promptConfig.prompts.system, originalSystem)
  assert.equal(state.promptConfig.prompts.optimization, originalOptimization)
  assert.equal(state.promptConfig.prompts.trading, '## 交易流程\n\n**先处理风险**')
  await page.locator('[data-prompt-key="optimization"]').click()
  await page.getByRole('button', { name: '编辑原文', exact: true }).click()
  await page.getByLabel('策略优化模式指令', { exact: true }).fill('## 优化流程\n\n**先验证再修改**')
  await page.getByRole('button', { name: '保存全部提示词', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('提示词已保存')
  await page.locator('.prompt-saved-preview summary').click()
  await expect(page.locator('[data-preview="trading"] h2')).toHaveText('交易流程')
  await expect(page.locator('[data-preview="optimization"] strong')).toHaveText('先验证再修改')
  assert.equal(state.fileWrites.length, 0, 'mode instructions are saved in model settings')
})

test('default prompt stays a draft and failed saves preserve edits', async (t) => {
  const { page, state } = await setup(t, [])
  const original = state.promptConfig.prompts.system
  await page.getByRole('button', { name: '模型', exact: true }).click()
  await page.getByRole('button', { name: '填入默认文本（待保存）', exact: true }).click()
  await expect(page.locator('.template-surface')).toHaveText('default system')
  assert.equal(state.promptWrites.length, 0)
  state.promptError = 'prompt validation failed'
  await page.getByRole('button', { name: '保存全部提示词', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('prompt validation failed')
  await expect(page.locator('.template-surface')).toHaveText('default system')
  assert.equal(state.promptConfig.prompts.system, original)
})

test('champion-inspired preset appends to INVEST.MD without replacing role or saved policy', async (t) => {
  const { page, state } = await setup(t, [])
  await page.getByRole('button', { name: '模型', exact: true }).click()
  await page.locator('.strategy-presets summary').click()
  await expect(page.getByText('SEPA / VCP 实验策略（冠军方法启发版）', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '追加到 INVEST.MD 草稿', exact: true }).click()
  await expect(page.locator('.template-surface')).toContainText('PRESET')
  assert.equal(state.promptWrites.length, 0)
  assert.equal(state.fileWrites.length, 0)
  await page.getByRole('button', { name: '保存 INVEST.MD', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('INVEST.MD 已保存')
  assert.equal(state.promptWrites.length, 0)
  assert.equal(state.fileWrites.length, 1)
  assert.ok(state.promptConfig.invest.content.includes('existing policy'))
  assert.ok(state.promptConfig.invest.content.includes('PRESET {{coins}}'))
  await page.getByRole('button', { name: '文件', exact: true }).click()
  await page.locator('.file-entry').filter({ hasText: 'INVEST.MD' }).click()
  await expect(page.getByRole('article', { name: 'Markdown 文档' })).toContainText('existing policy')
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  await expect(page.getByLabel('Markdown 正文')).toHaveValue(state.promptConfig.invest.content)
})

for (const width of [1440, 390]) test('INVEST.MD drafts retain revision conflicts across role saves at ' + width + 'px', async (t) => {
  const { page, state } = await setup(t, [], { viewport: { width, height: 950 } })
  await page.getByRole('button', { name: '模型', exact: true }).click()
  await page.locator('[data-prompt-key="invest"]').click()
  await page.getByRole('button', { name: '编辑原文', exact: true }).click()
  await page.getByLabel('INVEST.MD 正文', { exact: true }).fill('my pending policy')
  state.promptConfig.invest = { ...state.promptConfig.invest, revision: 2, content: 'AI updated policy' }
  await page.locator('[data-prompt-key="system"]').click()
  await page.getByRole('button', { name: '填入默认文本（待保存）', exact: true }).click()
  await page.getByRole('button', { name: '保存全部提示词', exact: true }).click()
  await expect(page.getByRole('status')).toContainText('提示词已保存')
  await page.locator('[data-prompt-key="invest"]').click()
  await page.getByRole('button', { name: '保存 INVEST.MD', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('文件版本已变化')
  await expect(page.locator('.template-surface')).toContainText('my pending policy')
  assert.equal(state.fileWrites[0].arguments.revision, 1, 'role save must not advance the file revision of an old draft')
  assert.equal(state.promptConfig.invest.content, 'AI updated policy')
  page.on('dialog', dialog => dialog.accept())
  await page.getByRole('button', { name: '读取最新 / 放弃草稿', exact: true }).click()
  await expect(page.locator('.template-surface')).toContainText('AI updated policy')
  await expect(page.getByRole('button', { name: '保存 INVEST.MD', exact: true })).toBeDisabled()
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'INVEST.MD editor fits viewport')
  await page.screenshot({ path: `../engine/data/invest-${width}.png`, fullPage: true })
})

for (const width of [1440, 390]) {
  test('Tools manager saves independently and survives reload at ' + width + 'px', async (t) => {
    const { page, state } = await setup(t, [], { viewport: { width, height: 900 } })
    await page.getByRole('button', { name: '模型', exact: true }).click()
    await expect(page.locator('[data-prompt-key="tools"]')).toHaveCount(0)
    await page.getByRole('button', { name: '打开 Tools 管理', exact: true }).click()
    const panel = page.locator('.tools-page')
    await expect(panel.getByRole('heading', { name: 'Tools 管理', exact: true })).toBeVisible()
    await expect(panel.locator('.template-surface')).toHaveText('saved independent tools')
    await expect(panel.locator('[data-tool-name]')).toHaveCount(5)
    const market = panel.locator('[data-tool-name="get_market"]')
    await market.locator('summary').click()
    await expect(market.locator('.tool-parameters')).toContainText('nextEnd')
    await expect(market.locator('.tool-parameters')).toContainText('1–1500')
    await expect(market.locator('.prompt-preview')).toContainText('"get_market"')
    assert.equal(state.toolWrites.length, 0)
    const originalPrompts = { ...state.promptConfig.prompts }
    await page.getByRole('button', { name: '编辑原文', exact: true }).click()
    await page.getByLabel('工具说明', { exact: true }).fill('updated independent tool instructions')
    await page.getByRole('button', { name: '保存工具说明', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('工具说明已独立保存')
    assert.deepEqual(state.toolWrites, [{ instructions: 'updated independent tool instructions' }])
    assert.deepEqual(state.promptConfig.prompts, originalPrompts)
    assert.equal(state.promptWrites.length, 0)
    assert.ok(await panel.evaluate(el => el.scrollWidth <= el.clientWidth + 1), 'Tools page fits mobile and desktop')
    await page.reload()
    await page.getByRole('button', { name: 'Tools 管理', exact: true }).click()
    await expect(page.locator('.template-surface')).toHaveText('updated independent tool instructions')
    await page.getByRole('button', { name: '模型', exact: true }).click()
    await page.getByRole('button', { name: '编辑原文', exact: true }).click()
    await page.getByLabel('系统提示词', { exact: true }).fill('updated system only')
    await page.getByRole('button', { name: '保存全部提示词', exact: true }).click()
    await expect(page.getByRole('status')).toContainText('提示词已保存')
    assert.equal(Object.hasOwn(state.promptWrites[0].prompts, 'tools'), false)
    assert.equal(state.toolsConfig.config.instructions, 'updated independent tool instructions')
  })
}

test('Tools default stays a draft and failed save keeps saved configuration', async (t) => {
  const { page, state } = await setup(t, [])
  await page.getByRole('button', { name: 'Tools 管理', exact: true }).click()
  await page.getByRole('button', { name: '填入默认工具说明（待保存）', exact: true }).click()
  await expect(page.locator('.template-surface')).toHaveText('default independent tools')
  assert.equal(state.toolWrites.length, 0)
  await page.locator('.tools-preview summary').click()
  await expect(page.locator('.tools-preview pre')).toHaveText('saved independent tools')
  state.toolsError = 'tools validation failed'
  await page.getByRole('button', { name: '保存工具说明', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('tools validation failed')
  await expect(page.locator('.template-surface')).toHaveText('default independent tools')
  assert.equal(state.toolsConfig.config.instructions, 'saved independent tools')
  assert.equal(state.promptWrites.length, 0)
})

for (const width of [1440, 390]) {
  test('all settings pages fill their viewport and reduced motion is respected at ' + width + 'px', async (t) => {
    const { page } = await setup(t, [], { viewport: { width, height: 900 } })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    for (const label of ['模型', 'Tools 管理', '策略', '交易所']) {
      await page.getByRole('button', { name: label, exact: true }).click()
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), label + ' fits viewport')
      const animation = await page.locator('main').evaluate(el => getComputedStyle(el).animationDuration)
      assert.ok(animation === '0s' || parseFloat(animation) <= 0.001, 'reduced motion avoids page animation')
    }
  })
}

test('charts default to enabled symbols but retain all-market access and free timeframe selection', async (t) => {
  const { page } = await setup(t, [])
  await expect(page.locator('.symbol-row > button')).toHaveText(['BTC', 'ETH'])
  await expect(page.getByRole('button', { name: 'SOL', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '全部行情', exact: true }).click()
  await expect(page.getByRole('button', { name: 'SOL', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'SOL', exact: true }).click()
  const request = page.waitForRequest((item) => item.url().includes('/api/klines?') && item.url().includes('symbol=SOLUSDT') && item.url().includes('interval=1w'))
  await page.getByRole('button', { name: '1周', exact: true }).click()
  await request
  await page.getByRole('button', { name: '策略', exact: true }).click()
  await expect(page.getByLabel('单笔名义价值上限（净值倍数）', { exact: true })).toBeVisible()
  await expect(page.getByText('全部提示词请到“模型”页面管理。', { exact: false })).toBeVisible()
  await expect(page.locator('textarea, select')).toHaveCount(0)
})

for (const width of [1440, 390]) {
  test('chart scope resets the selection without enabling trades at ' + width + 'px', async (t) => {
    const { page, state } = await setup(t, [], { viewport: { width, height: 900 } })
    const writes = []
    page.on('request', (request) => { if (request.method() !== 'GET') writes.push(request.url()) })
    await expect(page.locator('.symbol-row > button')).toHaveText(['BTC', 'ETH'])
    await expect(page.getByRole('button', { name: '策略币种', exact: true })).toHaveAttribute('aria-pressed', 'true')
    await page.getByRole('button', { name: '全部行情', exact: true }).click()
    await page.getByRole('searchbox', { name: '搜索行情合约' }).fill('sol')
    await expect(page.locator('.symbol-row > button')).toHaveText(['SOL'])
    await page.getByRole('button', { name: 'SOL', exact: true }).click()
    await expect(page.getByText('SOLUSDT：仅查看行情，未在策略中启用交易。', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '策略币种', exact: true }).click()
    await expect(page.locator('.symbol-row > button')).toHaveText(['BTC', 'ETH'])
    await expect(page.locator('.symbol-row > button.on')).toHaveText('BTC')
    await expect(page.getByRole('searchbox', { name: '搜索行情合约' })).toHaveCount(0)
    state.strategy.coins = ['ETHUSDT']
    await page.clock.fastForward(4100)
    await expect(page.locator('.symbol-row > button')).toHaveText(['ETH'])
    await expect(page.locator('.symbol-row > button.on')).toHaveText('ETH')
    await page.reload()
    await expect(page.locator('.symbol-row > button')).toHaveText(['ETH'])
    assert.deepEqual(writes, [], 'viewing charts must not mutate the strategy, start the agent or send orders')
  })
}

test('empty strategy never implies BTC and ETH are enabled', async (t) => {
  const { page } = await setup(t, [], { strategyCoins: [] })
  await expect(page.locator('.symbol-row > button')).toHaveCount(0)
  await expect(page.getByText('策略尚未启用币种，可在策略页面配置，或切换到全部行情查看。', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '全部行情', exact: true }).click()
  await expect(page.locator('.symbol-row > button')).toHaveText(['BTC', 'ETH', 'SOL'])
  await expect(page.getByText('BTCUSDT：仅查看行情，未在策略中启用交易。', { exact: true })).toBeVisible()
})

async function setup(t, lines, { viewport = { width: 1440, height: 900 }, failSettings = false, strategyCoins = ['BTCUSDT', 'ETHUSDT'], settings = { hasBinanceKey: false, testnet: true, modelName: 'Test model' }, trader = { running: false, busy: false, lastError: '' } } = {}) {
  const page = await browser.newPage({ viewport })
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  t.after(async () => {
    await page.close()
    assert.deepEqual(errors, [], 'no browser runtime errors')
  })
  const state = { lines, failLog: false, snapshot: null, socket: null, settings, trader, connectionWrites: [], connectionResponse: null, connectionError: null, testedConnections: [] }
  state.positions = []; state.orders = []; state.klines = []; state.protectionWrites = []; state.pnlRequests = []
  state.pnl = { asset: 'USDT', from: 1, to: 2, realized: 12, commission: -2, funding: -1, net: 9, updatedAt: Date.now() }
  state.strategy = { name: 'Test strategy', coins: strategyCoins, btcEthLeverage: 5, altLeverage: 3, maxPositions: 2, minConfidence: 75, positionValueRatio: 1, maxMarginUsage: 0.5, scanIntervalSec: 900 }
  state.toolsConfig = { ...toolsFixture(), parameters: parametersFixture() }
  state.toolWrites = []
  state.toolsError = ''
  state.promptConfig = promptFixture()
  state.promptWrites = []
  state.fileWrites = []
  state.promptError = ''
  state.models = [{ id: 'model-b', name: 'Model B', providerId: 'provider-b', upstreamModel: 'upstream-b' }]
  await page.clock.install()
  await page.routeWebSocket('**/api/ws', (socket) => { state.socket = socket })
  await page.routeWebSocket('**/api/klines/live?**', socket => { state.chartSocket = socket })
  // All application APIs are mocked; these tests never call trading endpoints.
  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    let json = []
    if (path === '/api/agent/log') {
      if (state.failLog) return route.fulfill({ status: 503, json: { error: 'temporary failure' } })
      json = state.snapshot ? await state.snapshot() : state.lines
    } else if (path === '/api/settings/binance/connection') {
      state.connectionWrites.push(route.request().postDataJSON())
      if (state.connectionError) return route.fulfill({ status: 409, json: { error: state.connectionError } })
      if (state.connectionResponse) state.settings = state.connectionResponse
      json = state.settings
    } else if (path === '/api/binance/test') {
      state.testedConnections.push(state.settings.binanceConnection)
      json = { testnet: state.settings.testnet, paper: state.settings.dryRun, equity: 10000, available: 10000, hedge: false }
    } else if (path === '/api/settings/tools') {
      if (route.request().method() === 'PUT') {
        state.toolWrites.push(route.request().postDataJSON())
        if (state.toolsError) return route.fulfill({ status: 400, json: { error: state.toolsError } })
        state.toolsConfig.config = { ...route.request().postDataJSON() }
        state.toolsConfig.preview = state.toolsConfig.config.instructions
      }
      json = state.toolsConfig
    } else if (path === '/api/settings/prompts') {
      if (route.request().method() === 'PUT') {
        state.promptWrites.push(route.request().postDataJSON())
        if (state.promptError) return route.fulfill({ status: 400, json: { error: state.promptError } })
        state.promptConfig.prompts = { ...route.request().postDataJSON().prompts }
        state.promptConfig.preview.system = renderFixture(state.promptConfig.prompts.system)
        state.promptConfig.preview.instructions = ''
        state.promptConfig.preview.chat = state.promptConfig.prompts.chat
        state.promptConfig.preview.trading = state.promptConfig.prompts.trading
        state.promptConfig.preview.optimization = state.promptConfig.prompts.optimization
      }
      json = state.promptConfig
    } else if (path === '/api/files') {
      if (route.request().method() === 'POST') {
        const body = route.request().postDataJSON()
        state.fileWrites.push(body)
        if (body.arguments.revision !== state.promptConfig.invest.revision) return route.fulfill({ status: 409, json: { error: '文件版本已变化' } })
        state.promptConfig.invest = { ...state.promptConfig.invest, content: body.arguments.content, revision: body.arguments.revision + 1 }
      }
      const read = new URL(route.request().url()).searchParams.get('read')
      json = route.request().method() === 'POST' || read ? state.promptConfig.invest : [state.promptConfig.invest]
    } else if (path === '/api/models') {
      json = state.models
    } else if (path === '/api/settings/model') {
      const selected = route.request().postDataJSON()
      state.settings = { ...state.settings, modelId: selected.id, modelName: selected.name }
      json = state.settings
    } else if (path === '/api/symbols') {
      json = ['BTCUSDT', 'ETHUSDT', 'SOLUSDT']
    } else if (path === '/api/settings') {
      if (failSettings) return route.fulfill({ status: 503, json: { error: 'settings unavailable' } })
      json = state.settings
    } else if (path === '/api/strategy') {
      json = state.strategy
    } else if (path === '/api/trader') {
      json = state.trader
    } else if (path === '/api/account') {
      json = { equity: 1000, wallet: 999, available: 900, unrealized: 1, marginUsed: 100, testnet: true }
    } else if (path === '/api/positions') {
      json = state.positions
    } else if (path === '/api/orders') {
      json = state.orders
    } else if (path === '/api/pnl') {
      state.pnlRequests.push(new URL(route.request().url()).searchParams.get('period'))
      if (state.pnlError) return route.fulfill({ status: 502, json: { error: state.pnlError } })
      json = state.pnl
    } else if (path === '/api/positions/protection') {
      const body = route.request().postDataJSON(); state.protectionWrites.push(body)
      json = state.protectionReply ? state.protectionReply(body) : { confirmed: false, stopLoss: 0, takeProfit: 0, message: '未完整生效' }
    } else if (path === '/api/klines') {
      json = state.klineSnapshot ? await state.klineSnapshot(new URL(route.request().url())) : state.klines
    } else if (path === '/api/klines/live') {
      return route.fulfill({ contentType: 'text/event-stream', body: ': test stream\n\n' })
    }
    await route.fulfill({ json })
  })
  await page.goto(baseURL)
  await expect(page.locator('.call.tool')).toHaveCount(lines.filter((item) => item.role === 'tool').length)
  return { page, state }
}

async function flushRender(page) {
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))))
}

test('market card shows date coverage, N/A and all candles newest first', async (t) => {
  const result = [
    'symbol=BTCUSDT interval=15m requested=2 count=2 nextEnd=1790640000',
    'K线开盘时间范围 UTC：2026-09-29 00:00:00 → 2026-09-29 00:15:00；北京时间 UTC+08:00：08:00:00 → 08:15:00',
    'BTCUSDT 15m 本页末根收盘价 0.00001235 EMA20 N/A（至少20根） MACD(12,26,9) N/A（至少34根） 信号 N/A 柱 N/A RSI7 N/A',
    'OpenTime(UTC) Open High Low Close Volume',
    '2026-09-29 00:00:00 0.00001234 0.00001236 0.00001231 0.00001234 1',
    '2026-09-29 00:15:00 0.00001234 0.00001236 0.00001231 0.00001235 2  <- 本页最后一根',
  ].join('\n')
  const { page } = await setup(t, [line(1, 'tool', 'get_market'), line(2, 'result', 'get_market', result)])
  await page.locator('.call.tool .call-head').click()
  const card = page.locator('.market-block')
  await expect(card.locator('h4')).toContainText('2 根')
  await expect(card).toContainText('北京时间 UTC+08:00')
  await expect(card.locator('.stat-grid')).toContainText('N/A（至少34根）')
  await expect(card.locator('tbody tr')).toHaveCount(2)
  await expect(card.locator('tbody tr').first()).toHaveClass('current')
  await expect(card.locator('tbody tr').first()).toContainText('2026-09-29 00:15:00')
  await expect(card.locator('tbody tr').last()).toContainText('2026-09-29 00:00:00')
  await page.getByText('查看原始行情结果', { exact: true }).click()
  await expect(page.locator('.call-body details pre')).toHaveText(result)
})

async function pollLog(page) {
  const response = page.waitForResponse((item) => new URL(item.url()).pathname === '/api/agent/log')
  await page.clock.fastForward(4100)
  await response
  await flushRender(page)
}

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test(`tool cards keep their height in a long conversation at ${viewport.width}px`, async (t) => {
    const { page } = await setup(t, conversation(40), { viewport })
    const cards = page.locator('.call.tool')
    const sizes = await cards.evaluateAll((items) => items.map((card) => ({
      height: card.getBoundingClientRect().height,
      header: card.querySelector('.call-head').getBoundingClientRect().height,
    })))
    assert.equal(sizes.length, 40)
    assert.ok(sizes.every(({ height, header }) => height >= header && header >= 34), JSON.stringify(sizes))
    for (const index of [0, 20, 39]) {
      const card = cards.nth(index)
      await card.locator('.call-head').click()
      await expect(card.locator('.call-body')).toBeVisible()
      await expect(card.locator('.call-body')).toContainText(`Account result ${index + 1}`)
      assert.ok(await card.evaluate((element) => element.clientHeight >= element.scrollHeight), 'expanded result is not clipped')
    }
  })
}

test('failed and empty log polls keep cards and their expanded results', async (t) => {
  const { page, state } = await setup(t, conversation(2))
  const first = page.locator('.call.tool').first()
  await first.locator('.call-head').click()
  state.failLog = true
  await pollLog(page)
  await expect(page.locator('.call.tool')).toHaveCount(2)
  await expect(first.locator('.call-head')).toHaveAttribute('aria-expanded', 'true')
  await expect(first.locator('.call-body')).toContainText('Account result 1')
  state.failLog = false
  state.lines = []
  await pollLog(page)
  await expect(page.locator('.call.tool')).toHaveCount(2)
  await expect(first.locator('.call-body')).toBeVisible()
})

test('an old HTTP response cannot remove tool events received through WebSocket', async (t) => {
  const { page, state } = await setup(t, conversation(1))
  let release
  state.snapshot = () => new Promise((resolve) => { release = resolve })
  const response = page.waitForResponse((item) => new URL(item.url()).pathname === '/api/agent/log')
  await page.clock.fastForward(4100)
  await expect.poll(() => typeof release).toBe('function')
  for (const entry of [line(4, 'tool', 'get_positions'), line(5, 'result', 'get_positions', 'Live position result')]) {
    state.socket.send(JSON.stringify({ ...entry, type: 'chat', phase: entry.role }))
  }
  await expect(page.locator('.call.tool')).toHaveCount(2)
  const latest = page.locator('.call.tool').last()
  await latest.locator('.call-head').click()
  await expect(latest.locator('.call-body')).toContainText('Live position result')
  release(state.lines)
  await response
  await flushRender(page)
  await expect(page.locator('.call.tool')).toHaveCount(2)
  await expect(latest.locator('.call-head')).toHaveAttribute('aria-expanded', 'true')
  await expect(latest.locator('.call-body')).toContainText('Live position result')
})

test('tool results stay in the same card when a user message arrives during execution', async (t) => {
  const { page, state } = await setup(t, [line(1, 'tool', 'get_account')])
  const card = page.locator('.call.tool')
  await card.locator('.call-head').click()
  state.lines = [line(1, 'tool', 'get_account'), line(2, 'user', '', 'Please continue'), line(3, 'result', 'get_account', 'Complete account result')]
  await pollLog(page)
  await expect(card).toHaveCount(1)
  await expect(card.locator('.call-head')).toHaveAttribute('aria-expanded', 'true')
  await expect(card.locator('.call-body')).toContainText('Complete account result')
  await expect(page.locator('.bubble.mine')).toContainText('Please continue')
})

test('parallel market cards keep symbols paired across live replies and refresh', async (t) => {
  const calls = [
    line(1, 'tool', 'get_market', '查看行情 {"symbol":"BTCUSDT"}'),
    line(2, 'tool', 'get_market', '查看行情 {"symbol":"ETHUSDT"}'),
    line(3, 'tool', 'get_account'),
  ]
  const { page, state } = await setup(t, calls)
  const cards = page.locator('.call.tool')
  await expect(cards).toHaveCount(3)
  await cards.nth(0).locator('.call-head').click()
  await cards.nth(1).locator('.call-head').click()
  const results = [
    line(4, 'result', 'get_account', 'Parallel account result'),
    line(5, 'result', 'get_market', 'BTCUSDT 1h 本页末根收盘价 100 EMA20 N/A MACD(12,26,9) N/A 信号 N/A 柱 N/A RSI7 N/A'),
    line(6, 'result', 'get_market', 'ETHUSDT 1h 本页末根收盘价 200 EMA20 N/A MACD(12,26,9) N/A 信号 N/A 柱 N/A RSI7 N/A'),
  ]
  for (const entry of results) state.socket.send(JSON.stringify({ ...entry, type: 'chat', phase: entry.role }))
  await expect(cards.nth(0).locator('.market-block h4')).toContainText('BTC')
  await expect(cards.nth(1).locator('.market-block h4')).toContainText('ETH')
  await expect(cards.nth(0).locator('.stat-grid')).toContainText('100')
  await expect(cards.nth(1).locator('.stat-grid')).toContainText('200')
  state.lines = [...calls, ...results]
  await page.reload()
  await expect(cards).toHaveCount(3)
  for (const [index, symbol] of [[0, 'BTC'], [1, 'ETH']]) {
    await cards.nth(index).locator('.call-head').click()
    await expect(cards.nth(index).locator('.market-block h4')).toContainText(symbol)
  }
})

test('a settings outage cannot block tool history loading', async (t) => {
  const { page } = await setup(t, conversation(3), { failSettings: true })
  await expect(page.locator('.call.tool')).toHaveCount(3)
  await page.locator('.call-head').first().click()
  await expect(page.locator('.call-body')).toContainText('Account result 1')
})

test('failed position reads never appear as no positions and old errors are dated', async (t) => {
  const { page } = await setup(t, [], { settings: { hasBinanceKey: true, testnet: true }, trader: { running: false, busy: false, lastError: 'historical SSH failure', stoppedAt: '2026-10-01T10:48:22Z' } })
  await page.route('**/api/positions', route => route.fulfill({ status: 502, json: { error: '币安连接被中断，未能取得最新数据。' } }))
  await page.reload()
  await expect(page.getByText('持仓尚未确认，请等待账户数据读取成功。')).toBeVisible()
  await expect(page.getByText('没有持仓', { exact: true })).toHaveCount(0)
  await expect(page.locator('.stat-row').first().locator('.stat strong')).toHaveText(['—','—','—','—'])
  await expect(page.getByText(/上次代理错误.*historical SSH failure/)).toBeVisible()
})

test('tool cards and results restore after navigation and a full page refresh', async (t) => {
  const { page } = await setup(t, conversation(20))
  await page.locator('nav button').nth(2).click()
  await page.locator('nav button').first().click()
  await expect(page.locator('.call.tool')).toHaveCount(20)
  await page.reload()
  await expect(page.locator('.call.tool')).toHaveCount(20)
  const first = page.locator('.call.tool').first()
  await first.locator('.call-head').click()
  await expect(first.locator('.call-body')).toContainText('Account result 1')
  assert.ok(await first.evaluate((element) => element.clientHeight >= element.scrollHeight))
})

test('paper mode has a visible entry and explains the configurator switch without changing it', async (t) => {
  const { page, state } = await setup(t, [], { settings: { hasBinanceKey: false, dryRun: true, testnet: false, keySource: 'freqtrade' } })
  const writes = []
  page.on('request', (request) => { if (request.method() !== 'GET') writes.push(request.url()) })
  await expect(page.locator('.page-head .pill.sim')).toHaveText('模拟（不发单）')
  await expect(page.locator('.page-head .pill.live')).toHaveCount(0)
  await expect(page.getByText('当前只验证代理决策', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: '交易模式', exact: true }).click()
  const section = page.getByRole('region', { name: '交易模式' })
  await expect(section).toContainText('勾选为模拟，取消勾选为实盘')
  await expect(section).toContainText('没有独立的虚拟资金、持仓和收益账本')
  await expect(page.locator('.segment')).toHaveCount(0)
  state.settings = { ...state.settings, dryRun: false }
  await page.getByRole('button', { name: '刷新当前模式' }).click()
  await expect(section.locator('.pill.live')).toHaveText('实盘')
  assert.deepEqual(writes, [], 'mode discovery and refresh never modify configuration')
})

test('locally configured accounts expose demo/live controls and require saving a choice', async (t) => {
  const { page } = await setup(t, [], { settings: { hasBinanceKey: false, dryRun: false, testnet: true, keySource: '' } })
  const writes = []
  page.on('request', (request) => { if (request.method() !== 'GET') writes.push(request.url()) })
  await expect(page.locator('.page-head .pill.sim')).toHaveText('币安模拟盘')
  await page.getByRole('button', { name: '交易模式', exact: true }).click()
  await expect(page.getByRole('button', { name: '币安模拟盘', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '实盘', exact: true }).click()
  await expect(page.getByRole('checkbox', { name: '我确认连接币安正式账户' })).not.toBeChecked()
  await expect(page.getByRole('region', { name: '交易模式' }).locator('.pill')).toHaveText('币安模拟盘')
  assert.deepEqual(writes, [], 'selecting a mode does not switch the account before saving')
})

const configuredPaper = { hasBinanceKey: true, keyHint: '0001', dryRun: true, testnet: false, keySource: 'freqtrade' }
const configuredLive = { ...configuredPaper, dryRun: false }
function demoSettings(configured = configuredPaper) {
  return { hasBinanceKey: true, keyHint: '0002', dryRun: false, testnet: true, keySource: 'local', binanceConnection: 'demo', hasDemoKey: true, demoKeyHint: '0002', configuredBinance: configured }
}

test('configurator users can save independent demo keys, test the connection and restore the selection after refresh', async (t) => {
  const { page, state } = await setup(t, [], { settings: { ...configuredPaper, binanceConnection: 'configured', hasDemoKey: false, configuredBinance: configuredPaper } })
  state.connectionResponse = demoSettings()
  const mutations = []
  page.on('request', (request) => { if (request.method() !== 'GET') mutations.push(new URL(request.url()).pathname) })
  await page.getByRole('button', { name: '交易模式', exact: true }).click()
  const panel = page.getByRole('region', { name: '币安模拟盘 · U 本位合约', exact: true })
  await expect(panel.getByRole('button', { name: '保存并启用模拟盘' })).toBeDisabled()
  await panel.getByLabel('模拟盘 API Key', { exact: true }).fill('demo-key-0002')
  await panel.getByLabel('模拟盘 API Secret', { exact: true }).fill('demo-test-secret')
  await panel.getByRole('button', { name: '保存并启用模拟盘' }).click()
  await expect(page.getByRole('region', { name: '交易模式', exact: true }).locator('.pill')).toHaveText('币安模拟盘')
  assert.deepEqual(state.connectionWrites, [{ connection: 'demo', apiKey: 'demo-key-0002', apiSecret: 'demo-test-secret' }])
  await expect(panel.getByLabel('模拟盘 API Secret', { exact: true })).toHaveValue('')
  await panel.getByRole('button', { name: '测试模拟盘连接' }).click()
  await expect(page.getByRole('status').filter({ hasText: '已连通' })).toBeVisible()
  assert.deepEqual(state.testedConnections, ['demo'])
  await page.locator('nav button').first().click()
  await expect(page.locator('.page-head .pill.sim')).toHaveText('币安模拟盘')
  await page.reload()
  await expect(page.locator('.page-head .pill.sim')).toHaveText('币安模拟盘')
  assert.deepEqual(mutations, ['/api/settings/binance/connection', '/api/binance/test'], 'saving and testing do not start the agent or submit trades')
})

test('a rejected demo switch keeps the original mode and the entered credentials for retry', async (t) => {
  const { page, state } = await setup(t, [], { settings: { ...configuredPaper, binanceConnection: 'configured', hasDemoKey: false } })
  state.connectionError = '请先停止代理并等待当前执行结束'
  await page.getByRole('button', { name: '交易模式', exact: true }).click()
  const panel = page.getByRole('region', { name: '币安模拟盘 · U 本位合约', exact: true })
  await panel.getByLabel('模拟盘 API Key', { exact: true }).fill('demo-key-0002')
  await panel.getByLabel('模拟盘 API Secret', { exact: true }).fill('demo-test-secret')
  await panel.getByRole('button', { name: '保存并启用模拟盘' }).click()
  await expect(page.getByRole('alert')).toContainText('请先停止代理')
  await expect(page.getByRole('region', { name: '交易模式', exact: true }).locator('.pill')).toHaveText('模拟（不发单）')
  await expect(panel.getByLabel('模拟盘 API Secret', { exact: true })).toHaveValue('demo-test-secret')
  await expect(panel.getByRole('button', { name: '测试模拟盘连接' })).toBeDisabled()
})

test('running agents cannot change demo credentials or switch connections', async (t) => {
  const { page, state } = await setup(t, [], { settings: demoSettings(), trader: { running: true, busy: false, lastError: '' } })
  await page.getByRole('button', { name: '交易模式', exact: true }).click()
  await expect(page.getByRole('button', { name: '保存并启用模拟盘' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '切回原配置' })).toBeDisabled()
  assert.deepEqual(state.connectionWrites, [])
})

test('restoring a live profile requires a fresh confirmation and retains demo credentials', async (t) => {
  const { page, state } = await setup(t, [], { settings: demoSettings(configuredLive) })
  state.connectionResponse = { ...configuredLive, binanceConnection: 'configured', hasDemoKey: true, demoKeyHint: '0002', configuredBinance: configuredLive }
  await page.getByRole('button', { name: '交易模式', exact: true }).click()
  const restore = page.getByRole('button', { name: '切回原配置' })
  await expect(restore).toBeDisabled()
  await page.getByRole('checkbox', { name: '我确认切回币安正式账户并允许真实下单' }).check()
  await restore.click()
  await expect(page.getByRole('region', { name: '交易模式', exact: true }).locator('.pill.live')).toHaveText('实盘')
  assert.deepEqual(state.connectionWrites, [{ connection: 'configured', confirmLive: true }])
  await expect(page.getByLabel('模拟盘 API Key', { exact: false })).toHaveAttribute('placeholder', '留空复用已保存的模拟盘 Key')
})

test('an older engine cannot expose an unusable demo activation button', async (t) => {
  const { page, state } = await setup(t, [], { settings: configuredPaper })
  await page.getByRole('button', { name: '交易模式', exact: true }).click()
  const panel = page.getByRole('region', { name: '币安模拟盘 · U 本位合约', exact: true })
  await panel.getByLabel('模拟盘 API Key', { exact: true }).fill('demo-key-0002')
  await panel.getByLabel('模拟盘 API Secret', { exact: true }).fill('demo-test-secret')
  await expect(panel.getByRole('button', { name: '保存并启用模拟盘' })).toBeDisabled()
  await expect(panel.getByRole('status')).toContainText('请先运行更新后的交易台引擎')
  assert.deepEqual(state.connectionWrites, [])
})

const protectionDemoSettings = { hasBinanceKey: true, binanceConnection: 'demo', testnet: true, dryRun: false, modelName: 'Test model' }

test('stop cause is visible after refresh and disappears while agent runs', async t => {
  const { page, state } = await setup(t, [], { trader: { running: false, busy: false, lastError: '', stopKind: 'maintenance', stopReason: 'Engine update completed; trading remains stopped', stoppedAt: '2026-09-30T09:19:28Z' } })
  const reason = page.locator('.rail-stop-reason')
  await expect(reason).toContainText('维护停止')
  await expect(reason).toContainText('Engine update completed')
  await page.reload()
  await expect(reason).toContainText('Engine update completed')
  state.trader.running = true
  await page.reload()
  await expect(reason).toHaveCount(0)
  await expect(page.locator('.rail-title')).toContainText('运行中')
})
const demoPosition = { symbol: 'BTCUSDT', side: 'long', amount: 1, entry: 100, mark: 101, pnl: 1, leverage: 5, notional: 101, liquidation: 20, protectionKnown: true, protection: [] }
const makeCandle = (time, close, extra = {}) => ({ time, open: 100, high: 120, low: 80, close, volume: 2, ...extra })

for (const width of [1440, 390]) {
  test('confirmed protection and realized PnL display; repair requires explicit submission at ' + width, async t => {
    const { page, state } = await setup(t, [], { settings: protectionDemoSettings, viewport: { width, height: 900 } })
    state.positions = [{ ...demoPosition, protection: [{ kind: 'stop', side: 'long', price: 90, protective: true, workingType: 'MARK_PRICE' }] }]
    state.protectionReply = body => {
      state.positions[0].protection.push({ kind: 'take', side: 'long', price: body.take_profit, protective: true, workingType: 'MARK_PRICE' })
      return { confirmed: true, stopLoss: 90, takeProfit: body.take_profit, message: '已查询确认保护单' }
    }
    await page.reload()
    const table = page.locator('.positions-table')
    await expect(table).toContainText('90')
    await expect(table).toContainText('未设置')
    await expect(page.locator('.pnl-card')).toContainText('12.00')
    await expect(page.locator('.pnl-card')).toContainText('-2.00')
    await page.getByLabel('盈亏统计周期').selectOption('7d')
    await expect.poll(() => state.pnlRequests.at(-1)).toBe('7d')
    await page.getByRole('button', { name: '补挂保护单', exact: true }).click()
    const form = page.getByRole('form', { name: '补挂持仓保护单' })
    await expect(form.getByLabel('止损触发价')).toHaveCount(0)
    await form.getByLabel('止盈触发价').fill('110')
    assert.equal(state.protectionWrites.length, 0)
    await form.getByRole('button', { name: '确认补挂保护单' }).click()
    await expect(form.getByRole('status')).toContainText('已查询确认')
    assert.deepEqual(state.protectionWrites, [{ symbol: 'BTCUSDT', side: 'long', stop_loss: 0, take_profit: 110 }])
    await expect(table).toContainText('110')
    await expect(page.locator('.level-row')).toContainText('90（标记价触发）')
    assert.ok(await page.locator('.page').evaluate(el => el.scrollWidth <= el.clientWidth + 1), 'dashboard fits viewport')
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'document fits viewport')
  })
}

test('unknown protection and failed PnL do not look like missing orders or zero profit', async t => {
  const { page, state } = await setup(t, [], { settings: protectionDemoSettings })
  state.positions = [{ ...demoPosition, protectionKnown: false, protectionError: 'orders unavailable' }]
  state.pnlError = 'income unavailable'
  await page.reload()
  await expect(page.locator('.positions-table')).toContainText('读取失败')
  await expect(page.locator('.pnl-card').getByRole('alert')).toHaveText('income unavailable')
  await expect(page.locator('.pnl-card .stat strong')).toHaveText(['—', '—', '—', '—'])
  await page.getByRole('button', { name: '补挂保护单', exact: true }).click()
  await expect(page.getByRole('button', { name: '确认补挂保护单' })).toBeDisabled()
  assert.equal(state.protectionWrites.length, 0)
})

test('live chart buffers early ticks, rejects old snapshots, follows new bars and shows disconnection', async t => {
  const { page, state } = await setup(t, [])
  let releaseHistory, releaseTail
  state.klineSnapshot = url => new Promise(resolve => { if (url.searchParams.get('limit') === '5') releaseTail = resolve; else releaseHistory = resolve })
  state.chartSocket = null
  await page.reload()
  await expect.poll(() => typeof releaseHistory).toBe('function')
  await expect.poll(() => !!state.chartSocket).toBe(true)
  await flushRender(page)
  const now = await page.evaluate(() => Date.now())
  const tailTime = Math.floor(now / 900000) * 900
  const tick = (close, time = tailTime) => makeCandle(time, close, { source: 'websocket', updatedAt: now + close })
  state.chartSocket.send(JSON.stringify(tick(111)))
  await expect(page.getByTestId('live-price')).toHaveText('111')
  releaseHistory(Array.from({ length: 200 }, (_, i) => makeCandle(tailTime - (199 - i) * 900, 100)))
  await expect(page.getByTestId('live-price')).toHaveText('111')
  await page.clock.fastForward(10100)
  await expect.poll(() => typeof releaseTail).toBe('function')
  state.chartSocket.send(JSON.stringify({ ...tick(112), updatedAt: await page.evaluate(() => Date.now()) }))
  await expect(page.getByTestId('live-price')).toHaveText('112')
  releaseTail([makeCandle(tailTime, 90)])
  await flushRender(page)
  await expect(page.getByTestId('live-price')).toHaveText('112')
  state.chartSocket.send(JSON.stringify({ ...tick(113, tailTime + 900), updatedAt: await page.evaluate(() => Date.now()) }))
  await expect(page.getByTestId('live-price')).toHaveText('113')
  await expect(page.getByRole('button', { name: '跟随实时' })).toHaveAttribute('aria-pressed', 'true')
  await page.locator('.chart').scrollIntoViewIfNeeded()
  const box = await page.locator('.chart').boundingBox()
  await page.mouse.move(box.x + box.width * .3, box.y + box.height * .5)
  await page.mouse.down(); await page.mouse.move(box.x + box.width * .8, box.y + box.height * .5, { steps: 10 }); await page.mouse.up()
  await expect(page.getByRole('button', { name: '返回实时' })).toBeVisible()
  await page.getByRole('button', { name: '返回实时' }).click()
  await expect(page.getByRole('button', { name: '跟随实时' })).toHaveAttribute('aria-pressed', 'true')
  await page.clock.fastForward(6100)
  await expect(page.getByTestId('live-status')).not.toHaveText('交易所实时推送')
})

for (const width of [1440, 390]) {
 test('analytics cards preserve full rows, partial errors and source after reload at '+width, async t=>{
  const rows=Array.from({length:200},(_,i)=>({time:1790640000+i*60,closed:i<199,close:100+i,ema20:i<19?null:90+i,macd:i<25?null:1,signal:i<33?null:0.5,histogram:i<33?null:0.5,rsi7:i<7?null:60,atr14:i<13?null:2}))
  const result={tool:'get_indicators',status:'ok',source:{environment:'demo',exchange:'Binance USD-M Futures',host:'https://demo-fapi.binance.com',referenceOnly:false},fetchedAt:'2026-09-30T10:00:00Z',request:{symbol:'BTCUSDT',interval:'1m'},data:{indicators:{count:200,nextEnd:1790640000,rows}},notes:['Indicators are calculated by the program; null means insufficient warmup.']}
  const partial={tool:'get_open_interest',status:'partial',source:result.source,fetchedAt:result.fetchedAt,request:{symbol:'BTCUSDT'},data:{current:{openInterest:'100',time:1790760000000}},errors:{history:'Demo endpoint returned non-JSON'},notes:[]}
  const unavailable={tool:'get_order_flow',status:'unavailable',source:result.source,request:{symbol:'BTCUSDT'},data:{},errors:{flow:'Statistics unavailable'},notes:[]}
  const {page}=await setup(t,[line(1,'tool','get_indicators'),line(2,'tool','get_open_interest'),line(3,'result','get_open_interest',JSON.stringify(partial)),line(4,'tool','get_order_flow'),line(5,'result','get_order_flow',JSON.stringify(unavailable)),line(6,'result','get_indicators',JSON.stringify(result))],{viewport:{width,height:900}})
  for(let refresh=0;refresh<2;refresh++){
   if(refresh)await page.reload()
   const cards=page.locator('.call.tool')
   for(let i=0;i<3;i++)await cards.nth(i).locator('.call-head').click()
   await expect(cards.nth(0).locator('tbody tr')).toHaveCount(200)
   await expect(cards.nth(0).locator('tbody tr').first()).toContainText('—')
   await expect(cards.nth(0).locator('tbody tr').last()).toContainText('299')
   await expect(cards.nth(0)).toContainText('币安模拟盘')
   await expect(cards.nth(1)).toContainText('部分可用')
   await expect(cards.nth(1)).toContainText('Demo endpoint returned non-JSON')
   await expect(cards.nth(2)).toContainText('数据不可用')
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth+1),'analytics table stays within viewport')
  }
 })
}
test('analytics reference source and incomplete execution prices stay explicit',async t=>{
 const result={tool:'get_order_book',status:'ok',source:{environment:'live_reference',referenceOnly:true,tradingEnvironment:'demo',host:'https://fapi.binance.com'},request:{symbol:'BTCUSDT'},data:{book:{bestBid:99,bestAsk:101,buyEstimate:{fullyCovered:false,filledQuantity:1,fullOrderVWAP:null,slippageBps:null}}},notes:['Visible depth only; no fees included.']}
 const {page}=await setup(t,[line(1,'tool','get_order_book'),line(2,'result','get_order_book',JSON.stringify(result))])
 await page.locator('.call-head').click()
 await expect(page.locator('.analytics-result')).toContainText('实盘公开参考数据')
 await expect(page.locator('.analytics-result')).toContainText('不是模拟账户成交')
 await expect(page.locator('.analytics-result')).toContainText('交易账户环境：币安模拟盘')
 await expect(page.locator('.analytics-fields').last()).toContainText('否')
 await expect(page.locator('.analytics-fields').last()).toContainText('完整订单均价—')
})
