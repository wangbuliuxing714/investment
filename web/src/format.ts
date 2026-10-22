export function usd(value: number) {
  if (!Number.isFinite(value)) return '—'
  return value.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

export function px(value: number) {
  if (!Number.isFinite(value) || value === 0) return '—'
  const digits = value >= 1000 ? 2 : value >= 1 ? 4 : 6
  return value.toLocaleString('zh-CN', { maximumFractionDigits: digits })
}

export function when(iso: string) {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export function actionLabel(action: string) {
  switch (action) {
    case 'open_long':
      return '开多'
    case 'open_short':
      return '开空'
    case 'close_long':
      return '平多'
    case 'close_short':
      return '平空'
    default:
      return '观望'
  }
}

export function sideLabel(side: string) {
  return side === 'short' ? '空' : '多'
}
