# JCP A股 ETF 支持：指标口径定义与数据契约

> **范围**：**仅 A股 ETF**。美股于 2026-10-30 由用户决定剔除，本文不含美股口径。
> **来源**：基金侧（沈知微）口径定义 + 工程侧实测 + 主理人裁决。
> **配套文档**：`docs/etf/Phase2-口径裁决记录.md`（字段级取舍：保留/降级/替代/剔除，以该文档为准）
> **Go 草案**：`docs/etf/models_etf_draft.go`（可直接拷入 `internal/models/`）
>
> 本文定义的是**口径本身**（公式、单位、语义、来源）；「本期做不做」的裁决在配套文档里。两者互补，不冲突。

---

## 0. 阅读指引

| 层 | 内容 | 适用对象 |
|---|---|---|
| L1 | 跨市场统一抽象（枚举 / 代码格式 / 货币 / 交易日历） | 所有标的 |
| L2 | 通用行情层字段（A股个股与 ETF 共用） | 所有标的 |
| L3 | ETF 专属层 | 场内基金 |

**不可得字段一律写"不可得"，禁止用估算值冒充实测值。**

**「核心必需」列取值含义**：`是` = 本期实现且必需；`否` = 本期可选；`降级` = 实现但精度/频率降级；`剔除` = 本期不做（口径保留在契约中，未来接入数据源后可直接启用）。

---

# 第一部分 · A股 ETF 指标口径

## A1. 交易层（与个股口径一致性）

### 结论：二级市场交易层与个股**完全一致**，无需改造

A股 ETF 二级市场按"股票式撮合交易"，因此 `price / open / high / low / preClose / change / changePercent / volume / amount / 五档盘口` 的口径与现有 `models.Stock`、`models.OrderBook` **完全相同**。

实测确认新浪、腾讯行情接口对 `sh510300 / sh510050 / sz159915` 返回与个股同构的字段布局。主理人亦已交叉验证 TDX 主源（`market_provider_tdx.go:53-125`）与 Sina 兜底源（`market_service.go:244-263`）**取数路径完全不区分资产类型** —— ETF 走同一条路。

> **裁决 D-01 相关**：`GetStockRealTimeData` / `GetKLineData` / `GetOrderBook` 三大主链路**不为 ETF 写任何分支逻辑**。

### ETF 特有的交易层差异（6 项）

| # | 差异项 | 口径 | 工程影响 |
|---|---|---|---|
| 1 | **涨跌幅限制** | ETF 与成分股同规则：主板/创业板 ±10%，科创板/北交所成分 ±20%，**ST/退市整理 ±5%**，债券/货币/商品 ETF ±10% | 需按 `assetType` + 所属板块动态取值，不可写死（裁决 D-13 待实测） |
| 2 | **最小变动价位** | 0.001 元（与个股一致） | 无需改造 |
| 3 | **最小交易单位** | 1 手 = 100 份（与个股一致） | 无需改造 |
| 4 | **回转交易（T+0）** | **境内 A股 ETF = T+1**；跨境 ETF、债券 ETF、黄金 ETF、货币 ETF、商品期货 ETF = T+0 | 需 `t0Tradable bool` 字段，供 AI Agent 与风控提示使用（裁决 D-10） |
| 5 | **申赎机制** | 一级市场按"申赎清单"实物申赎（股票ETF）、现金申赎（跨境/债券/商品）；ETF 申赎门槛高（通常 ≥50万份） | 折溢价套利可行性判断依据 |
| 6 | **IOPV 播报** | 交易所每 **15 秒**播报一次参考单位基金净值 | 折溢价率计算的分母，见 A2 |

> **来源**：证监会北京监管局《ETF基金投资者教育问答》；深交所投教《ETF投资问答》第 44 期。

### 盘口深度的 ETF 特殊性

- 盘口五档结构与个股一致，**但 ETF 的盘口挂单量单位是「份」不是「股」**。上交所/深交所官方接口返回的 ETF 盘口量单位为**份**（1 手 = 100 份），换算逻辑一致，但字段语义需从"股"改为"份"（裁决 3.1 `volume`/`amount` 语义注记）。
- ETF 的盘口常常"单边厚"（做市商集中挂单），`total`/`percent` 的视觉占比计算逻辑可直接复用。
- **盘口深度是 ETF 流动性的第一现场**：规模小/流动性差的 ETF 买一卖一价差可达 0.5% 以上。

---

## A2. 净值层（核心，最易出错）

### 关键结论：**盘中折溢价率的分母是 IOPV，不是前收盘净值，也不是日终净值**

交易所与投教机构的标准定义（两处原文互为等价）：

> 深交所投教《ETF投资问答》第 44 期：
> - 溢价率 = (市场价格 − 实时参考净值) / 实时参考净值
> - 折价率 = (实时参考净值 − 市场价格) / 实时参考净值
>
> 证监会北京监管局《ETF基金投资者教育问答》：
> - 溢价率 = ETF成交价格 / 当前IOPV − 1
> - 示例：市价 3.211、IOPV 3.2090 → 溢价率 +0.06%

两者数学等价：`price / iopv − 1 = (price − iopv) / iopv`。

### ⚠️ 必须区分的两个不同口径（不可混用）

行情软件上常见的"溢价率"多为**日频口径**（收盘价 vs 上一交易日单位净值），与**盘中口径**分母不同、数值不同，必须拆成两个字段：

| 字段 | 口径 | 分母 | 更新频率 |
|---|---|---|---|
| `premiumRate` | **盘中实时溢价率** | IOPV（实时参考净值） | 实时（15s） |
| `closePremiumRate` | **日频收盘溢价率** | 前一交易日单位净值 NAV | 日 |

```
premiumRate      = (price − iopv) / iopv × 100%         ← 盘中，主用
closePremiumRate = (close − nav_prev) / nav_prev × 100%  ← 日频，与软件对账用
```

### IOPV 与日终净值的差异（必须让 AI Agent 理解）

| 维度 | IOPV（盘中参考净值） | 日终单位净值 NAV |
|---|---|---|
| 计算时点 | 交易时段内每 15 秒滚动 | 收盘后一次 |
| 计算方式 | 成分股实时价 × 权重 + 现金 + 申赎项，**估算** | 交易所收盘价 + 申赎清单，**审定** |
| 发布方 | 交易所（上交所/深交所） | 基金管理人 |
| 用途 | 盘中折溢价判断、套利基准 | 业绩计算、赎回价 |
| 跨境/商品 ETF 偏差 | **极大**：海外本土市场休市时，IOPV 只能用海外上一收盘价 + 实时汇率测算 | — |

**实测印证**（2026-10-30 11:02）：
- `sh510300` 市价 4.0720 / IOPV 4.0764 → 盘中溢价率 **−0.108%**
- `sz159915` 市价 2.469 / IOPV 2.469 → 盘中溢价率 **≈ 0.000%**
- `sh513100`（纳指ETF，QDII）市价 2.2480 / IOPV 1.9660 → 盘中溢价率 **+14.34%** ← 与市场公开报道的"跨境 ETF 溢价超 10%"完全吻合，口径验证通过

> **结论：A股 ETF 的 IOPV 与单位净值均可免费获取，且有四条互相可校验的通路 —— 这是本项目 ETF 能力的关键抓手。**

#### IOPV 四源对照（2026-10-30 11:24 同刻实测，值完全一致）

| 源 | 取值位置 | 特点 | 实测值（510300 / 159915 / 513100） |
|---|---|---|---|
| **新浪** `hq.sinajs.cn/list={code}_iopv` | `"日期,时间,IOPV"` | 轻量、响应 ~27ms、**逐标的**（可逗号批量） | 4.0863 / 2.4735 / 1.9660 |
| **腾讯** `qt.gtimg.cn/q={code}` | **field[78]** | **一次请求可批量多只**（推荐主力） | 4.0863 / 2.4735 / 1.9660 |
| 上交所 `yunhq.sse.com.cn:32041/v1/sh1/snap/{code}` | `snap[9]` | 官方权威，沪市 | 4.0764 / — / 1.9660 |
| 深交所 `www.szse.cn/api/market/ssjjhq/getTimeData` | `data.netValue` | 官方权威，深市；**逐标的**接口，批量需并发+缓存；附 `data.piciopv` IOPV 分时序列 | — / 2.469 / — |

> **推荐取数策略**：腾讯 field[78] 批量拿 IOPV（一次 N 只）→ 用新浪 `_iopv` 抽样交叉校验 → 交易所接口做权威兜底。三源同刻值一致（4/4 校验通过），任一源异常都能被另一源发现。
>
> ⚠️ **实测缺口**：上交所 `snap` 的 `nav` 字段返回 `0.000`（不可用），日终净值**不能**从该接口取。

#### 单位净值 / 累计净值 —— 东财 F10 结构化接口（优于解析 JS）

```
GET https://api.fund.eastmoney.com/f10/lsjz?fundCode=510300&pageIndex=1&pageSize=N
（需 Referer: https://fundf10.eastmoney.com/ 与浏览器 UA）
→ {"Data":{"LSJZList":[{"FSRQ":"2026-10-29","DWJZ":"4.1176","LJJZ":"1.8539",
                       "JZZZL":"-1.03","SGZT":"场内买入","SHZT":"场内卖出","FHFCZ":"...","DTYPE":null}]}}
   FSRQ=净值日期  DWJZ=单位净值  LJJZ=累计净值  JZZZL=日增长率(%)  FHFCZ=分红信息
```
- 腾讯 field[81] 与东财 `DWJZ` **6/6 完全一致**（510300=4.1176、159915=2.5341、588000=1.2978、513100=1.9943、513050=0.9445、512880=0.9755）→ 两者可互为校验。
- 分页可得完整净值序列，优于解析 544KB 的 `pingzhongdata` JS。

#### ⚠️ 腾讯 field[77] = 折溢价率（已标定，仅作交叉校验用）

实测 4/4 精确吻合 `(price − iopv) / iopv × 100`：

| ETF | 市价 | IOPV(field78) | 手算折溢价 | field[77] |
|---|---|---|---|---|
| sh510300 | 4.084 | 4.0863 | −0.056% | **−0.06** ✓ |
| sz159915 | 2.475 | 2.4735 | +0.061% | **0.06** ✓ |
| sh513100 | 2.250 | 1.9660 | +14.446% | **14.45** ✓ |
| sh588000 | 1.258 | 1.2569 | +0.088% | **0.09** ✓ |

> **契约口径仍为「自算」**（`premiumRate = (price − iopv)/iopv × 100`），field[77] 仅作**交叉校验哨兵**：两者偏差 > 0.05 个百分点即告警，说明 IOPV 或价格取错了。
> ⚠️ 另有 field[80] 数值（实测 0.25/0.49/0.27/0.40）**语义未标定**，禁止使用。

#### 腾讯字段索引的稳定性与校验锚点

- `qt.gtimg.cn/q=` 同一端点的字段索引**已实测稳定**（两次独立请求，510300 的 field[35] 复合字段位置一致）。
- **但工程侧不要裸写索引**。用**语义锚点**自校验，索引一旦位移会立刻暴露：
  - `field[78]` 应 **等于**新浪 `_iopv` 同刻值（实测 4/4 一致）
  - `field[81]` 应 **等于**东财 `DWJZ`（实测 6/6 一致）
  - `field[77]` 应 **等于** `(field[3] − field[78]) / field[78] × 100`
  - `field[35]` 是自描述复合串 `"价/量(手)/额(元)"`（如 `4.084/4337565/1766205926`），比裸索引更稳

> **裁决 D-01**：IOPV 走**独立数据通路**，不塞进现有行情响应 —— 不同来源、不同刷新节奏、不同失败模式，混在一起会导致"行情整体失败"与"IOPV 取不到"无法区分。
>
> **裁决 D-01 补充（工程侧实测）**：TDX 主源**拿不到** ETF 的 IOPV/净值 —— `protocol.Quote` 结构里对应位置是未解析的保留字节（`ReversedBytes3 int // 保留，未知,基金的昨收净值?`），通达信协议库未暴露该字段。**这从反面印证了 IOPV 必须走独立通路的决策**，且 ETF 的 IOPV 只能走新浪/腾讯/交易所。

---

## A3. 产品层

| 字段 | 口径定义 | 来源（实测可得性） | 频率 |
|---|---|---|---|
| 基金规模 `fundScale` | 基金**净资产**总额（元）= 基金份额 × **单位净值** | ✅ 东财 push2 `f116`（⚠️ 服务器 IP 风控，D-06）；✅ pingzhongdata `Data_fluctuationScale`（亿元+环比）；✅ pingzhongdata `Data_assetAllocation.净资产` | **季**（`Data_fluctuationScale` 仅季度点） |
| 基金份额 `shareCount` | 基金份额总数（份） | ✅ 腾讯 field[72]/[73]/[76]（实测 37475587700，与东财 `f84`=37475587584 差 116 份）；✅ 东财 push2 `f84`；✅ pingzhongdata `Data_buySedemption.总份额` | 最近一次披露值；**日频更新与否待观察** |
| 成立日 `inceptionDate` | 基金合同生效日 | ✅ 东财 push2 `f189`（实测 510300 = 20120528 ✅） | 静态 |
| 跟踪指数 | 标的指数代码 + 名称 | ⚠️ pingzhongdata **不含**跟踪指数；需东财 F10 基金接口或人工维护（裁决 D-07） | 静态 |
| 管理人 `managerName` | 基金管理公司 | ⚠️ pingzhongdata 不含公司名；含基金经理 `Data_currentFundManager[].name` | 季 |
| 管理费 `managementFee` | 年费率 % | ❌ 免费源**不可得**（pingzhongdata 的 `fund_Rate` 是**申购费率**，不是管理费）→ 须读基金合同/定期报告 | 静态 |
| 托管费 `custodyFee` | 年费率 % | ❌ **不可得**，同上 | 静态 |
| 跟踪误差 `trackingError` | 年化，见 A4 | ❌ 免费源**无原始数值**，需自行计算（见 A4）或读定期报告 | 月/季 |
| 折溢价率历史分位 `premiumRatePercentile` | 近 N 个交易日收盘溢价率的分位数 | ⚠️ 需自行积累日频序列后计算（冷启动期为空值） | 日 |
| 复制策略 | 完全复制 / 抽样复制 | ❌ **不可得** | 静态 |

> ### ⚠️ 「规模」与「份额」的口径陷阱（实测澄清）
>
> 新浪 ETF 全列表（§A6）提供 `mktcap` 与 `nmc`，但**都不是净资产**：
> - `nmc` = 总份额 × **市价**（实测 510300：nmc=15335010.49 万元 ÷ 10000 ÷ 4.092 = 37,475,587,700 份 ✓ 精确吻合）
> - 即 `nmc` 是**市值**口径，不是**净值**口径
> - **净资产（真实基金规模）= 份额 × 单位净值**，须用 `DWJZ` 而非市价计算
> - 腾讯 field[44]/[45] 同为亿元口径（实测 510300 = 1530.50 亿元 = 37475587700 × 4.084 / 1e8 ✓）
>
> **结论：不要把 `nmc`/`mktcap` 当作 `fundScale` 直接落库**，否则规模会随市价漂移而非反映真实净值规模。真实净资产仍受东财 push2 风控（D-06）影响，或用季度 `Data_fluctuationScale`。
>
> **规模频率按季度**：免费源里只有 `Data_fluctuationScale` 提供带环比的规模序列，且**仅 5 个季度点**（实测 2025-09-30 → 2026-09-30）。**本期不定义日频规模指标。**

**免费源能拿到的 ETF 产品信息实测清单**（东财 `fund.eastmoney.com/pingzhongdata/{code}.js`）：
`fS_name, fS_code, fund_sourceRate, fund_Rate, fund_minsg, stockCodes(前十大重仓代码), stockCodesNew, zqCodes, zqCodesNew, syl_1n/syl_6y/syl_3y/syl_1y, Data_netWorthTrend(净值序列+日增长率+每份派发), Data_ACWorthTrend, Data_grandTotal, Data_rateInSimilarType(同类排名), Data_rateInSimilarPersent, Data_fluctuationScale(规模变动), Data_holderStructure(机构/个人/内部持有比例), Data_assetAllocation(股票/债券/现金占净比+净资产), Data_performanceEvaluation(五维评分), Data_currentFundManager(基金经理+任职回报), Data_buySedemption(期间申购/赎回/总份额), Data_fundSharesPositions(股票仓位)`

> ### 🚨 重大口径陷阱
> `Data_performanceEvaluation` 的维度名包含"**跟踪误差**"，但它的取值是 **0–100 的评分**（实测 510300 跟踪误差评分 = 90 分），**不是年化跟踪误差百分比**。
> **禁止**把它当作 `trackingError` 填入契约。同理 `Data_currentFundManager[].power.categories` 里的"跟踪误差"也是评分。
> 真正的跟踪误差必须按 A4 公式计算。**（裁决 D-08：必须自算，禁用东财评分字段）**

> ### 🚨 重大口径陷阱（补充实测）：`stockCodes` 末尾数字是**市场标志**，不是权重
>
> 形如 `"3003080"` / `"6005191"` 的末尾一位，**不是持仓权重** —— 它是市场标志：**0 = 深市、1 = 沪市**。
>
> **验证**（510300 沪深300ETF）：
> `stockCodes = ["3003080","6005191","3007500","6013181","3005020","6000361","6018991","6032591","0003330","6009001"]`
> - 深市（300308/300750/300502/000333）→ 末尾全为 `0`
> - 沪市（600519/601318/600036/601899/603259/600900）→ 末尾全为 `1`
> **10/10 完全吻合市场标志，零例外。**
>
> **独立验证**（159915 创业板ETF）：`stockCodes = ["3007500","3003080","3005020","3000590","3003940","3004080","3004760","3002740","3006040","3001240"]` —— 10 只全是深市，末尾全为 `0` ✓
>
> 且 `pingzhongdata` 全部 27 个变量中**不存在任何权重变量**（已逐个枚举确认）。
> → **裁决 D-09 结论成立：成分股权重不可得。** `constituentWeights[]` / `industryWeights[]` / `top10Weight` 维持「剔除」。
> `stockCodesNew` 的 `"0.300308"` 形式已显式带市场号，末尾标志位是冗余的。

---

## A6. ETF 标的识别与检索（裁决 D-12 已解决）

### A6.1 ETF 全列表（1715 只）

```
GET https://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodeData
    ?page=1&num=200&sort=symbol&asc=1&node=etf_hq_fund&symbol=&_s_r_a=page
（需 Referer: https://vip.stock.finance.sina.com.cn/ 与浏览器 UA）
```
返回字段（实测 510300）：
```json
{"symbol":"sh510300","code":"510300","name":"沪深300ETF华泰柏瑞",
 "trade":"4.092","pricechange":-0.019,"changepercent":-0.46,
 "buy":"4.091","sell":"4.092","settlement":"4.111",   ← settlement = 前收盘
 "open":"4.059","high":"4.095","low":"4.046",
 "volume":441118218,"amount":1796300363,               ← volume 单位 = 份
 "ticktime":"11:25:10",
 "per":0,"pb":0,                                        ← ETF 恒为 0，与契约一致
 "mktcap":9869735.28,"nmc":15335010.49,                ← 万元；见 A3 口径陷阱
 "turnoverratio":1.17708}                              ← 换手率 %
```

**用途与口径**：
| 字段 | 契约字段 | 说明 |
|---|---|---|
| `symbol` | `symbol` | 带市场前缀，与现有 `sh/sz/bj` 体系一致，**零迁移** |
| `trade` | `price` | 最新价 |
| `settlement` | `preClose` | 前收盘价 |
| `volume` | `volume` | 单位 **份**（非股） |
| `amount` | `amount` | 元 |
| `turnoverratio` | `turnoverRate` | **换手率 = 成交额/规模 × 100**（实测 1796300363 / 153350104868.4 = 1.171% ≈ 1.177 ✓）。**裁决 D-11 的 ETF 换手率由此解决** |
| `per` / `pb` | `peTtm` / `pb` | ETF 恒为 0 → 与契约「固定 0 并 UI 隐藏」一致 |
| `mktcap` / `nmc` | ⚠️ **不要**映射到 `fundScale` | 见 A3 口径陷阱（市值口径非净值口径） |

### A6.2 标的类型识别

```
GET https://smartbox.gtimg.cn/s3/?q=510300&t=all
→ v_hint="sh~510300~沪深300ETF华泰柏瑞~hs300etfhtbr~ETF"
                                     ↑ 第5段 = 类型标签（ETF / LOF / 指数 / 普通股 …）
```
用于 `assetType` 与 `securityType` 的判定，与深交所 `data.groupId`、新浪 ETF 列表**三源交叉**。

### A6.3 推荐的 `assetType` / `securityType` 判定优先级

| 目标 | 判定来源（按优先级） | 备注 |
|---|---|---|
| `assetType = etf` | ① 新浪 `etf_hq_fund` 节点列表命中 → `etf`；② 腾讯 smartbox 第5段含 `ETF`/`LOF`；③ 深交所 `data.groupId == 8` | 三源任一命中即可 |
| `securityType`（跨境/债券/商品） | ① 腾讯 smartbox 类型标签细分；② pingzhongdata 基金类型字段；③ 名称/代码段兜底（`sh513xxx`/`sh5131xx`/`sz159xxx` 跨境类） | **具体规则待工程侧实测后写死**（裁决 D-10），不靠猜测 |
| `t0Tradable` | 由 `securityType` 推导：境内股票ETF=false；跨境/债券/黄金/货币/商品ETF=true | 裁决 D-10 |

---

## A4. 跟踪误差 / 跟踪偏离度（口径必须写死）

### A4.1 行业标准阈值（基金合同约定）

| 产品类型 | 日均跟踪偏离度绝对值 | 年化跟踪误差 | 来源 |
|---|---|---|---|
| A股股票 ETF | ≤ **0.2%** | ≤ **2%** | 深交所投教《ETF投资者问答》第15期 / 中证网 |
| 普通开放式指数基金 / ETF联接 | 更宽（仓位≤95%） | 更宽 | 同上 |
| QDII ETF / 指数增强 ETF | 适度放宽 | 适度放宽 | 同上 |
| 境外指数 LOF（实例） | ≤ 0.5% | ≤ 6%（美元计价） | 华安标普全球石油LOF 招募说明书 |
| 指数增强基金 | — | 7.75%–8% | 公开投教口径 |

### A4.2 计算公式（必须与招募说明书一致）

```
① 跟踪偏离度（日）
   TD_t = R_P,t − R_B,t
   R_P,t = 基金净值日增长率 = (NAV_t − NAV_{t−1}) / NAV_{t−1}
   R_B,t = 业绩比较基准日收益率 = (Index_t − Index_{t−1}) / Index_{t−1}

② 日均跟踪偏离度绝对值
   annualTrackingDeviation = (1/n) × Σ_{t=1..n} |TD_t|

③ 年化跟踪误差
   trackingError = sqrt( Σ_{t=1..n} (TD_t − TD̄)² / (n−1) ) × sqrt(250)
   其中 TD̄ = (1/n) × Σ TD_t，年化因子 250（A股年交易日数）
```

**口径选择说明（工程侧必须遵守）**：采用 **减均值的样本标准差**版本，与海通证券《公募基金的工具属性评级方法(2020)》（中国基金业协会发布）及 Morningstar ETF Index Tracking Analysis 方法论一致。
部分招募说明书写作 `sqrt(Σ TD_t²/(n−1)) × sqrt(250)`（**不减均值**）。两者数值略有差异，**本契约统一采用减均值版本**，并在字段注释中写明"样本标准差口径（减均值），年化因子250"。

> **裁决 D-08**：本字段**必须自算**，严禁使用东财 `Data_performanceEvaluation` 的「跟踪误差」评分字段（语义完全不同的两个东西占同一个字段名，是最危险的坑 —— 填进去会让看板显示「跟踪误差 90%」这种荒谬数字）。
>
> ⚠️ **开放依赖**：业绩基准日收益率 `R_B,t` 的免费来源待工程侧确认（D-08 副裁决）。若不可得，`trackingError` 本期降级为不可得。
>
> ⚠️ **分红处理**：必须用**累计净值或分红再投资调整** —— 东财 `Data_netWorthTrend` 的 `unitMoney` 记录了每份派发，直接用单位净值算会把分红当亏损，**系统性高估跟踪误差**。

### A4.3 自算所需数据

| 数据 | 来源 | 状态 |
|---|---|---|
| 基金净值日序列（含日增长率 `JZZZL`、每份派发 `FHFCZ`） | 东财 `api.fund.eastmoney.com/f10/lsjz` 分页（**推荐**）；或 pingzhongdata `Data_netWorthTrend` | ✅ 可得 |
| 基金累计净值序列 | 同上 `LJJZ`；或 pingzhongdata `Data_ACWorthTrend` | ✅ 可得 |
| 业绩基准指数日序列 | 东财/腾讯指数日 K（`sh000300` 沪深300、`sh000016` 上证50、`sz399006` 创业板指） | ⏳ D-08 副裁决待工程侧确认 |
| 年化因子 | 固定 250 | ✅ |

> ### ⚠️ 净值序列的两个坑（比"分红"更隐蔽）
>
> **坑 1：分红会下调单位净值。** 直接用单位净值算收益率会把分红当亏损，**系统性高估跟踪误差**。必须用**累计净值**或做分红再投资调整。
>
> **坑 2：份额折算（拆分）会让净值序列跳变。** 实测 510300 的 `Data_netWorthTrend` 早期存在：
> `{"x":1336665600000,"y":2.637,"equityReturn":-2.8606,"unitMoney":"拆分：每份基金份额折算份..."}`
> —— 净值从 1.007 跳到 2.637，同时日增长率 −2.86%。若直接对净值序列取对数收益，会把这个**拆分跳变**当成 −2.86% 的真实亏损，**直接污染跟踪误差**。
>
> **正确做法**：
> 1. 优先用**累计净值**序列（`LJJZ` / `Data_ACWorthTrend`），它已消化分红与拆分
> 2. 若必须用单位净值，**按 `unitMoney` / `FHFCZ` 标记筛出分红日与拆分日并剔除或做复权调整**
> 3. 自算时先过滤掉发生拆分/分红的异常日，再算标准差
>
> **口径要求**：`trackingError` 与 `annualTrackingDeviation` 的计算**必须声明净值序列口径**（累计净值 / 复权单位净值），并写入 `trackingErrorWindow` 字段。

> **裁决 D-08**：本字段**必须自算**，严禁使用东财 `Data_performanceEvaluation` 的「跟踪误差」评分字段（语义完全不同的两个东西占同一个字段名，是最危险的坑 —— 填进去会让看板显示「跟踪误差 90%」这种荒谬数字）。
>
> ⚠️ **开放依赖**：业绩基准日收益率 `R_B,t` 的免费来源待工程侧确认（D-08 副裁决）。若不可得，`trackingError` 本期降级为不可得。

---

## A5. 持仓层

| 字段 | 口径 | 可得性 |
|---|---|---|
| 成分股只数 `holdingsCount` | 定期报告披露的股票持仓只数 | ⚠️ 需东财 F10 持仓接口 / 定期报告（裁决 D-09 待实测） |
| 成分股权重 `constituentWeights[]` | 定期报告披露的个股权重 | ❌ pingzhongdata 只有前十大**代码**，**无权重** → 本期剔除（裁决 D-09） |
| 前十大集中度 `top10Weight` | Σ 前十大权重 | ⚠️ 依赖上一项 → 降级 |
| 行业分布 `industryWeights[]` | 定期报告行业配置 | ❌ 需 F10 → 本期剔除 |
| 股票仓位 `equityPosition` | 股票占净值比 % | ✅ pingzhongdata `Data_fundSharesPositions` 末值 |

**ETF 持仓的特殊风险点**：
- 成分股只数少（如北证50）→ 可完全复制，跟踪误差可控；成分股多（如沪深300）→ 多用抽样复制
- 指数调样日会产生**被动买卖冲击**，放大跟踪误差
- 成分股停牌/摘牌/临时纳入 → 跟踪误差跳升

---

# 第二部分 · 跨市场统一抽象（本期仅 A股，预留扩展位）

> **设计意图**：本期只做 A股 ETF，但枚举与字段按"市场"与"资产类型"两个正交维度建模 —— 这样未来加新市场时，只需新增 `Market` 值，不需要改动 `AssetType` 与任何字段语义。

## C1. 标的类型枚举设计

### 核心原则：把"市场"和"资产类型"拆成两个正交维度

理由：交易时段/货币/节假日由 `market` 决定；有无净值/折溢价/跟踪误差由 `assetType` 决定。合成一个枚举（如 `stock_etf`）会导致组合爆炸，且每次新增市场都要改枚举与所有 switch。

### Go 定义（照抄，裁决 D-02）

```go
// Market 市场
type Market string

const (
    MarketCN Market = "CN" // 中国内地（沪/深/北）—— 本期唯一取值
    // 未来扩展预留：MarketUS 等，届时同步补齐 §C4 交易日历
)

// AssetType 资产类型
type AssetType string

const (
    AssetTypeStock AssetType = "stock" // 个股
    AssetTypeETF   AssetType = "etf"   // 场内交易基金（ETF/LOF）
    AssetTypeIndex AssetType = "index" // 指数（对应现有 models.MarketIndex，本期 ETF 场景不使用，预留）
)

// SecurityType 证券细分类（A股）
type SecurityType string

const (
    SecTypeCNMain     SecurityType = "cn_main"      // 主板
    SecTypeCNGEM      SecurityType = "cn_gem"       // 创业板
    SecTypeCNSTAR     SecurityType = "cn_star"      // 科创板
    SecTypeCNBSE      SecurityType = "cn_bse"       // 北交所
    SecTypeCNFundLOF  SecurityType = "cn_fund_lof"  // LOF
    SecTypeCNFundQDII SecurityType = "cn_fund_qdii" // QDII ETF（跨境）
    SecTypeCNFundBond SecurityType = "cn_fund_bond" // 债券/货币/商品 ETF
)
```

> `AssetTypeIndex` 为预留位：JCP 现有 `models.MarketIndex` 走的是指数口径，与 `Quote` 不同构，本期不合并，但枚举值先留好，避免未来加指数时破坏 `AssetType` 语义。

### 自动识别映射（免费源字段 → 枚举）

| 源字段 | 映射规则 |
|---|---|
| 深交所 `data.groupId` | `8` = ETF（实测 159915 groupId=8） |
| 东财 `f107` / `f110` | `1`=沪, `0`=深, `51`=深个股 → 交易所；结合代码段判 ETF |
| 代码段 | `sh5xxxxx` / `sz15xxxx` / `sh51xxxx` 等 → ETF 候选（须与 `groupId`/F10 交叉确认） |
| 名称后缀 | 名称含「ETF」/「LOF」/「联接」→ 辅助判定，**不单独作为依据** |

> **裁决 D-10**：`t0Tradable` 判定来源优先级 —— ① 深交所 `data.groupId` → ② 东财 pingzhongdata 基金类型字段 → ③ 名称/代码段规则兜底。**具体规则待工程侧实测确认后写死，不靠猜测。**

---

## C2. 代码格式统一

### 本期方案：A股现状**完全不变**

| 市场 | 格式 | 示例 | 备注 |
|---|---|---|---|
| A股个股 | `sh`/`sz`/`bj` + 6位数字 | `sh600000`、`sz000001`、`bj430047` | 零迁移 |
| A股 ETF | 同上（ETF 与个股共用代码空间） | `sh510300`、`sh510050`、`sz159915`、`sh513100` | 无需特殊前缀 |

### 本期**不引入**新市场的代码格式

美股代码格式（`us` + 大写 ticker 等）随美股一并剔除，本期不实现。
若未来恢复，`us` 前缀需与指数用的 `us.` 点号前缀区分，且 `us` 与 `sh/sz/bj` 同为小写字母前缀，需确认解析无歧义（已知前缀直接剥离，不做搜索）。

### 三条铁律（**对 ETF 同样成立，必须写进代码注释**）

1. **`symbol` 是不透明 ID，前端永不解析它。** 所有语义由 `market` + `assetType` + `currency` + `securityType` 字段承载。
2. **symbol 解析只允许发生在一处**（`SymbolResolver`），输入 `(market, code)` 或 `(symbol)` → 输出 `ResolvedSymbol{Market, Code, AssetType, SecurityType, Exchange, Currency}`。各数据源适配器只消费 `ResolvedSymbol`。
3. **禁止用 symbol 前缀推断业务逻辑**（如 `strings.HasPrefix(code,"sh")` 来决定显示单位或时段）—— 一律读显式字段。

> **裁决 D-02 配套铁律**，前端不解析 symbol：显示单位/时段由 `market`/`assetType`/`securityType` 决定。

---

## C3. 货币字段

### 决策（裁决 D-03）：**本期恒为 CNY，但字段先建**

`models.Stock` 当前**完全没有货币字段**。虽然美股已剔除，但该字段是**防止未来踩坑的前置投资** —— 一旦等到真要加新市场时再加，`models.Stock` 的所有消费方（含 `mergeRealtimeStock` 白名单、前端 `types.ts`）都要重新过一遍。**现在加成本最低**（前端仅 3 处消费：`types.ts:9`、`StockList.tsx:86`、`WelcomePage.tsx:68`）。

### 规则

1. `models.Stock` 新增 `currency Currency` 字段，本期所有 A股标的恒为 `"CNY"`。
2. 所有价格/金额字段一律使用**标的本币计价**，不做任何隐式换算。
3. **无量纲指标**（涨跌幅、折溢价率、跟踪误差、换手率、持股比例）天然与货币无关，跨品种可直接横比。
4. **金额类字段聚合时必须带 currency**，禁止不同币种直接相加（本期不触发，但规则先立）。
5. **汇率不是行情域的字段**，不放入 `Stock`。如需折算，另建 `FxRate{Market, Base, Quote, Rate, AsOf}`。跨境 ETF 的汇率风险在**风险提示**中体现（见 `ETF_FX_RISK`）。
6. `marketCap` 由 `string` 改为 `float64`（单位=货币元），并新增 `marketCapDisplay string`（如 "3.92万亿"）供前端直接渲染（裁决 D-04）—— **破坏性变更**，需同步改 `frontend/src/types.ts` 与 `app.go` 的 `mergeRealtimeStock`（该函数是**白名单式手工赋值**，逐字段 copy，漏字段会静默丢数据）。

### 新增 `models.Stock` 字段（增量）

```go
Market       Market       `json:"market"`       // CN（本期唯一值）
AssetType    AssetType    `json:"assetType"`    // stock / etf
SecurityType SecurityType `json:"securityType"` // cn_main / cn_fund_qdii / ...
Currency     Currency     `json:"currency"`     // CNY
Exchange     string       `json:"exchange"`     // SSE / SZSE / BSE
QuoteTime    string       `json:"quoteTime"`    // RFC3339 带时区偏移（D-05）
TradePhase   TradePhase   `json:"tradePhase"`   // 交易时段枚举
DataSource   string       `json:"dataSource"`   // 数据来源标识
DataDelayed  bool         `json:"dataDelayed"`  // 是否为延迟行情
```

---

## C4. 交易日历与市场状态

### 接口（现有逻辑搬家 + 预留扩展）

```go
type TradingCalendar interface {
    Market() Market
    IsTradeDay(date time.Time) (bool, string)   // 返回是否交易日 + 节假日名称
    GetMarketStatus(now time.Time) MarketStatus
    GetTradingSchedule() TradingSchedule
    GetTradeDates(days int) ([]string, error)
    // 扩展位：未来加新市场时补充
    // IsEarlyClose(date time.Time) bool
    // LocalTime(now time.Time) time.Time
}
```

- 现有 `MarketService.isTradeDay` / `GetMarketStatus` / `GetTradingSchedule` / `GetTradeDates` → 抽到 `CNTradingCalendar`（**逻辑不变，只是搬家**）。
- 现有 A股时段判定规则**保持不变**：盘前 00:00–09:15 / 集合竞价 09:15–09:30 / 交易中 09:30–11:30 / 午间休市 11:30–13:00 / 交易中 13:00–15:00 / 已收盘 15:00–24:00。

### A股节假日

沿用现有逻辑：非周末 且 非节假日（调休上班**也不算**交易日）。节假日数据来源与缓存机制不变。

### 时间戳规范（裁决 D-05）

**落库与传输的时间戳一律 RFC3339 带偏移**（如 `2026-10-30T09:31:00+08:00`），**禁止裸 `"2026-10-30 09:31:00"`**。

理由：现有 `KLineData.Time` 存的是日期串（`2026-10-30`），日线尚可；但 ETF 的 IOPV 分时序列若存裸时间，会在时区场景下错乱。A股无夏令时，偏移固定 `+08:00`，风险可控，但**现在立规范成本几乎为零**，后期改要动所有历史数据。

### 交易时段枚举 `TradePhase`

`pre_market`（盘前）/ `auction`（集合竞价）/ `trading`（交易中）/ `lunch_break`（午间休市）/ `closed`（已收盘、休市）

> 本期不引入 `after_hours` / `early_close`（延长交易时段概念，非 A股 交易制度）。
> A股**无夏令时**，因此不引入 `time/tzdata` 依赖 —— 用 `time.FixedZone("CST", 8*60*60)` 即可，避免精简 Linux 镜像缺 `/usr/share/zoneinfo` 的静默失败风险。

---

## C5. 通用指标 vs ETF 特有指标

### A股个股与 ETF 共用（字段名统一，前端复用同一套渲染逻辑）

`price, preClose, open, high, low, change, changePercent, amplitude, volume, amount, avgPrice, turnoverRate, volumeRatio, totalShares, floatShares, marketCap, floatMarketCap, marketCapDisplay, peTtm, pb, quoteTime, tradePhase, dataSource, dataDelayed, orderBook{bids,asks}, kline{time,open,high,low,close,volume,amount,avg}`

> ⚠️ **语义注记**：`volume`/`amount` 对个股是「股/元」，对 ETF 是「**份**/元」；`totalShares`/`floatShares` 对个股是「股本」，对 ETF 是「**份额**」。数值口径不变，**显示文案需区分**（裁决 3.1）。

### A股特有（个股侧，ETF 不适用）

`limitUpPrice, limitDownPrice`（涨跌停价）、`mainNetInflow`（主力资金流）、`longHuBang`（龙虎榜）、`sectorTags`、`coreThemes`（题材）、`marginBalance`（两融）、`restrictedRelease`（解禁）、`pledgeRatio`（质押率）、`holderCount`（股东户数）

### ETF 特有

`iopv, nav, accNav, navDate, premiumRate, closePremiumRate, premiumRatePercentile, premiumRateHistory, iopvHistory, fundScale, shareCount, inceptionDate, trackingIndexCode, trackingIndexName, managerName, fundManagerName, managementFee, custodyFee, replicationStrategy, trackingError, annualTrackingDeviation, trackingErrorWindow, trackingErrorStdType, holdingsCount, top10Weight, equityPosition, constituentWeights[], industryWeights[], t0Tradable`

### 指数特有

`indexCode, indexName, indexBase, constituentCount, indexWeightTop10`（对应现有 `models.MarketIndex`，本期不合并进 `Quote`）

---

# 第三部分 · 数据契约字段清单

> Go 类型 / JSON tag 直接照抄。`核心必需` 列取值：`是` / `否` / `降级` / `剔除`（口径保留在契约中，本期不实现，未来接入数据源即可启用）。
> 字段级取舍的完整裁决见 `docs/etf/Phase2-口径裁决记录.md` §三。

## 4.1 通用行情层 `models.Quote`

| 字段名 | Go 类型 | JSON tag | 含义 | 单位 | 计算公式/来源 | 更新频率 | 核心必需 |
|---|---|---|---|---|---|---|---|
| symbol | string | symbol | 标的唯一标识 | - | 源系统透传 | - | 是 |
| name | string | name | 证券名称 | - | 源 | 实时 | 是 |
| market | Market | market | 所属市场 | 枚举 | 派生（本期恒为 CN） | - | 是 |
| assetType | AssetType | assetType | 资产类型 | 枚举 | stock/etf/index | - | 是 |
| securityType | SecurityType | securityType | 证券细分类 | 枚举 | 见 C1 映射表 | - | 是 |
| exchange | string | exchange | 上市交易所 | - | SSE/SZSE/BSE | - | 是 |
| currency | Currency | currency | 计价货币 ISO-4217 | - | 本期恒为 CNY（D-03） | - | 是 |
| price | float64 | price | 最新价 | 货币单位 | 源 | 实时 | 是 |
| preClose | float64 | preClose | 前收盘价 | 货币单位 | 源 | 实时 | 是 |
| open | float64 | open | 开盘价 | 货币单位 | 源 | 实时 | 是 |
| high | float64 | high | 最高价 | 货币单位 | 源 | 实时 | 是 |
| low | float64 | low | 最低价 | 货币单位 | 源 | 实时 | 是 |
| change | float64 | change | 涨跌额 | 货币单位 | price − preClose | 实时 | 是 |
| changePercent | float64 | changePercent | 涨跌幅 | % | (price − preClose)/preClose×100 | 实时 | 是 |
| amplitude | float64 | amplitude | 振幅 | % | (high − low)/preClose×100 | 实时 | 否 |
| volume | int64 | volume | 成交量 | **股**(个股) / **份**(ETF) | 源；A股源常为手，需归一并标注语义 | 实时 | 是 |
| amount | float64 | amount | 成交额 | 货币单位 | 源 | 实时 | 是 |
| avgPrice | float64 | avgPrice | 均价 | 货币单位 | amount / volume；腾讯 field[35] 复合串 "价/量(手)/额(元)" | 实时 | 降级（D-11） |
| turnoverRate | float64 | turnoverRate | 换手率 | % | 个股：成交量/流通股本×100。**ETF：成交额/规模×100**（新浪 ETF 列表 `turnoverratio`，实测 1.177% ✓） | 实时 | **是**（ETF 口径已由实测确定，D-11 部分解决） |
| volumeRatio | float64 | volumeRatio | 量比 | 倍 | 源 | 实时 | 降级（D-11） |
| bidVolume | int64 | bidVolume | 委买总量 | 股/份 | 源 | 实时 | 否 |
| askVolume | int64 | askVolume | 委卖总量 | 股/份 | 源 | 实时 | 否 |
| totalShares | float64 | totalShares | 总股本(个股) / 总份额(ETF) | 股/份 | 源（ETF 来源 `f84`） | 日 | 降级（D-06） |
| floatShares | float64 | floatShares | 流通股本/流通份额 | 股/份 | 源 | 日 | 降级（D-06） |
| marketCap | float64 | marketCap | 总市值 | 货币单位 | price × totalShares | 实时 | 是（D-04，改 float64） |
| floatMarketCap | float64 | floatMarketCap | 流通市值 | 货币单位 | price × floatShares | 实时 | 否 |
| marketCapDisplay | string | marketCapDisplay | 市值可读文案 | - | 格式化（"3.92万亿"） | 实时 | 是（D-04 新增） |
| peTtm | float64 | peTtm | 市盈率TTM | 倍 | price / EPS(TTM)。**ETF 无此语义，固定 0 并在 UI 隐藏** | 日 | 降级 |
| pb | float64 | pb | 市净率 | 倍 | price / BVPS。**ETF 无此语义，固定 0 并在 UI 隐藏** | 日 | 降级 |
| ps | float64 | ps | 市销率 | 倍 | marketCap / TTM营收 | 日 | 剔除（ETF 不适用） |
| epsTtm | float64 | epsTtm | 每股收益TTM | 货币/股 | 源 | 日 | 剔除（ETF 不适用） |
| bvps | float64 | bvps | 每股净资产 | 货币/股 | 源 | 日 | 剔除（ETF 不适用） |
| dividendYield | float64 | dividendYield | 股息率 | % | 年化每股分红/price×100 | 日 | 剔除（免费源不可得） |
| high52Week | float64 | high52Week | 52周最高 | 货币单位 | 源 | 日 | 剔除（免费源不可得） |
| low52Week | float64 | low52Week | 52周最低 | 货币单位 | 源 | 日 | 剔除（免费源不可得） |
| quoteTime | string | quoteTime | 行情时间戳 | RFC3339 带偏移 | 源；**强制带时区偏移**（D-05） | 实时 | 是 |
| tradePhase | TradePhase | tradePhase | 交易时段 | 枚举 | pre/auction/trading/lunch_break/closed | 实时 | 是 |
| dataSource | string | dataSource | 数据来源标识 | - | tdx/sina/sse/szse/eastmoney | - | 是 |
| dataDelayed | bool | dataDelayed | 是否为延迟行情 | bool | 源标记 | 实时 | 是 |

> 已剔除字段（延长交易时段与境外市场概念，本期不做）：`session`（盘前盘后）、`afterHoursPrice`、`afterHoursChangePercent`、`preMarketPrice`、`season`、`exchangeSuffix`、`englishName`、`adrRatio`、`isAdr`、`nextEarningsDate`、`fiscalYearEnd`、`shortFloat`、`isEarlyClose`。

## 4.2 ETF 专属层 `models.ETFProfile`

| 字段名 | Go 类型 | JSON tag | 含义 | 单位 | 计算公式/来源 | 更新频率 | 核心必需 |
|---|---|---|---|---|---|---|---|
| iopv | float64 | iopv | 盘中参考净值(IOPV) | 货币/份 | **腾讯 field[78]（可批量，推荐）** / 新浪 `{code}_iopv` / 上交所 `snap[9]` / 深交所 `data.netValue`（四源实测同刻一致，见 A2） | 实时(秒级~15s) | **是** |
| nav | float64 | nav | 前一交易日单位净值 | 货币/份 | 东财 `api.fund.eastmoney.com/f10/lsjz` 的 `DWJZ`；腾讯 field[81] 可校验 | 日 | **是** |
| accNav | float64 | accNav | 累计单位净值 | 货币/份 | 东财 lsjz 的 `LJJZ`；或 `Data_ACWorthTrend` 末值 | 日 | **是**（跟踪误差必须用它；并须剔除拆分日，见 A4.3） |
| navDate | string | navDate | 净值对应日期 | YYYY-MM-DD | 源 | 日 | **是** |
| premiumRate | float64 | premiumRate | **盘中折溢价率** | % | **(price − iopv)/iopv×100** | 实时 | **是** |
| closePremiumRate | float64 | closePremiumRate | 日频收盘折溢价率 | % | (close − nav)/nav×100 | 日 | **是** |
| premiumRatePercentile | float64 | premiumRatePercentile | 折溢价率历史分位 | % | 近N交易日 closePremiumRate 分位 | 日 | 降级（冷启动，先给空值+说明） |
| premiumRateHistory | []PremiumRatePoint | premiumRateHistory | 折溢价率历史序列 | - | 自积累日频 | 日 | 降级 |
| iopvHistory | []IOPVPoint | iopvHistory | IOPV分时序列 | 货币/份 | 深交所 `data.piciopv` | 实时 | 降级（量有限） |
| fundScale | float64 | fundScale | 基金规模(净资产) | 货币单位 | 东财 `f116`（⚠️风控 D-06）/ pingzhongdata `Data_fluctuationScale`（亿元+环比）。**勿用新浪 `nmc`/`mktcap`（市值口径）** | **季** | 降级（D-06） |
| shareCount | float64 | shareCount | 基金份额总数 | 份 | 腾讯 field[72]/[73]/[76]；东财 `f84`（实测两者差 116 份） | 最近一次披露值（日频更新与否待观察） | 降级（D-06） |
| inceptionDate | string | inceptionDate | 基金成立日 | YYYY-MM-DD | 东财 `f189` | 静态 | 是 |
| trackingIndexCode | string | trackingIndexCode | 跟踪指数代码 | - | 基金合同/F10 | 静态 | 降级（D-07） |
| trackingIndexName | string | trackingIndexName | 跟踪指数名称 | - | 基金合同/F10 | 静态 | 降级（D-07） |
| managerName | string | managerName | 基金管理人 | - | F10（免费源不可得） | 静态 | 降级 |
| fundManagerName | string | fundManagerName | 基金经理 | - | `Data_currentFundManager[].name` | 季 | 降级 |
| managementFee | float64 | managementFee | 管理费年费率 | %/年 | 基金合同/定期报告 | 静态 | 剔除（免费源不可得） |
| custodyFee | float64 | custodyFee | 托管费年费率 | %/年 | 基金合同/定期报告 | 静态 | 剔除（免费源不可得） |
| replicationStrategy | string | replicationStrategy | 指数复制策略 | 枚举 | 完全复制/抽样复制 | 静态 | 剔除（免费源不可得） |
| trackingError | float64 | trackingError | 年化跟踪误差 | % | **见 A4.2**，必须自算（D-08） | 月/季 | 替代（自算；基准源待定） |
| annualTrackingDeviation | float64 | annualTrackingDeviation | 日均跟踪偏离度绝对值 | % | (1/n)Σ\|TD_t\| | 月/季 | 替代（自算；D-08） |
| trackingErrorWindow | string | trackingErrorWindow | 跟踪误差统计区间 | - | 如 "近1年" / "成立以来" | - | 是 |
| trackingErrorStdType | string | trackingErrorStdType | 标准差口径标识 | - | 常量 "sample_std_dev_minus_mean" | - | 是 |
| holdingsCount | int | holdingsCount | 成分股只数 | 只 | 定期报告/F10 | 季 | 降级（D-09） |
| top10Weight | float64 | top10Weight | 前十大集中度 | % | Σ 前十大权重 | 季 | 降级（权重不可得，D-09） |
| equityPosition | float64 | equityPosition | 股票占净值比 | % | `Data_fundSharesPositions` 末值 | 季 | 是 |
| constituentWeights | []ConstituentWeight | constituentWeights | 成分股权重明细 | % | 定期报告/F10 | 季 | 剔除（权重不可得） |
| industryWeights | []IndustryWeight | industryWeights | 行业分布 | % | 定期报告/F10 | 季 | 剔除（需 F10） |
| t0Tradable | bool | t0Tradable | 是否支持T+0回转交易 | bool | 跨境/债券/黄金/货币/商品ETF=true；境内股票ETF=false（D-10） | 静态 | 是 |

```go
type ConstituentWeight struct {
    Symbol   string  `json:"symbol"`   // 成分股 symbol（统一格式，如 sh600519）
    Name     string  `json:"name"`     // 成分股名称
    Weight   float64 `json:"weight"`   // 权重(%)
    AsOfDate string  `json:"asOfDate"` // 报告期
}

type IndustryWeight struct {
    Industry string  `json:"industry"` // 行业名称
    Weight   float64 `json:"weight"`   // 权重(%)
    AsOfDate string  `json:"asOfDate"` // 报告期
}

type IOPVPoint struct {
    Time string  `json:"time"` // 市场本地时间 HH:mm:ss
    IOPV float64 `json:"iopv"`
}
```

## 4.3 交易日历与市场状态 `models.MarketStatus` / `TradingSchedule`

| 字段名 | Go 类型 | JSON tag | 含义 | 单位 | 计算公式/来源 | 更新频率 | 核心必需 |
|---|---|---|---|---|---|---|---|
| market | Market | market | 市场 | 枚举 | CN | - | 是 |
| status | TradePhase | status | 状态 | 枚举 | trading/closed/pre_market/auction/lunch_break | 实时 | 是 |
| statusText | string | statusText | 中文状态文案 | - | 派生 | 实时 | 是 |
| isTradeDay | bool | isTradeDay | 是否交易日 | bool | 交易日历 | 实时 | 是 |
| holidayName | string | holidayName | 节假日名称 | - | 交易日历 | 实时 | 是 |
| timezone | string | timezone | IANA 时区 | - | Asia/Shanghai | - | 是 |
| localTime | string | localTime | 市场本地时间 | RFC3339 | now.In(loc) | 实时 | 是 |
| beijingTime | string | beijingTime | 北京时间 | RFC3339 | now.In(Asia/Shanghai) | 实时 | 是 |

```go
type TradingPeriod struct {
    Status    TradePhase `json:"status"`    // pre_market/auction/trading/lunch_break/closed
    Text      string     `json:"text"`      // 中文描述
    StartTime string     `json:"startTime"` // 市场本地时间 HH:mm
    EndTime   string     `json:"endTime"`   // 市场本地时间 HH:mm
}

type TradingSchedule struct {
    Market      Market          `json:"market"`
    IsTradeDay  bool            `json:"isTradeDay"`
    HolidayName string          `json:"holidayName"`
    Timezone    string          `json:"timezone"`
    Periods     []TradingPeriod `json:"periods"`
    TradeDates  []string        `json:"tradeDates,omitempty"` // YYYY-MM-DD
}
```

## 4.4 折溢价率历史序列（供 AI Agent 与分位计算）

```go
type PremiumRatePoint struct {
    Date             string  `json:"date"`             // YYYY-MM-DD
    ClosePrice       float64 `json:"closePrice"`       // 收盘价
    NAV              float64 `json:"nav"`              // 单位净值
    ClosePremiumRate float64 `json:"closePremiumRate"` // (close−nav)/nav×100
    IntradayHigh     float64 `json:"intradayHighPremiumRate,omitempty"` // 盘中最高溢价率
    IntradayLow      float64 `json:"intradayLowPremiumRate,omitempty"`  // 盘中最低溢价率
}
```

## 4.5 风险提示模型（看板必须可见）

```go
type RiskAlert struct {
    Code        string  `json:"code"`        // 风险码，如 ETF_PREMIUM_HIGH
    Level       string  `json:"level"`       // info/warning/critical
    Title       string  `json:"title"`       // 标题
    Detail      string  `json:"detail"`      // 详情
    Trigger     string  `json:"trigger"`     // 触发依据（含实测数值）
    Suggestion  string  `json:"suggestion"`  // 处置建议
    AsOf        string  `json:"asOf"`        // 判定时点
}

type RiskProfile struct {
    Symbol string      `json:"symbol"`
    Alerts []RiskAlert `json:"alerts"`
}
```

> **强制约束**：`Trigger` 必须含实测数值 —— **无实测值的告警等于噪音**。风险提示必须在 UI 可见（裁决 §七第 5 条）。

## 4.6 AI Agent 工具清单（tool registry）

现有 37 个内置工具（`internal/adk/tools/registry.go`），ETF 相关改动：

| 工具名 | 类型 | 说明 | 优先级 |
|---|---|---|---|
| `get_etf_realtime` | 新增 | ETF 行情 + IOPV + 盘中溢价率（**独立通路**，D-01） | P0 |
| `get_etf_profile` | 新增 | ETF 产品层信息（规模/成立日/跟踪指数/费率/经理） | P1 |
| `get_etf_holdings` | 新增 | 成分股权重 + 行业分布 + 集中度 | P2 |
| `get_etf_tracking` | 新增 | 跟踪误差/跟踪偏离度（**自算**，D-08） | P2 |
| `get_premium_rate_history` | 新增 | 折溢价率历史序列与分位 | P2 |
| `get_risk_alerts` | 新增 | ETF 风险提示（溢价、QDII额度、T+0/T+1 三类必显） | P1 |
| `get_stock_realtime` | **改造** | 增加 `assetType`/`market`/`currency` 入参，返回新增字段 | P0 |
| `get_kline_data` | **改造** | 时间戳 RFC3339 带偏移（D-05） | P1 |
| `get_orderbook` | **改造** | ETF 盘口量单位语义（份） | P1 |
| `search_stocks` | **改造** | ETF 搜索：新浪 `etf_hq_fund` 列表(1715只) + 腾讯 smartbox 类型标签，返回 `market`/`assetType`/`securityType`（D-12 已解决，见 A6） | P0 |

**AI Agent 上下文注入要求**（`app.go buildCoreContext`，现仅注入 quote/position/marketStatus/remote 四段）：需注入 `market / assetType / currency / securityType / tradePhase / quoteTime / t0Tradable`，否则 Agent 会用 A股个股常识误读 ETF 数据（误判 T+1/T+0、误用市值口径、忽略份额 vs 股本）。

---

# 第四部分 · 风险提示清单

> 每条注明**触发依据**。看板必须展示，且 AI Agent 的分析必须显式引用。
> 「本期」列依据裁决记录 §3.3：**本期落地核心 8 条**，其余依赖本期不可得的字段或属静态说明，保留在契约中待数据源到位后启用。

## D1. A股 ETF 风险（14 条 · 本期落地 8 条）

| # | 代码 | 级别 | 风险 | 触发依据（可计算） | 建议 | 本期 |
|---|---|---|---|---|---|---|
| 1 | `ETF_PREMIUM_HIGH` | critical | 盘中溢价过高，买入即承担溢价收敛损失 | `premiumRate > 3%`（A股ETF正常波动 <0.5%） | 等待溢价收敛 / 改用场外联接基金 | ✅ |
| 2 | `ETF_PREMIUM_EXTREME` | critical | 极端溢价（历史案例 24.69%–28.30%） | `premiumRate > 10%` | **禁止追高**；查是否已发布风险提示公告 | ✅ |
| 3 | `ETF_QDII_QUOTA` | critical | QDII 外汇额度耗尽 → 一级市场申购关闭 → 套利机制失效 → 溢价持续 | 跨境 ETF 且 `premiumRate > 1%` 且管理人公告限大额申购 | 溢价收敛风险显著，优先场外 | ✅ |
| 4 | `ETF_TIME_LAG` | warning | 盘中 IOPV 与真实资产价值背离 | 跨境 ETF（海外休市时 IOPV 用海外上一收盘价 + 实时汇率测算） | 勿把 IOPV 溢价率当真实套利空间 | ✅ |
| 5 | `ETF_DISCOUNT` | warning | 折价交易（可能预示流动性差或赎回压力） | `premiumRate < −0.5%` | 检查规模与流动性 | ✅ |
| 6 | `ETF_TRADING_HALT` | warning | 临时停牌风险（基金公司可申请盘中临停降温） | 管理人已发布溢价风险提示公告 | 临停后复牌可能伴随集中抛压 | ✅ |
| 7 | `ETF_LIQUIDITY` | warning | 规模小/流动性差 → 冲击成本高 | `fundScale < 5亿` 或 `amount` 显著低于同类 | 避开，或用限价单 | ✅ |
| 8 | `ETF_T0_RULE` | info | 场内/场外规则差异 | 境内股票ETF T+1；跨境/债券/黄金/货币/商品ETF T+0 | 影响交易计划 | ✅ |
| 9 | `ETF_TRACKING_ERROR` | warning | 跟踪误差超合同约定 | `trackingError > 2%`（A股股票ETF）/ `> 6%`（境外LOF） | 评估管理人指数复制能力 | ⏳ 依赖 D-08 |
| 10 | `ETF_SMALL_SCALE_CLEAR` | warning | 规模过小 → 清盘风险 | `fundScale < 5000万`（合同约定的连续60个工作日低于5000万可触发清盘） | 关注合同条款 | ⏳ 依赖 D-06 |
| 11 | `ETF_SCALE_CONCENTRATION` | info | 单一成分股/单一行业集中度过高 | `top10Weight > 60%` 或单行业 > 30% | 视为行业 Beta 而非宽基配置 | ⏳ 依赖 D-09 |
| 12 | `ETF_DIVIDEND_NAV_DROP` | info | 分红导致单位净值跳空 | 净值序列 `unitMoney` 非空 | 用累计净值/复权计算收益率，否则高估回撤 | ⏳ 需净值序列积累 |
| 13 | `ETF_OTF_VS_ONMARKET` | info | 场内 vs 场外定价差异 | 场内按市价成交（含溢价），场外按日终净值；跨系统转托管 **T+2** | 明确走哪条通道 | ⏳ 静态说明，非告警 |
| 14 | `ETF_REBALANCE_IMPACT` | info | 指数调样/成分股停牌造成跟踪误差跳升 | 定期报告跟踪偏离度突然放大 | 属正常事件，非管理人失误 | ⏳ 依赖 D-08 |

## D2. 汇率风险（跨境 ETF 专有）

| # | 代码 | 级别 | 风险 | 触发依据 | 建议 | 本期 |
|---|---|---|---|---|---|---|
| 15 | `ETF_FX_RISK` | critical | 人民币兑美元汇率波动侵蚀跨境 ETF 实际收益 | `securityType == cn_fund_qdii`（标的以美元计价） | 以人民币口径核算实际收益；勿只看指数涨幅 | ✅ |

> **说明**：原「跨市场组合级风险」（日历错配 / 跨币种相加 / 跨市场时段 / 延迟行情）随多市场方向一并剔除 —— 本期单一市场、单一币种，跨市场组合级风险不成立。延迟行情的**能力**保留在 `dataDelayed` 字段与 `dataSource` 标识中（§4.1），但不作为独立风险条目。

---

# 第五部分 · 实施优先级

## P0 · 强烈建议先做（用户价值最高 + 实现难度低）

| # | 项目 | 理由 | 依赖 |
|---|---|---|---|
| 1 | **统一枚举 + 货币字段 + `assetType`/`securityType`** | 一切后续功能的地基；不先做，后面每加一个字段都要改一遍 | 无 |
| 2 | **ETF 复用现有个股管线** | 主理人已交叉验证：TDX 主源与 Sina 兜底源取数路径**不区分资产类型**；三大主链路**零改动** | 1 |
| 3 | **ETF IOPV + 盘中溢价率（独立通路）** | ETF 的核心差异化能力；SSE/SZSE 官方接口实测可用；折溢价率双字段拆分 | 1 |
| 4 | **ETF 搜索（返回 market/assetType/securityType）** | 用户找不到 ETF 一切归零；**数据源已定**（新浪 `etf_hq_fund` 1715只 + 腾讯 smartbox，见 A6） | 1,2 |
| 5 | **市场状态接口加 market 参数** | 沿用现有 `GetMarketStatus()`，加参数即可，为 `Market` 维度留位 | 1 |
| 6 | **前端货币/时段/份额语义适配** | 用户明确诉求；`volume`/`amount`/`totalShares` 对 ETF 需显示为「份」 | 1,5 |
| 7 | **RFC3339 带时区偏移（D-05）** | 现在立规范成本几乎为零，后期改要动所有历史数据 | 无 |

## P1 · 高价值（工作量中等）

| # | 项目 | 理由 |
|---|---|---|
| 8 | ETF 产品层信息卡（规模/成立日/跟踪指数/费率/管理人/经理） | 问答场景核心；pingzhongdata 实测可得大部分（D-06/D-07 通路待定） |
| 9 | 统一风险提示模型 + `get_risk_alerts` 工具 | 用户明确要求"结论在看板可见"；溢价/QDII额度/T+0-T+1 三类必显 |
| 10 | ETF 涨跌幅限制动态取值 | 不能写死 ±10%，须按板块与产品类型取值（D-13） |
| 11 | AI Agent 工具改造 + `buildCoreContext` 注入 | 保证 Agent 不拿个股常识误读 ETF 数据 |
| 12 | 折溢价率历史序列积累 | 冷启动期无值，但必须从第一天上就开始存，否则永远没有分位 |

## P2 · 增强（依赖 P1 或需额外抓取）

| # | 项目 | 理由/难点 |
|---|---|---|
| 13 | 跟踪误差 / 跟踪偏离度自算 | 需业绩基准日收益率来源（D-08 副裁决开放依赖）；须处理分红再投资 |
| 14 | ETF 申赎清单（PCF） | 交易所每日发布，含成分股代码/数量/现金替代标志 |
| 15 | ETF 成分股只数 / 前十大集中度 | 权重不可得（D-09），只有代码 |
| 16 | 折溢价率历史分位展示 | 依赖 #12 的序列积累 |

## P3 · 本期不做（需付费源 / 需人工，详见第六部分）

| # | 项目 | 状态 |
|---|---|---|
| 17 | ETF 管理费/托管费 | 免费源不可得，须读基金合同/定期报告 |
| 18 | ETF 复制策略 | 免费源不可得 |
| 19 | 成分股权重 / 行业分布 | 需 F10/定期报告 |
| 20 | 基金规模/份额稳定通路 | 东财 `f116`/`f84` 在服务器被风控（D-06 待替代域名） |

---

# 第六部分 · 本期不做清单（因免费源不可得）

> 以下字段**口径已定义并保留在数据契约中**，本期不实现。
> **未来接入付费源或定期报告后可直接启用，字段已在 §4.1/§4.2 预留，无需改动契约结构。**

| 字段 | 契约位置 | 为什么不可得 | 未来启用路径 |
|---|---|---|---|
| `managementFee`（管理费年费率） | §4.2 | 免费源只有**申购费率**（`fund_Rate`），不是管理费；`fund_sourceRate` 是原申购费率 | 基金合同 / 定期报告「基金费用」章节，或付费基金数据源 |
| `custodyFee`（托管费年费率） | §4.2 | 同上 | 同上 |
| `replicationStrategy`（复制策略） | §4.2 | 基金合同表述不结构化，无结构化字段 | 基金合同文本解析，或基金主数据源 |
| `constituentWeights[]`（成分股权重） | §4.2 | pingzhongdata 只有前十大**代码**，**无权重** | 定期报告持仓明细 / 付费 F10 持仓接口 |
| `industryWeights[]`（行业分布） | §4.2 | 同上 | 定期报告行业配置 / 付费 F10 |
| `holdingsCount`（成分股只数） | §4.2 | 需持仓明细汇总 | 同上（降级实现） |
| `top10Weight`（前十大集中度） | §4.2 | 依赖成分股权重 | 同上（降级实现） |
| `trackingError` / `annualTrackingDeviation` | §4.2 | 不是"不可得"而是"**必须自算**"（D-08）。业绩基准日收益率来源待工程侧确认 → 若不可得则本期降级为不可得 | 基准指数日序列确认可得后启用 |
| `ps` / `epsTtm` / `bvps` | §4.1 | ETF 不适用（无市值/股本口径的盈利含义） | 永久剔除，仅在需要时从契约移除 |
| `dividendYield` / `high52Week` / `low52Week` | §4.1 | 免费源不提供 | 付费数据源 |
| `fundScale` 日频值 | §4.2 | 东财 `f116` 在**服务器 IP** 被风控；真实净资产=份额×单位净值，季度 `Data_fluctuationScale` 可用 | D-06 待确认替代域名 |
| `trackingIndexCode` / `trackingIndexName` | §4.2 | pingzhongdata 不含跟踪指数 | D-07 待工程侧确认 F10 通路 |
| `volumeRatio`（ETF 语义）/ `avgPrice` | §4.1 | ETF 的换手率已实测确定为 **成交额/规模×100**（新浪 ETF 列表 `turnoverratio`）；但量比/均价在 ETF 语义下仍待确认 | D-11 剩余项待实测 |

> **说明**：`ps`/`epsTtm`/`bvps` 与 `dividendYield`/`high52Week`/`low52Week` 已在 §4.1 表中标注为「剔除」，字段定义保留是为了让契约保持完整、未来无需重新设计；本期**不写入响应体**。

---

# 附录 A · 现有代码改造清单

| 文件 | 改造点 |
|---|---|
| `internal/models/stock.go` | `Stock` 加 `market/assetType/securityType/exchange/currency/quoteTime/tradePhase/dataSource/dataDelayed`；`marketCap` 由 `string`→`float64`（**破坏性**）+ 新增 `marketCapDisplay`；新增 `ETFProfile`、`ConstituentWeight`、`IndustryWeight`、`PremiumRatePoint`、`IOPVPoint`、`RiskAlert`、`RiskProfile`、`ResolvedSymbol`（详见 `models_etf_draft.go`） |
| `internal/models/stock.go` | `KLineData.Time` 保持 string 但约定：日线 `YYYY-MM-DD`（市场本地日期）；分时线 RFC3339 带偏移（D-05） |
| `internal/models/market.go`（新） | `Market`、`AssetType`、`SecurityType`、`Currency`、`TradePhase` 枚举 + `TradingCalendar` 接口 + `CNTradingCalendar` |
| `internal/services/market_service.go` | `isTradeDay`/`GetMarketStatus`/`GetTradingSchedule`/`GetTradeDates` 迁入 `CNTradingCalendar`；`GetMarketStatus` 按 market 参数分派 |
| `internal/services/market_provider.go` | `marketProvider` 接口扩展为 `FetchQuote(ResolvedSymbol)`；新增 `etfMarketProvider`（**IOPV 独立通路**，D-01，与行情 provider 解耦） |
| `internal/services/stock_search.go` | 搜索结果加 `market`/`assetType`/`securityType`；支持 ETF 检索（D-12） |
| `internal/adk/tools/registry.go` | 新增/改造 10 个工具（见 §4.6） |
| `app.go` | `mergeRealtimeStock` 为**白名单式手工赋值**（逐字段 copy，漏字段静默丢数据）→ 必须补齐新增字段；`buildCoreContext` 注入 `market/assetType/currency/securityType/tradePhase/quoteTime/t0Tradable`；`GetMarketStatus`/`GetTradingSchedule` 加 market 参数 |
| `frontend/src/types.ts` | `Stock` 接口同步新增字段；`marketCap` 改 number + 新增 `marketCapDisplay`；`volume`/`amount`/`totalShares` 的「股/份」语义在 ETF 下切换；新增 `ETFProfile`/`RiskAlert` 接口 + `Market`/`AssetType` 联合类型 |

**破坏性变更清单（必须前后端同步改）**：
1. `models.Stock.marketCap`: `string` → `float64`（+ 新增 `marketCapDisplay`）—— 前端 3 处：`types.ts:9`、`StockList.tsx:86`、`WelcomePage.tsx:68`
2. `GetMarketStatus()` → 需带 market 参数（或新增 `GetGlobalMarketStatus()` 保留旧接口）
3. `GetTradingSchedule()` → 需带 market 参数
4. `frontend/src/types.ts` 的 `Stock` 接口同步；`sector string` 建议拆 `sectorCode`

---

# 附录 B · 实测原始数据（2026-10-30 11:00–11:03 UTC+8）

```text
# 上交所 Level-1 快照（含 IOPV）—— 关键：513100 纳指ETF 溢价 14.34%
GET http://yunhq.sse.com.cn:32041/v1/sh1/snap/513100?select=name,last,prev_close,open,high,low,volume,amount,tradephase,iopv,nav
→ {"code":"513100","date":20261030,"time":110220,"snap":["纳指ETF",2.2480,2.2900,2.2410,2.2570,2.2340,70574100,158785654,"T111    ",1.9660,0.000]}
   溢价率 = (2.2480 − 1.9660) / 1.9660 × 100% = +14.34%
   注意 nav 字段返回 0.000 → 不可用，须另接净值源

GET http://yunhq.sse.com.cn:32041/v1/sh1/snap/510300?select=...,iopv
→ {"snap":["300ETF",4.0720,4.1110,4.0590,4.0950,4.0460,397437462,1618238792,"T111    ",4.0764,0.000,null]}
   溢价率 = (4.0720 − 4.0764) / 4.0764 × 100% = −0.108%

GET http://yunhq.sse.com.cn:32041/v1/sh1/snap/510050?select=...,iopv
→ {"snap":["50ETF",2.8530,2.8710,2.8660,2.8670,2.8420,331735926,947349386,"T111    ",2.8549]}
   溢价率 = (2.8530 − 2.8549) / 2.8549 × 100% = −0.0665%

# 深交所实时快照（含 IOPV + IOPV 分时 + 五档）
GET https://www.szse.cn/api/market/ssjjhq/getTimeData?marketId=1&code=159915
→ data.netValue = "2.469"   (IOPV)
  data.now       = "2.469"   (现价)
  data.sellbuy5  = [{price,volume}×5]
  data.piciopv   = [["09:30",2.45],["09:31",2.443],...]   (IOPV 分时序列)
  data.picavgprice = [["09:30",2.45],...]                    (分时均价线)
  data.picupdata   = [['09:30','2.450','2.450','-0.085','-3.35',940124,230330380.0,'2.450'],...]
  data.groupId = 8  (ETF 标识)
  ⚠️ 逐标的接口，批量需并发

# 东财 A股 ETF
GET https://push2.eastmoney.com/api/qt/stock/get?secid=1.510300&fields=...
→ f43=4071现价(×1000) f44=4095高 f45=4046低 f46=4059开 f47=3798746量(手)
  f48=1546738722额(元) f51=4522涨停价 f52=3700跌停价 f71=4072均价
  f80=[{"b":202610300930,"e":202610301130},{"b":202610301300,"e":202610301500}]  ← 交易时段，可直接做日历
  f84=37475587584 总份额 f86=时间戳 f116=152563117054.464 基金规模(元) f169=-40涨跌额 f170=-97涨跌幅%
  f171=119振幅% f189=20120528 成立日 f292=2
  ⚠️ 全字段扫描 1..700 未发现 IOPV 字段 → 必须走交易所接口

# 东财 ETF 基金详情
GET https://fund.eastmoney.com/pingzhongdata/510300.js  (544KB)
→ 变量见 §A3；⚠️ Data_performanceEvaluation 的"跟踪误差"是 0-100 评分(实测=90)，非百分比

# A股 ETF 行情与个股同构（验证"零改动"结论）
GET https://hq.sinajs.cn/list=sh510300,sh510050,sz159915
→ var hq_str_sh510300="沪深300ETF华泰柏瑞,4.059,4.111,4.071,4.095,4.046,4.071,4.072,377938662,1538858962.000,...,2026-10-30,10:48:35,00,"
   与个股字段布局完全一致
GET https://qt.gtimg.cn/q=sh510300,sh510050,sz159915
→ v_sh510300="1~沪深300ETF华泰柏瑞~510300~4.072~4.111~4.059~3778205~...~153837.7752~0.0000~0~   A~ETF~..."
   第 56 位附近含 "ETF" 类型标识
```

## 附录 B-2 · 存档参考：多市场方向（**已按用户 2026-10-30 决定剔除，以下仅作历史存档，非本期范围**）

> 本节记录多市场方向在剔除前完成的实测，供未来若恢复该方向时参考。**本期不实现、不引用、口径不作为契约。**

```text
# 腾讯多市场行情（us 前缀，73 个字段）
GET https://qt.gtimg.cn/q=usAAPL,usSPY,us.DJI,us.IXIC,us.SPX,usVIX
  [0]=200 市场标识  [1]=苹果  [2]=AAPL.OQ(交易所后缀)  [3]=348.91现价  [4]=343.83昨收
  [30]=时间戳  [31]=涨跌额  [32]=涨跌幅%  [33]=最高  [34]=最低  [35]=USD货币
  [39]=40.01 PE(TTM)  [43]=1.20振幅%  [44]=流通市值(亿USD)  [45]=总市值(亿USD)  [46]=英文名
  [47]=8.72 EPS(TTM)  [48]/[49]=52周高/低  [51]=47.36 PB  [52]=0.30股息率%  [56]=GP/GP-ETF/ZS 类型
  [62]/[63]=总/流通股本  [64]=1.04量比  [67]=349.40均价
  usSPY: PE/PB/EPS/市值/份额全部为空 → 该市场 ETF 免费源无 NAV/iNAV/AUM

# 腾讯全局市场状态（一次拿全，含 US_close）
"market":["2026-10-30 11:02:43|HK_open_交易中|SH_open_交易中|SZ_open_交易中|US_close_已收盘|..."]

# 腾讯美股 K线 + 延长时段 + 夏令时标记（端点仅返回2条，需重新探查）
pandata: {last:'352.05', pct:'0.90', netchange:'3.14', time:'2026-10-29 20:01:00', tag:'after', season:'EST'}
```

**剔除时一并撤销的技术决策**（本期不适用，勿照搬）：
- `import _ "time/tzdata"` —— 夏令时需求；A股无 DST，用 `time.FixedZone("CST", 8*60*60)` 即可
- `Session` 枚举（盘前/常规/盘后）—— 延长交易时段概念
- 境外市场节假日日历（9 个休市日 + 提前收盘日）
- 境外公司行动（拆股/反向拆股）、ADR 比例、财年结束月、做空数据
- 境外 ETF 溢价率口径（`(marketPrice − NAV)/NAV`）—— 与 A股 IOPV 口径同构但数据可得性完全不同

---

# 附录 C · 口径来源

| 口径 | 来源 |
|---|---|
| IOPV 每 15 秒播报、溢价率公式 | 证监会北京监管局《ETF基金投资者教育问答》 |
| 溢价率/折价率公式、套利机制、申赎规则 | 深交所基金部《深交所ETF投资问答》（中国财政经济出版社2024年版）第44期；深交所投教《场内基金交易指南》 |
| A股股票ETF 日均跟踪偏离度≤0.2%、年化跟踪误差≤2% | 深交所投教《ETF投资者问答》第15期；中证网《多只ETF跟踪同一标的指数应当如何选择》(2024-12-26) |
| 跟踪偏离度与年化跟踪误差计算公式 | 基金招募说明书（如华安标普全球石油LOF）；海通证券《公募基金的工具属性评级方法(2020)》（中国基金业协会发布）；Morningstar《ETF Index Tracking Analysis Notebook Methodology》(2022-01) |
| QDII 额度导致跨境 ETF 高溢价 | 《华夏时报》《上海证券报·中国证券网》《财联社》《新浪财经》公开报道（2026-08/09） |
| 交易所/东财/新浪/腾讯接口字段 | 2026-10-30 实测（附录 B） |
| 字段级取舍与关键决策 | `docs/etf/Phase2-口径裁决记录.md`（主理人编写） |

---

> **免责声明**：本文档为技术口径定义与数据契约说明，用于工程实现。文中所有数值为口径示例或公开报道引用，**不构成任何投资建议**。涉及实际投资决策，请咨询持牌投资顾问专业人士。
