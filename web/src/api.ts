export interface BinanceProfile {
  hasBinanceKey: boolean
  keyHint: string
  testnet: boolean
  keySource: string
  dryRun: boolean
}

export interface Settings extends BinanceProfile {
  binanceConnection?: 'configured' | 'demo'
  hasDemoKey?: boolean
  demoKeyHint?: string
  configuredBinance?: BinanceProfile
  modelId: string
  modelName: string
  providerId: string
  upstreamModel: string
}

export interface PromptParameter { key: string; label: string; value: string; unit: string }

export interface PromptPreset { id: string; name: string; description: string; prompt: string }

export interface PromptSettingsView {
  prompts: Record<string, string>
  definitions: {
    key: string
    label: string
    description: string
    category: 'system' | 'common' | 'chat' | 'automation'
    role: 'system' | 'request' | 'event'
    scenario: string
    placement: string
    effectiveWhen: string
    default: string
    required: boolean
  }[]
  variables: string[]
  parameters: PromptParameter[]
  presets?: PromptPreset[]
  invest: WorkspaceFile
  preview: { system: string; instructions: string; chat: string; trading: string; optimization: string }
}

export interface ToolsSettingsView {
  config: { instructions: string }
  defaults: { instructions: string }
  definitions: {
    name: string
    label: string
    description: string
    effect: string
    scope: string
    example: string
    parameters: { name: string; type: string; required: boolean; description: string }[]
  }[]
  variables: string[]
  parameters: PromptParameter[]
  preview: string
}

export interface Strategy {
  name: string
  coins: string[]
  btcEthLeverage: number
  altLeverage: number
  maxPositions: number
  minConfidence: number
  positionValueRatio: number
  maxMarginUsage: number
  scanIntervalSec: number
  prompt: string
  timeframe: string
  klineCount: number
}

export interface TraderState {
	mode?: 'trading' | 'optimization'
	conversationId?: string
	switching?: boolean
  running: boolean
  startedAt: string
  lastScanAt: string
  lastError: string
  busy: boolean
  stopKind?: string
  stopReason?: string
  stoppedAt?: string
}

export interface Account {
  equity: number
  wallet: number
  available: number
  unrealized: number
  marginUsed: number
  testnet: boolean
}

export interface Position {
  symbol: string
  side: string
  amount: number
  entry: number
  mark: number
  pnl: number
  leverage: number
  notional: number
  liquidation: number
  protection?: Level[]
  protectionKnown?: boolean
  protectionError?: string
}

export interface Candle {
	closed?: boolean
  time: number
  open: number
  high: number
  low: number
  close: number
  volume: number
  updatedAt?: number
  source?: string
}

export interface Level {
  kind: string
  side: string
  price: number
  protective?: boolean
  workingType?: string
}

export interface PnLSummary {
  asset: string
  from: number
  to: number
  realized: number
  commission: number
  funding: number
  net: number
  updatedAt: number
}

export interface ProtectionResult {
  stopLoss: number
  takeProfit: number
  confirmed: boolean
  message: string
}

export interface AdminModel {
  id: string
  name: string
  providerId: string
  upstreamModel: string
}

export interface DecisionRecord {
  id: number
  createdAt: string
  status: string
  reasoning: string
  rawResponse: string
  inputPrompt: string
  decisionsJson: string
  executionLog: string
  error: string
}

export interface ModelDecision {
  symbol: string
  action: string
  leverage: number
  position_size_usd: number
  stop_loss: number
  take_profit: number
  confidence: number
  reasoning: string
}

export interface ChatLine {
	conversationId?: string
  id: number
  role: string
  name?: string
  text: string
  at: string
}

export interface ScanEvent {
	conversationId?: string
  type: string
  phase?: string
  decisionId?: number
  id?: number
  name?: string
  text?: string
  at?: string
  running?: boolean
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  })
  const data = (await res.json().catch(() => ({}))) as T & { error?: string }
  if (!res.ok) throw new Error(data.error || `请求失败 ${res.status}`)
  return data
}

export interface WorkspaceFile { path: string; directory: boolean; content: string; revision: number; updatedAt: string }

export interface HistoryDataset { market: 'spot' | 'um'; symbol: string; source: string; status: string; rows: number; first: number; last: number; availableFirst: number; syncedThrough: number; archivesDone: number; archivesTotal: number; gapCount: number; missingMinutes: number; updatedAt: string; error: string }
export interface HistoryGap { from: number; to: number; minutes: number }
export interface HistoryPage { candles: (Candle & { closed?: boolean })[]; dataset: HistoryDataset; nextEnd: number; hasMore: boolean }

export const api = {
  history: () => request<{ datasets: HistoryDataset[]; gaps: Record<string, HistoryGap[]> }>('/api/history'),
  historyKlines: (market: string, interval: string, end = 0, limit = 1500) => request<HistoryPage>(`/api/history/klines?market=${encodeURIComponent(market)}&interval=${encodeURIComponent(interval)}&end=${end}&limit=${limit}`),
  syncHistory: (market: string) => request<{ started: boolean }>(`/api/history/sync?market=${encodeURIComponent(market)}`, { method: 'POST' }),
  files: (path: string) => request<WorkspaceFile[]>(`/api/files?path=${encodeURIComponent(path)}`),
  readFile: (path: string) => request<WorkspaceFile>(`/api/files?read=1&path=${encodeURIComponent(path)}`),
  writeFile: (path: string, content: string, revision: number) => request<WorkspaceFile>('/api/files', { method: 'POST', body: JSON.stringify({ name: 'write_file', arguments: { path, content, revision } }) }),
  createDirectory: (path: string) => request<WorkspaceFile>('/api/files', { method: 'POST', body: JSON.stringify({ name: 'create_directory', arguments: { path } }) }),
  tools: () => request<ToolsSettingsView>('/api/settings/tools'),
  saveTools: (instructions: string) => request<ToolsSettingsView>('/api/settings/tools', { method: 'PUT', body: JSON.stringify({ instructions }) }),
  prompts: () => request<PromptSettingsView>('/api/settings/prompts'),
  savePrompts: (prompts: Record<string, string>) => request<PromptSettingsView>('/api/settings/prompts', { method: 'PUT', body: JSON.stringify({ prompts }) }),
  settings: () => request<Settings>('/api/settings'),
  saveBinance: (body: { apiKey: string; apiSecret: string; testnet: boolean }) =>
    request<Settings>('/api/settings/binance', { method: 'PUT', body: JSON.stringify(body) }),
  saveBinanceConnection: (body: { connection: 'configured' | 'demo'; apiKey?: string; apiSecret?: string; confirmLive?: boolean }) =>
    request<Settings>('/api/settings/binance/connection', { method: 'PUT', body: JSON.stringify(body) }),
  saveModel: (body: AdminModel) =>
    request<Settings>('/api/settings/model', {
      method: 'PUT',
      body: JSON.stringify({
        id: body.id,
        name: body.name,
        providerId: body.providerId,
        upstreamModel: body.upstreamModel,
      }),
    }),
  models: () => request<AdminModel[]>('/api/models'),
  testBinance: () => request<{ ok: boolean; testnet: boolean; paper: boolean; equity: number; available: number; hedge: boolean }>('/api/binance/test', { method: 'POST' }),
  strategy: () => request<Strategy>('/api/strategy'),
  saveStrategy: (body: Strategy) => request<Strategy>('/api/strategy', { method: 'PUT', body: JSON.stringify(body) }),
  symbols: () => request<string[]>('/api/symbols'),
  account: () => request<Account>('/api/account'),
  positions: () => request<Position[]>('/api/positions'),
  pnl: (period: string) => request<PnLSummary>(`/api/pnl?period=${encodeURIComponent(period)}`),
  setProtection: (body: { symbol: string; side: string; stop_loss: number; take_profit: number }) =>
    request<ProtectionResult>('/api/positions/protection', { method: 'POST', body: JSON.stringify(body) }),
  closePosition: (symbol: string, side: string) =>
    request<{ orderID?: number; warning?: string }>('/api/positions/close', { method: 'POST', body: JSON.stringify({ symbol, side }) }),
  klines: (symbol: string, interval: string, end?: number, limit = 1500) => {
    const query = new URLSearchParams({ symbol, interval, limit: String(limit) })
    if (end && end > 0) query.set('end', String(Math.floor(end)))
    return request<Candle[]>(`/api/klines?${query}`)
  },
  orders: (symbol: string) => request<Level[]>(`/api/orders?symbol=${encodeURIComponent(symbol)}`),
  trader: () => request<TraderState>('/api/trader'),
  start: (conversationId?: string) => request<TraderState>('/api/trader/start', { method: 'POST', body: JSON.stringify({ conversationId }) }),
  agentMode: (mode: 'trading' | 'optimization', conversationId: string) => request<TraderState>('/api/agent/mode', { method: 'PUT', body: JSON.stringify({ mode, conversationId }) }),
  stop: () => request<TraderState>('/api/trader/stop', { method: 'POST' }),
  agentLog: (conversationId?: string) => request<ChatLine[]>(`/api/agent/log${conversationId ? `?conversationId=${encodeURIComponent(conversationId)}` : ''}`),
  ask: (text: string, conversationId?: string) => request<{ ok: boolean }>('/api/agent/ask', { method: 'POST', body: JSON.stringify({ text, conversationId }) }),
  decisions: () => request<DecisionRecord[]>('/api/decisions?limit=30'),
}
