# AI 研究数据接入

本次新增六个只读工具：get_indicators、get_trading_history、get_funding、get_open_interest、get_order_flow、get_order_book。独立读取共用最多8并发的既有调度器；交易及保护单仍是顺序屏障。参数和完整中文说明在 Tools 管理中维护。

## 来源与计算

- 原始数据来自当前绑定的 Binance USD-M Futures API；模拟盘使用 demo-fapi.binance.com。
- 公开数据仅在显式 environment=live_reference 时使用 fapi.binance.com，且不携带账户密钥；返回 referenceOnly=true。
- 2026-09-30 实测：模拟盘历史持仓量和主动买卖量统计接口返回普通文本 ok；视为不可用。模拟盘当前持仓量正常，工具返回 partial。实盘公开参考统计可用。
- 统一返回 status、source、fetchedAt、request、data、errors、notes。时间戳除K线time为秒外为毫秒；界面显示UTC。
- EMA按首周期均值初始化；MACD(12,26,9)，histogram=DIF-signal；RSI7、ATR14为Wilder平滑。样本不足为null；指标基于本页初始化，分页不是连续计算状态。
- 收益按明确区间分页读取同合约USDT收入：realized+commission+funding=net。成交是独立最长7天样本；达到limit会标记可能截断，不据此估算全期胜率或回撤。非USDT费用仅在成交中保留原币种，不混加。
- 盘口只含已返回档位，不包含隐藏/RPI流动性。固定数量=金额/mid，逐档计算两个方向的滑点；完整数量不可覆盖时完整订单VWAP和滑点为null。

## 验证方式

Go单元测试使用内存HTTP模拟，不调用模型或订单端点。cmd/analytics-check 只读打开现有SQLite配置，只允许当前模拟账户；所有网络请求均为GET。-reference只把公开OI/order-flow切为显式实盘参考，不切账户。

工具结果在刷新、历史回放时保留完整JSON与全部记录，不再在6000字节处截断新数据。

## 升级与运行状态

升级默认工具说明前逐字比较旧默认版本；只有原版会自动升级并备份为agent_tools_before_analytics，自定义说明不覆盖。引擎恢复时读取已有历史，并保留最近未到期的wait；更新后的用户消息或工具结果使旧等待失效。恢复不会重放历史交易调用。

官方接口依据：Binance binance-connector-python 的 derivatives_trading_usds_futures REST market_data_api.py、trade_api.py 和 account_api.py。本地只读参考保存在engine/data/binance-reference。相关端点为 /fapi/v1/klines、premiumIndex、fundingRate、openInterest、depth、income、userTrades，以及 /futures/data/openInterestHist、takerlongshortRatio。


## 2026-09-30 研究数据源修复

截图对应的调用省略environment，因此旧实现访问模拟盘统计接口，模型未重试公开参考源。现仅get_open_interest/get_order_flow在模拟盘模式下省略environment时默认live_reference；当前值与历史值均读同一实盘公开源，不携带密钥。显式active仍查询模拟盘，其余工具仍保持原环境。source同时包含environment和tradingEnvironment，界面分别显示。不会改变交易账户、持仓或下单客户端。

旧默认工具说明按精确文本迁移，并保存agent_tools_before_research_sources；自定义说明保留。公开参考请求失败仍返回明确的partial/unavailable，不伪造数据。
