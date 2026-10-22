import { useCallback, useEffect, useState } from 'react'
import { api, type Settings, type TraderState } from '../api'
import { usd } from '../format'
import { tradingMode } from '../tradingMode'

export function Exchange() {
  const [settings, setSettings] = useState<Settings | null>(null)
  const [apiKey, setApiKey] = useState('')
  const [apiSecret, setApiSecret] = useState('')
  const [testnet, setTestnet] = useState(true)
  const [confirmLive, setConfirmLive] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [testText, setTestText] = useState('')
  const [saving, setSaving] = useState(false)
  const [trader, setTrader] = useState<TraderState | null>(null)
  const [demoApiKey, setDemoApiKey] = useState('')
  const [demoApiSecret, setDemoApiSecret] = useState('')
  const [confirmRestore, setConfirmRestore] = useState(false)

  const refreshSettings = useCallback(async () => {
    try {
      const [value, state] = await Promise.all([api.settings(), api.trader()])
      setSettings(value)
      setTrader(state)
      setConfirmRestore(false)
      setTestnet(value.testnet)
      setConfirmLive(!value.testnet)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : '读取模式失败')
    }
  }, [])

  useEffect(() => {
    void refreshSettings()
  }, [refreshSettings])

  async function save() {
    if (!testnet && !confirmLive) {
      setError('切换实盘前，先勾选确认')
      return
    }
    setSaving(true)
    setError('')
    setMessage('')
    setTestText('')
    try {
      const saved = await api.saveBinance({ apiKey, apiSecret, testnet })
      setSettings(saved)
      setApiSecret('')
      setMessage(testnet ? '模拟盘密钥已保存' : '实盘密钥已保存')
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  async function test() {
    setTestText('')
    setError('')
    try {
      const result = await api.testBinance()
      const mode = result.paper ? '模拟交易，未发单' : result.testnet ? '模拟盘' : '实盘'
      setTestText(`${mode}已连通，净值 ${usd(result.equity)}，可用 ${usd(result.available)}，${result.hedge ? '双向持仓' : '单向持仓'}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : '连接失败')
    }
  }

  async function selectConnection(connection: 'configured' | 'demo') {
    setError('')
    setMessage('')
    setTestText('')
    setSaving(true)
    try {
      const saved = await api.saveBinanceConnection(connection === 'demo'
        ? { connection, apiKey: demoApiKey, apiSecret: demoApiSecret }
        : { connection, confirmLive: confirmRestore })
      setSettings(saved)
      setTestnet(saved.testnet)
      setConfirmLive(!saved.testnet && !saved.dryRun)
      setConfirmRestore(false)
      setDemoApiKey('')
      setDemoApiSecret('')
      setMessage(connection === 'demo' ? '已启用币安模拟盘。请测试连接，成功后可回看板启动代理。' : `已切回原配置：${tradingMode(saved).label}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : '切换交易环境失败')
    } finally {
      setSaving(false)
    }
  }

  const fromConfigurator = settings?.keySource === 'freqtrade'
  const mode = tradingMode(settings)
  const demoActive = settings?.binanceConnection === 'demo'
  const demoSupported = settings?.binanceConnection === 'configured' || demoActive
  const configured = settings?.configuredBinance || settings
  const configuredMode = tradingMode(configured)
  const exchangeBusy = !trader || trader.running || trader.busy
  const demoPairReady = settings?.hasDemoKey
    ? (!demoApiKey.trim() || !!demoApiSecret.trim())
    : !!demoApiKey.trim() && !!demoApiSecret.trim()

  return (
    <div className="page exchange-page">
      <div className="page-head"><div><span className="eyebrow">CONNECTIONS</span><h1>交易所</h1><p className="sub">账户连接与交易环境，集中管理。</p></div></div>
      <section className="card form" aria-labelledby="trading-mode-title">
        <div className="row gap wrap">
          <h2 id="trading-mode-title">交易模式</h2>
          <span className={mode.className}>{mode.label}</span>
          <button className="btn" type="button" disabled={saving} onClick={() => void refreshSettings()}>刷新当前模式</button>
        </div>
        {demoActive ? (
          <p>当前连接币安模拟环境，代理会在模拟账户中执行订单。正式账户的配置已保留。</p>
        ) : fromConfigurator ? (
          <>
            <p>当前使用桌面配置器。要使用币安模拟盘，可直接在下方填写专用密钥并启用，无需修改配置器。</p>
            <ol className="mode-steps">
              <li>先在看板停止代理，等待当前执行结束。</li>
              <li>打开桌面配置器，在「交易模式」中找到「模拟交易」复选框：勾选为模拟，取消勾选为实盘。</li>
              <li>点击配置器的「保存配置」，再点击下方「刷新当前模式」核对。</li>
            </ol>
          </>
        ) : settings ? (
          <p>在下方选择「币安模拟盘」或「实盘」，填写对应环境的密钥并保存。切换前先停止代理。</p>
        ) : <p className="sub">正在读取当前模式和配置来源…</p>}
        <table className="mode-table">
          <thead><tr><th>模式</th><th>订单如何处理</th></tr></thead>
          <tbody>
            <tr><td>模拟（不发单）</td><td>读取正式账户和行情，代理给出决策，程序拦截开平仓指令。目前没有独立的虚拟资金、持仓和收益账本。</td></tr>
            <tr><td>币安模拟盘</td><td>连接币安模拟环境，在模拟账户执行订单。需要该环境的专用密钥。</td></tr>
            <tr><td>实盘</td><td>连接币安正式账户，按策略真实下单，成交和盈亏会影响真实资金。</td></tr>
          </tbody>
        </table>
        {mode.id === 'paper' && <p className="sub">当前显示的净值和持仓来自正式账户，不代表模拟收益。模拟模式不会关闭已有的真实持仓。</p>}
      </section>
      <section className="card form" aria-labelledby="binance-demo-title">
        <div className="row gap wrap">
          <h2 id="binance-demo-title">币安模拟盘 · U 本位合约</h2>
          {demoActive && <span className="pill sim">已启用</span>}
        </div>
        <p className="sub">在币安模拟交易页面创建专用 API Key 和 Secret，填入后点击「保存并启用模拟盘」。模拟盘密钥单独保存，不会复用或覆盖正式账户密钥。</p>
        <p><a href="https://demo.binance.com" target="_blank" rel="noopener noreferrer">打开币安模拟交易</a></p>
        {settings && !demoSupported && <p className="sub" role="status">币安模拟盘功能已添加，请先运行更新后的交易台引擎，再刷新此页启用。</p>}
        <label>
          <span>模拟盘 API Key {settings?.demoKeyHint ? `（已保存，末四位 ${settings.demoKeyHint}）` : ''}</span>
          <input value={demoApiKey} onChange={(event) => setDemoApiKey(event.target.value)} placeholder={settings?.hasDemoKey ? '留空复用已保存的模拟盘 Key' : '币安模拟盘专用 API Key'} autoComplete="off" disabled={saving} />
        </label>
        <label>
          <span>模拟盘 API Secret</span>
          <input type="password" value={demoApiSecret} onChange={(event) => setDemoApiSecret(event.target.value)} placeholder={settings?.hasDemoKey ? '留空复用模拟盘 Secret；更换 Key 时请同时填写' : '币安模拟盘专用 API Secret'} autoComplete="new-password" disabled={saving} />
        </label>
        {exchangeBusy && settings && <p className="sub">先停止代理并等待当前执行结束，再保存或切换交易环境。</p>}
        <div className="row gap wrap">
          <button className="btn primary" type="button" disabled={saving || !settings || !demoSupported || exchangeBusy || !demoPairReady} onClick={() => void selectConnection('demo')}>{saving ? '保存中…' : '保存并启用模拟盘'}</button>
          <button className="btn" type="button" disabled={saving || !demoActive || !settings?.hasBinanceKey} onClick={test}>测试模拟盘连接</button>
        </div>
        {demoActive && (
          <div className="mode-restore">
            <p className="sub">原配置：{configuredMode.label}。切回后，模拟盘密钥仍会保留。</p>
            {configuredMode.id === 'live' && (
              <label className="check">
                <input type="checkbox" checked={confirmRestore} onChange={(event) => setConfirmRestore(event.target.checked)} />
                我确认切回币安正式账户并允许真实下单
              </label>
            )}
            <button className="btn" type="button" disabled={saving || exchangeBusy || (configuredMode.id === 'live' && !confirmRestore)} onClick={() => void selectConnection('configured')}>切回原配置</button>
          </div>
        )}
      </section>
      {!demoActive && <div className="card form">
        <h2>币安 U 本位合约</h2>
        {fromConfigurator ? (
          <p className="sub">密钥读取自桌面上的 Freqtrade 币安配置器，末四位 {settings?.keyHint || '—'}。密钥和交易模式共用该配置，以页面上方显示的模式为准。</p>
        ) : (
          <p className="sub">这里只配置交易所。密钥写在本机数据库。模拟盘密钥要在币安模拟交易里单独创建。</p>
        )}
        {!fromConfigurator && settings && (
          <>
            <div className="segment">
              <button className={testnet ? 'on' : ''} onClick={() => setTestnet(true)} type="button">币安模拟盘</button>
              <button className={!testnet ? 'on' : ''} onClick={() => setTestnet(false)} type="button">实盘</button>
            </div>
            {!testnet && (
              <label className="check">
                <input type="checkbox" checked={confirmLive} onChange={(e) => setConfirmLive(e.target.checked)} />
                我确认连接币安正式账户
              </label>
            )}
            <label>
              <span>API Key {settings?.keyHint ? `（已保存，末四位 ${settings.keyHint}）` : ''}</span>
              <input value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={settings?.hasBinanceKey ? '留空不会改已保存的 Key' : 'API Key'} autoComplete="off" />
            </label>
            <label>
              <span>API Secret</span>
              <input type="password" value={apiSecret} onChange={(e) => setApiSecret(e.target.value)} placeholder={settings?.hasBinanceKey ? '留空则不修改' : 'API Secret'} autoComplete="off" />
            </label>
          </>
        )}
        <div className="row gap">
          {!fromConfigurator && <button className="btn primary" disabled={saving || !settings || exchangeBusy} onClick={save}>保存</button>}
          <button className="btn" disabled={!settings?.hasBinanceKey} onClick={test}>测试连接</button>
        </div>
      </div>}
      {testText && <p className="ok" role="status">{testText}</p>}
      {message && <p className="ok" role="status">{message}</p>}
      {error && <p className="err" role="alert">{error}</p>}
    </div>
  )
}
