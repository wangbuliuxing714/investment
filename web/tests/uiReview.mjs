// Read-only visual review. Browser APIs are mocked and writes rejected.
import fs from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
const require = createRequire(import.meta.url)
const { chromium } = require(process.env.DESK_PLAYWRIGHT_MODULE || '@playwright/test')
const output = fileURLToPath(new URL('../../engine/data/ui-review-20260930/', import.meta.url))
await fs.mkdir(output, { recursive: true })
const read = async path => { const r = await fetch('http://127.0.0.1:8787/api/' + path); if (!r.ok) throw Error(path); return r.json() }
const [settings,strategy,prompts,tools] = await Promise.all(['settings','strategy','settings/prompts','settings/tools'].map(read))
const labels = [['coins','交易币种',''],['btcEthLeverage','BTC / ETH 杠杆','倍'],['altLeverage','其他币种杠杆','倍'],['maxPositions','最多持仓','个'],['positionValueRatio','单笔名义价值上限','倍净值'],['maxMarginPercent','保证金占用上限','%'],['minConfidence','最低把握','']]
const values = {...strategy,coins:strategy.coins.join(', '),maxMarginPercent:(strategy.maxMarginUsage*100).toFixed(0)}
const parameters = labels.map(([key,label,unit]) => ({key,label,unit,value:String(values[key])}))
const render = text => parameters.reduce((s,p) => s.replaceAll('{{'+p.key+'}}',p.value),text)
if (prompts.prompts.trading) {
  const old = prompts.prompts
  prompts.prompts = { system:[old.system,'## 交易规则',old.trading,'## 自定义交易说明',old.strategy,'## 历史处理规则',old.history].join('\n\n'),chat:old.chat,start:old.start,noTool:old.noTool,resume:old.resume }
  prompts.definitions = prompts.definitions.filter(d => Object.hasOwn(prompts.prompts,d.key))
}
prompts.parameters = parameters
prompts.preview = {system:render(prompts.prompts.system),instructions:'',chat:render(prompts.prompts.chat)}
tools.parameters = parameters
const fixtures = {'/api/settings':settings,'/api/strategy':strategy,'/api/settings/prompts':prompts,'/api/settings/tools':tools,'/api/models':[{id:settings.modelId,name:settings.modelName,providerId:settings.providerId,upstreamModel:settings.upstreamModel}],'/api/symbols':['BTCUSDT','ETHUSDT','SOLUSDT','BNBUSDT'],'/api/trader':{running:false,busy:false,lastError:''},'/api/account':{equity:10000,wallet:10000,available:9200,unrealized:0,marginUsed:800,testnet:true},'/api/pnl':{asset:'USDT',realized:0,commission:0,funding:0,net:0,updatedAt:Date.now()}}
const browser = await chromium.launch({headless:true})
const errors = []
try {
  for (const width of [1440,1024,768,390]) {
    const page = await browser.newPage({viewport:{width,height:960},reducedMotion:'reduce'})
    page.on('pageerror', e => errors.push(e.message))
    await page.routeWebSocket('**/api/ws', () => {})
    await page.routeWebSocket('**/api/klines/live?**', () => {})
    await page.route('**/api/**', route => {
      if (route.request().method() !== 'GET') throw Error('Visual review attempted a write')
      return route.fulfill({json:fixtures[new URL(route.request().url()).pathname] ?? []})
    })
    await page.goto(process.env.DESK_TEST_URL || 'http://127.0.0.1:3212')
    for (const [label,name] of [['模型','models'],['Tools 管理','tools'],['交易所','exchange'],['策略','strategy'],['看板','dashboard']]) {
      await page.getByRole('button',{name:label,exact:true}).click()
      await page.waitForTimeout(150)
      await page.screenshot({path:output+'/'+name+'-'+width+'.png'})
      if (name === 'models') {
        const parameter = page.locator('.template-surface .parameter-token').first()
        if (await parameter.count()) {
          await parameter.scrollIntoViewIfNeeded()
          await page.screenshot({path:output+'/parameters-'+width+'.png'})
        }
      }
      const geometry = await page.evaluate(() => ({viewport:innerWidth,scroll:document.documentElement.scrollWidth}))
      console.log(JSON.stringify({page:name,width,...geometry}))
    }
    await page.close()
  }
} finally { await browser.close() }
if (errors.length) throw Error(errors.join('\n'))
