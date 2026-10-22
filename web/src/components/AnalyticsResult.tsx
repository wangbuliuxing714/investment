type RecordValue = Record<string, unknown>
const isRecord = (v: unknown): v is RecordValue => !!v && typeof v === 'object' && !Array.isArray(v)
const names: Record<string,string> = {
 indicators:'连续技术指标',current:'当前交易所数据',history:'历史数据',flow:'主动成交方向',book:'盘口快照',income:'实际收益',fills:'成交样本',summary:'收益汇总',rows:'明细',
 from:'起始时间',to:'截止时间',time:'时间',timestamp:'统计时间',closeTime:'收盘时间',closed:'已收盘',fetchedAt:'获取时间',eventTime:'行情事件时间',transactionTime:'撮合时间',fundingTime:'结算时间',nextFundingTime:'下次结算时间',updatedAt:'更新时间',
 close:'收盘价',ema20:'EMA20',macd:'MACD DIF',signal:'MACD 信号',histogram:'MACD 柱',rsi7:'RSI7',atr14:'ATR14',
 count:'返回数量',interval:'周期',calculation:'计算定义',nextEnd:'向前分页 end（秒）',nextStart:'下一页 start（毫秒）',previousEnd:'向前 end（毫秒）',atLimit:'达到单次数量上限',
 symbol:'合约',markPrice:'标记价',indexPrice:'指数价格',lastFundingRate:'最近资金费率（小数）',fundingRate:'历史资金费率（小数）',interestRate:'利率（小数）',openInterest:'未平仓量',sumOpenInterest:'未平仓总量',sumOpenInterestValue:'未平仓名义价值',change:'数量变化',changePercent:'变化率（%）',
 buyVol:'主动买量',sellVol:'主动卖量',buySellRatio:'买卖比',delta:'买卖量差',windowCVD:'本窗口 CVD',imbalance:'成交量不平衡',
 bids:'买盘',asks:'卖盘',price:'价格',quantity:'数量',qty:'成交数量',bestBid:'买一',bestAsk:'卖一',mid:'中间价',spread:'价差',spreadBps:'价差（基点）',depthImbalance:'可见深度不平衡',visibleBidQuantity:'可见买盘数量',visibleAskQuantity:'可见卖盘数量',visibleBidNotional:'可见买盘名义金额',visibleAskNotional:'可见卖盘名义金额',buyEstimate:'买入估算',sellEstimate:'卖出估算',notionalUSD:'估算金额（USDT）',requestedQuantity:'目标数量',filledQuantity:'可成交数量',fullyCovered:'可完整覆盖',filledVWAP:'可成交部分均价',fullOrderVWAP:'完整订单均价',slippageBps:'滑点（基点）',
 asset:'币种',realized:'已实现盈亏',realizedPnl:'本笔已实现盈亏',commission:'手续费',commissionAsset:'手续费币种',funding:'资金费收支',net:'交易净收益',side:'买卖方向',positionSide:'持仓方向',orderId:'订单号',id:'成交编号',scope:'覆盖说明',complete:'完整分页',possiblyTruncated:'可能截断',completeForIncomeWindow:'成交覆盖全部收益期间',
}
const statusNames: Record<string,string> = {ok:'读取成功',partial:'部分可用',unavailable:'数据不可用'}
const environmentNames: Record<string,string> = {demo:'币安模拟盘',live:'币安实盘',live_reference:'实盘公开参考数据'}
export function analyticsHint(text: string): string {
 try {const v=JSON.parse(text);return [v.request?.symbol,v.request?.interval || v.request?.period,statusNames[v.status]].filter(Boolean).join(' · ')}catch{return ''}
}
export function AnalyticsResult({text}:{text:string}) {
 let parsed:unknown
 try{parsed=JSON.parse(text)}catch{return <pre className="detail-text">{text}</pre>}
 if(!isRecord(parsed))return <pre className="detail-text">{text}</pre>
 const source=isRecord(parsed.source)?parsed.source:{}
 const data=isRecord(parsed.data)?parsed.data:{}
 const errors=isRecord(parsed.errors)?parsed.errors:{}
 const notes=Array.isArray(parsed.notes)?parsed.notes:[]
 return <div className="detail-stack analytics-result">
  <strong>{statusNames[String(parsed.status)] || String(parsed.status)}</strong>
  <p className="detail-text">{environmentNames[String(source.environment)] || '数据源未确认'} · {String(source.exchange || '')}<br/>{String(source.host || '')}<br/>获取时间：{String(parsed.fetchedAt || '—')}（UTC）</p>
  {typeof source.tradingEnvironment === 'string' && <p className="detail-text">交易账户环境：{environmentNames[source.tradingEnvironment] || source.tradingEnvironment}</p>}
  {source.referenceOnly===true && <p className="detail-text err">仅供实盘公开行情参考，不是模拟账户成交或模拟盘报价。</p>}
  {Object.entries(errors).map(([key,value])=><p className="detail-text err" key={key}>{names[key] || key}：{String(value)}</p>)}
  {Object.entries(data).map(([key,value])=><section className="detail" key={key}><h3>{names[key] || key}</h3><AnalyticsValue value={value}/></section>)}
  {notes.map((note,i)=><p className="detail-text" key={i}>{String(note)}</p>)}
  <details><summary>查看完整原始结果</summary><pre className="detail-text">{JSON.stringify(parsed,null,2)}</pre></details>
 </div>
}
function AnalyticsValue({value}:{value:unknown}) {
 if(Array.isArray(value)) {
  if(value.length===0)return <p className="detail-text">本次窗口没有记录</p>
  if(value.every(isRecord)) {
   const keys=Array.from(new Set(value.flatMap(row=>Object.keys(row))))
   return <div className="candle-wrap"><table className="candle-table"><thead><tr>{keys.map(key=><th key={key}>{names[key] || key}</th>)}</tr></thead><tbody>{value.map((row,i)=><tr key={i}>{keys.map(key=><td key={key}>{scalar(key,row[key])}</td>)}</tr>)}</tbody></table></div>
  }
  return <pre className="detail-text">{JSON.stringify(value,null,2)}</pre>
 }
 if(isRecord(value)) {
  const simple=Object.entries(value).filter(([,v])=>!isRecord(v)&&!Array.isArray(v))
  const complex=Object.entries(value).filter(([,v])=>isRecord(v)||Array.isArray(v))
  return <div className="detail-stack">{simple.length>0&&<dl className="analytics-fields">{simple.map(([key,v])=><div key={key}><dt>{names[key] || key}</dt><dd>{scalar(key,v)}</dd></div>)}</dl>}{complex.map(([key,v])=><section key={key}><h4>{names[key] || key}</h4><AnalyticsValue value={v}/></section>)}</div>
 }
 return <span>{scalar('',value)}</span>
}
function scalar(key:string,value:unknown):string {
 if(value==null)return '—'
 if(typeof value==='boolean')return value?'是':'否'
 if(['time','timestamp','from','to','closeTime','eventTime','transactionTime','fundingTime','nextFundingTime','updatedAt'].includes(key)){
  const n=Number(value);if(Number.isFinite(n)&&n>0){const d=new Date(n<1e12?n*1000:n);if(!Number.isNaN(d.getTime()))return d.toISOString().replace('T',' ').replace('.000Z',' UTC')}
 }
 return typeof value==='object'?JSON.stringify(value):String(value)
}
