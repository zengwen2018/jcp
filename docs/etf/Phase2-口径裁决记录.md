# Phase 2 裁决记录 — A股 ETF 支持（jcp）

> 范围：**仅 A股 ETF**。美股于 2026-10-30 由用户决定剔除，本文档不含美股内容。
> 主理人：何执舟。裁决依据：基金侧口径定义 + 工程侧服务器实测 + 主理人独立复核。

---

## 一、裁决输入与独立复核

| 来源 | 产出 | 状态 |
|------|------|------|
| 基金侧（沈知微） | 指标口径 + 数据契约（20 类型 / 139 字段） | ✅ `docs/etf/ETF指标口径与数据契约.md` |
| 工程侧（寇豆码） | 数据源可得性探查 + 改造面评估 | ✅ `docs/etf/数据源可得性探查报告.md` |
| 主理人 | 独立复核 + 冲突裁决 | ✅ 本文档 |

**主理人未采信单方陈述，对关键实测结论在服务器上独立复测**：

| 结论 | 复核结果 | 证据 |
|---|---|---|
| 新浪 IOPV 可用 | ✅ 属实 | `hq_str_sh510300_iopv="2026-10-30,11:18:20,4.0769"` |
| 腾讯 field78=IOPV | ✅ 属实 | `4.0770`（与新浪 4.0769 交叉吻合） |
| 腾讯 field81=DWJZ | ✅ 属实 | `4.1176` |
| 东财基金域可达 | ✅ 属实 | `api.fund.eastmoney.com` → 200、`fund.eastmoney.com/pingzhongdata` → 200 |
| 东财行情域 push2 不可用 | ✅ 属实，**且为间歇性** | 复测 5 次全 `000`；但**首次调用曾返回 200** |
| TDX 拿不到 ETF IOPV | ✅ 属实 | `protocol/model_quote.go:33` — `ReversedBytes3 int // 保留，未知,基金的昨收净值?` **库未解析** |
| 新浪 ETF 全列表 1715 只 | ✅ 属实 | `getHQNodeStockCount?node=etf_hq_fund` → `"1715"` |
| 腾讯 smartbox 带 ETF 类型标记 | ✅ 属实 | `v_hint="sh~510300~沪深300ETF华泰柏瑞~hs300etfhtbr~ETF"` |

> ⚠️ **push2 是间歇性可用，不是永久封禁** —— 复测中首次命中 200，随后 5 次全 `000`。
> 这修正了工程侧「0/10 = 已封死」的表述。**含义**：东财不能作唯一源，
> 但可作**兜底重试源**；且现有代码的东财依赖是「时好时坏」而非「全坏」。

---

## 二、核心裁决：ETF 行情能否复用现有管线？

### 裁决 D-01：**能，三条主链路零改动**

主理人直接读代码验证：

**TDX 主源**（`internal/services/market_provider_tdx.go:53-125`）
```go
quotes, err := cli.GetQuote(codes...)     // 不区分个股/ETF
symbol := quote.Exchange.String() + quote.Code
```
取数路径**完全不区分资产类型** —— 任何 8 位带市场前缀的代码走同一条路。
行情字段与五档盘口（`BuyLevel`/`SellLevel`）**直接复用现有 `models.Stock`/`models.OrderBook`**。

**Sina 兜底源**（`internal/services/market_service.go:244-263`）同样按位置解析字段，不区分资产类型。

**结论**：`GetStockRealTimeData` / `GetKLineData` / `GetOrderBook` **不为 ETF 写任何分支**。

### 裁决 D-02：**折溢价率必须独立通路，不塞进现有行情响应**

理由：①IOPV 来源与行情源不同（新浪 `_iopv` / 腾讯 field78 vs 行情接口）
②刷新节奏不同（秒级 vs 实时 vs T+1 vs 季度）③失败模式不同，混在一起无法区分
「行情整体失败」与「IOPV 取不到」④`/api/stock/realtime` 是 SSE 高频轮询，塞入持仓/规模会撑爆响应体。

### 裁决 D-03：**新增 Provider 的必要性 —— 降级为「只需 ETF 元数据源」**

工程侧建议新增腾讯 Provider 作「多市场统一主源」（2.0 人日）。主理人裁决：**本期不做**。

理由：
1. TDX + 新浪已覆盖 ETF 的实时行情/盘口/K线，**无需第三个行情源**
2. ETF 需要的增量只有 **IOPV + 净值 + 规模 + 持仓 + 列表**，这些**全部在新浪/东财基金域**
3. 腾讯的独特价值仅两点：field78/field81 可批量拿 IOPV+DWJZ、smartbox 带类型标记
   —— 前者可由新浪 `_iopv` 替代，后者由新浪 `etf_hq_fund` 列表替代
4. 引入第三个非公开行情源 = 多一份格式变更风险，而收益有限

**替代方案（裁决 D-03 副）**：新增的不是「行情 Provider」，而是 **`ETFMetadataProvider`**
—— 一个只负责 ETF 静态/准静态数据的窄接口，与 `marketProvider` 并列而非继承。

```go
type ETFMetadataProvider interface {
    FetchIOPV(code string) (iopv float64, ok bool, err error)
    FetchNAVSeries(code string, limit int) ([]NAVPoint, error)
    FetchETFProfile(code string) (*ETFProfile, error)
    ListETFs(keyword string, limit int) ([]ETFListItem, error)
}
```

---

## 三、最终数据源组合（裁决后）

| 能力 | 主源 | 兜底 | 备注 |
|---|---|---|---|
| 实时行情 + 五档盘口 | **TDX（现有，零改动）** | 新浪（现有，零改动） | 决策 D-01 |
| K线 | **TDX（现有，零改动）** | 新浪 / 腾讯 | 决策 D-01 |
| **IOPV（秒级）** | **新浪 `{code}_iopv`**（实测 27ms） | 腾讯 field78 | 决策 D-02 |
| 单位净值 | **腾讯 field81** | 东财 `api.fund.eastmoney.com/f10/lsjz` | **双源已交叉验证 6/6 一致** |
| 历史净值序列 | **东财 f10/lsjz**（分页） | 东财 `pingzhongdata` `Data_netWorthTrend` | 决策 D-06 |
| 规模（季度序列） | **东财 `pingzhongdata` `Data_fluctuationScale`** | 无 | 决策 D-06 |
| 前十大持仓（代码） | **东财 `pingzhongdata` `stockCodesNew`** | 无 | ⚠️ 无权重，决策 D-09 |
| ETF 全列表（1715） | **新浪 `node=etf_hq_fund`** | 腾讯 smartbox | 决策 D-12 |
| ETF 搜索 + 类型标记 | **腾讯 smartbox**（原生 `~ETF`） | 新浪列表本地过滤 | 决策 D-12 |
| 涨跌停价 | **本期不实现** | — | 决策 D-13 |

### 腾讯 ETF 快照字段索引（主理人复核，2026-10-30）

以 `qt.gtimg.cn?q=sh510300` 实测标定（0-based，`~` 分隔）：

| 索引 | 实测值 | 含义 | 备注 |
|---|---|---|---|
| field[61] | `ETF` | 标的类型标记 | `GP`=个股 / `ETF`=ETF / `ZS`=指数 |
| field[77] | **0.04** | **折溢价率 (%)** | ⚠️ **不是 field[80]** |
| field[78] | 4.1032 | IOPV（盘中参考净值） | 与新浪 `_iopv` 交叉验证一致 |
| field[80] | 0.44 | 其它 | ⚠️ 早期误记为折溢价，实测否 |
| field[81] | 4.1176 | 单位净值 DWJZ | 与东财 lsjz 6/6 一致 |
| field[82] | `CNY` | 货币 | |
| field[77] 附近 | 37475587700 | 总份额 | ⚠️ 份额数，非市值 |

**验证**：同一时刻 `price=4.105` / `IOPV=4.1032` → 自算 `(4.105−4.1032)/4.1032×100`
= **0.0439%**，与 field[77]=`0.04` 吻合。

> 折溢价率**仍然自算**（不依赖源站字段），field[77] 只作**交叉校验哨兵**。

**角色分工一句话**：
**TDX/新浪管「价格」，新浪 `_iopv` 管「净值锚」，东财基金域管「产品档案」，腾讯 smartbox 管「发现」。**

---

## 四、逐字段最终裁决

> `保留` = 按基金侧口径实现；`降级` = 保留字段但降级精度/频率/来源；
> `替代` = 换算法或换源；`剔除` = 本期不做（契约中预留字段）。

### 4.1 通用行情层 `models.Quote`

| 字段 | 裁决 | 理由 |
|---|---|---|
| `symbol`/`name`/`price`/`preClose`/`open`/`high`/`low` | **保留** | ETF 与个股同构，零改动 |
| `change`/`changePercent`/`amplitude` | **保留** | 复用现有计算 |
| `volume`/`amount` | **保留** | ⚠️ 语义：ETF 为「份」非「股」，数值不变、显示文案需区分 |
| `market`/`assetType`/`securityType` | **保留** | 决策 D-04 |
| `currency` | **保留** | 本期恒 `CNY`，字段先建（决策 D-05） |
| `exchange` | **保留** | SSE/SZSE/BSE |
| `marketCap` | **替代** | 决策 D-06（`string`→`float64` + `marketCapDisplay`） |
| `peTtm`/`pb` | **降级** | ETF 无估值语义，固定 0，UI 隐藏 |
| `epsTtm`/`bvps`/`ps` | **剔除** | ETF 不适用 |
| `turnoverRate`/`volumeRatio`/`avgPrice` | **保留** | ✅ 新浪 ETF 列表实测含 `turnoverratio`/`mktcap`/`nmc` |
| `totalShares`/`floatShares` | **降级** | ETF 为「份额」，季度更新（决策 D-06） |
| `quoteTime` | **保留** | 强制 RFC3339 带偏移（决策 D-07） |
| `tradePhase` | **保留** | A股时段，复用现有 `getMarketPhase()` |
| `dataSource`/`dataDelayed` | **保留** | ETF 源与个股不同 |
| `high52Week`/`low52Week`/`dividendYield` | **剔除** | 免费源不可得 |

### 4.2 ETF 专属层 `models.ETFProfile`

| 字段 | 裁决 | 理由 |
|---|---|---|
| `iopv` | **保留** | 折溢价率分母，**ETF 能力分水岭**（决策 D-02） |
| `nav` | **保留** | 日频折溢价分母 + 跟踪误差基础 |
| `accNav` | **保留** | 跟踪误差必须用累计净值或分红再投资调整 |
| `navDate` | **保留** | 区分盘中 IOPV 与日终净值 |
| `premiumRate` | **保留** | 盘中 `(price−iopv)/iopv×100%` |
| `closePremiumRate` | **保留** | 日频 `(close−nav)/nav×100%` |
| `premiumRatePercentile` | **降级** | 需冷启动，先给空值 |
| `premiumRateHistory` | **降级** | 新浪/腾讯 IOPV 序列可得但量有限 |
| `fundScale` | **保留** | ✅ 东财 `Data_fluctuationScale` 实测 200，含季度序列 |
| `shareCount` | **降级** | 季度更新 |
| `inceptionDate` | **降级** | 东财 `f189` 走 push2（间歇性），改用 pingzhongdata |
| `trackingIndexCode`/`trackingIndexName` | **降级** | 免费源不可得 → 决策 D-08（显示"跟踪指数"名而非代码） |
| `managerName` | **剔除** | 免费源不可得 |
| `fundManagerName` | **保留** | ✅ `Data_currentFundManager` |
| `managementFee`/`custodyFee`/`replicationStrategy` | **剔除** | 免费源不可得 |
| `trackingError`/`annualTrackingDeviation` | **替代** | 决策 D-09：**必须自算** |
| `trackingErrorWindow`/`trackingErrorStdType` | **保留** | 口径标识 |
| `holdingsCount`/`top10Weight`/`equityPosition` | **降级** | 决策 D-10 |
| `constituentWeights`/`industryWeights` | **剔除** | 权重需定期报告/F10 |
| `t0Tradable` | **保留** | 决策 D-11 |

### 4.3 风险提示层

| 项 | 裁决 | 理由 |
|---|---|---|
| ETF 风险 14 条 | **保留核心 9 条** | 剔除依赖不可得字段的（跟踪误差类依赖 D-09、成分集中度依赖 D-10） |
| `RiskAlert` 模型 | **保留** | 契约要求「风险提示不可被工程实现吞掉」 |
| `Trigger` 必须含实测数值 | **保留** | 无实测值的告警等于噪音 |

**保留的 9 条**：ETF_PREMIUM_HIGH / ETF_PREMIUM_EXTREME / ETF_QDII_QUOTA /
ETF_TIME_LAG / ETF_DISCOUNT / ETF_LIQUIDITY / ETF_SMALL_SCALE_CLEAR /
ETF_DIVIDEND_NAV_DROP / ETF_T0_RULE

---

## 五、关键决策清单

### 决策 D-04：标的类型枚举拆成两个正交维度

```go
type Market string     // "CN"（本期唯一值，为未来扩展留位）
type AssetType string  // "stock" | "etf"
type SecurityType string // cn_main/cn_gem/cn_star/cn_bse/cn_fund_lof/cn_fund_qdii/
                        // cn_fund_bond/cn_fund_gold/cn_fund_money/cn_fund_commodity
```

**理由**：合成枚举（如 `cn_etf_qdii` 与 `us_etf`）会导致笛卡尔积，每加一个市场都要改所有 switch。
市场维度决定时段/货币/节假日，资产类型决定有无净值/折溢价 —— 正交，拆开才能独立扩展。

**配套铁律**（写入代码注释）：
1. `symbol` 是不透明 ID，**前端永不解析它**；语义由 `market`/`assetType`/`securityType` 承载
2. symbol 解析只允许发生在一处（`market_code.go`），各数据源适配器只消费解析结果
3. **禁止用 `strings.HasPrefix(code,"sh")` 决定显示单位或时段**

### 决策 D-05：货币字段先建，本期恒为 CNY

**理由**：美股虽已剔除，但这是**防止未来踩坑的前置投资**。现在加成本最低
（前端仅 3 处消费：`types.ts:9`、`StockList.tsx:86`、`WelcomePage.tsx:68`）；
等真要加美股时再加，`mergeRealtimeStock`（白名单式手工赋值，`app.go:384-399`）
与前端所有消费方都要重过一遍。金额跨币种禁止相加，本期不触发但规则先立。

### 决策 D-06：`marketCap` 从 `string` 改为 `float64`

**理由**：ETF 规模/市值需参与计算（折溢价、集中度），字符串无法计算。
保留 `marketCapDisplay string` 供现有前端格式化。**影响面已核实：前端仅 3 处。**

### 决策 D-07：所有时间戳强制带时区偏移

**决策**：落库与传输一律 RFC3339 带偏移（`2026-10-30T11:18:20+08:00`），禁止裸时间串。
**理由**：ETF 的 IOPV 分时序列若存裸时间，跨时区场景会错乱。A股无 DST 风险可控，
但**现在立规范成本几乎为零**，后期改要动所有历史数据。

### 决策 D-09：跟踪误差必须自算，禁用东财评分字段

**决策**：`trackingError` 与 `annualTrackingDeviation` **自算**，
**严禁**使用东财 `Data_performanceEvaluation` 的「跟踪误差」字段。

**理由**：该字段取值是 **0–100 的评分**（实测 510300 跟踪误差评分=90），
**不是年化跟踪误差百分比**。语义完全不同的两样东西占同一字段名是最危险的坑 ——
填进去会让看板显示「跟踪误差 90%」这种荒谬数字。

**自算口径**（与海通证券《公募基金工具属性评级方法》、Morningstar ETF Index
Tracking Analysis 一致）：
```
TD_t = R_P,t − R_B,t
日均跟踪偏离度绝对值 = (1/n)Σ|TD_t|
年化跟踪误差 = sqrt( Σ(TD_t − TD̄)² / (n−1) ) × sqrt(250)
```
- 减均值的样本标准差版本，年化因子 A股用 250
- **必须用累计净值或分红再投资调整** —— 东财 `Data_netWorthTrend` 的 `unitMoney`
  记录每份派发，直接用单位净值算会把分红当亏损，**系统性高估跟踪误差**

> ⚠️ **开放依赖**：业绩基准日收益率 `R_B,t` 的免费来源尚未确认。
> 若不可得 → `trackingError` 本期降级为「不可得」，**不用近似值凑数**。

### 决策 D-10：成分股权重 —— 只取代码，不猜权重

**决策**：`constituentWeights`/`industryWeights` **本期不做**；
`holdingsCount`/`top10Weight`/`equityPosition` 降级为可得项。

**理由**：东财 `stockCodesNew` 给出的是**最近披露期的前十大持仓代码**，
**不含权重**。`top10Weight` 若无权重则无法计算 → 降级为不可得。
持仓展示必须标注「截至 XX 报告期」，避免用户误以为是实时权重。

### 决策 D-11：T+0/T+1 判定来源

**决策**：`t0Tradable` 保留，判定优先级：
1. 深交所 `data.groupId`（ETF 标识，可能含类别）
2. 东财 `pingzhongdata` 基金类型字段
3. 代码段规则兜底（`sh51xxxx`/`sh513xxx`/`sh588xxx`/`sz159xxx` 等跨境类）

**理由**：境内股票 ETF 是 T+1，跨境/债券/黄金/货币/商品 ETF 是 T+0。
这个差异直接影响 AI 的交易建议（当天买入当天可卖 vs 不可）。
**具体判定规则需在实现时用实测数据写死**，不靠猜测。

### 决策 D-13：涨跌停价 —— 本期不实现

**裁决**：ETF 涨跌停价**本期不做**。

**理由**：①现有代码里**根本没实现涨跌停**（grep 到的 `f51/f52` 是资金流的
date/mainNet 字段，不是涨跌停）②涨跌停需按板块动态取值（±10% / ±20% / ±5%），
不可写死 ③ETF 与成分股同规则，用户可从跟踪指数反推 ④不是 ETF 核心能力，
**优先级低于 IOPV/折溢价**。

---

## 六、回答工程侧的 6 项裁决请求

| # | 请求 | 裁决 |
|---|---|---|
| 1 | 代码格式是否用 `us`+ticker | **本期不适用**（美股已剔除）。A股前缀 `sh/sz/bj` **保持不变** |
| 2 | 涨跌颜色是否跟随美股惯例 | **本期不适用**。A股红涨绿跌保持不变 |
| 3 | 折溢价用 IOPV 口径还是净值口径 | **两者都用，分字段**（决策 D-01/D-09 已定）：<br>`premiumRate` 盘中用 IOPV（实时，反映交易折溢价）<br>`closePremiumRate` 日频用单位净值（反映 T+1 真实折溢价）<br>**两者数值不同，必须分别标注口径**，UI 上不可混用 |
| 4 | ETF 清单是否排除跨境 ETF | **不排除，全部纳入**（1715 只）。理由：跨境 ETF 的 IOPV 本质就是海外持仓的 iNAV，**是当前唯一能免费看到真实折溢价的一类**，且 `ETF_QDII_QUOTA`/`ETF_TIME_LAG` 风险提示正是针对它们的 |
| 5 | 美股标的池范围 | **本期不适用** |
| 6 | 是否顺带修东财现存 bug | **独立排期，不与 ETF 捆绑**。但需记录为**已知问题**（见下） |

---

## 七、新发现的现存问题（非本期新增，但需记录）

| 问题 | 事实 | 影响 | 建议 |
|---|---|---|---|
| **东财 push2 间歇性不可用** | 实测 5 次全 `000`，首次曾 200。`market_service.go` 的 `indexSecID`(:1539)、`fetchEastMoneyJSON`(:1746) 依赖它 | 板块资金流 / 盘口异动 / 指数资金流**时好时坏** | **独立任务**：迁移到 `datacenter.eastmoney.com`（实测 10/10）或腾讯/新浪。ETF 支持不依赖 push2，**不阻塞** |
| **TDX `ReversedBytes3` 未解析** | `protocol/model_quote.go:33` 注释「基金的昨收净值?」 | ETF 的 IOPV 无法从 TDX 获得 | **本期绕过**：走新浪 `_iopv`。若将来要在 TDX 侧拿到，需给上游库提 PR |
| **`mergeRealtimeStock` 白名单式赋值** | `app.go:384-399` 逐字段 copy | 新增字段漏加会**静默丢数据**（无报错） | 实现时逐字段核对；建议加单测覆盖 |

---

## 八、给工程实现的强制约束

1. **不改口径**：字段名、公式、单位以 `ETF指标口径与数据契约.md` + 本文档为准
2. **ETF 三大主链路零改动**：`GetStockRealTimeData`/`GetKLineData`/`GetOrderBook`
   不为 ETF 写任何分支逻辑
3. **IOPV 独立通路**：新增 `ETFMetadataProvider` 窄接口与 4 个 ETF 端点，与行情端点解耦
4. **新增 Provider 只做元数据**：不引入第三个行情 Provider（决策 D-03）
5. **前端不解析 symbol**：显示单位/时段由 `market`/`assetType`/`securityType` 决定
6. **风险提示必须在 UI 可见**：ETF 溢价 3 条（PREMIUM_HIGH/PREMIUM_EXTREME/QDII_QUOTA）
   + T+0/T+1 提示 必显
7. **改代码必同步文档**（用户铁律）：`docs/` 下对应章节与 `check_docs.py` 校验必须同步通过
8. **拿不到就说拿不到**：不得用近似值或空数据凑数，AI 工具对不支持的市场返回明确拒绝信息

---

## 九、强制验收清单（Phase 3 实现的门禁）

> 来源：基金侧按「出错概率 × 出错后果」排序的易错项分析。
> **以下每一项都必须由工程侧自测通过**，否则不算完成。

### 验收 1：折溢价率双字段（概率高、后果最高）

**典型错法**：只实现 `premiumRate`；分母写成前收盘价或日终净值；漏 `×100`。

**验收标准**：
- 取 510300 同一时刻：`price=4.084`、`IOPV=4.0863`
  → `premiumRate` 必须 = **−0.06%**
  → `closePremiumRate` 必须 = (4.084−4.1176)/4.1176 = **−0.82%**
  → **两者必须不相等；相等即判错**
- 哨兵：腾讯 `field[77]` 与自算 `premiumRate` 绝对偏差 ≤ 0.05 个百分点
- 513100 纳指ETF：2.250 / 1.9660 → **+14.45%**，须与源站 field[77]=14.45 一致

### 验收 2：跟踪误差自算 + 分红拆分处理（概率中、后果最高）

**典型错法**：①误用东财 `Data_performanceEvaluation` 的「跟踪误差」=**90（评分）**
②用单位净值算收益率 → 分红/拆分被当亏损，系统性高估 ③不减均值。

**验收标准**：
- **量级断言**：扫描全部 ETF 的 `trackingError`，出现 **>50 的值必为评分字段误用** → 判失败
- **拆分断言**：510300 在 2012-05 有「拆分：每份基金份额折算」跳变
  （净值 1.007→2.637、日增长率 −2.86%）。若用单位净值必须剔除
  `unitMoney`/`FHFCZ` 标记日；首选累计净值 `LJJZ`
- **口径断言**：实现的是**减均值**版 `sqrt(Σ(TD−TD̄)²/(n−1))×√250`；
  不减均值版应接近但不相等
- **基准序列未确认前，`trackingError` 必须留空，不得填近似值**

### 验收 3：`mergeRealtimeStock` 白名单漏字段（静默失败）

**典型错法**：struct 加了字段，忘了在 `app.go:384-399` 逐字段 copy
→ 接口返回零值，**不报错**。

**验收标准（零值反射断言）**：
拉一次 ETF 行情，断言 `market=="CN"`、`currency=="CNY"`、`assetType=="etf"`、
`securityType!=""`、`tradePhase!=""`、`quoteTime!=""`、`dataSource!=""`。
**任一为空即白名单漏字段**（非数据源问题）。

另：`marketCap` string→float64 是破坏性变更，前端 3 处
（`types.ts:9` / `StockList.tsx:86` / `WelcomePage.tsx:68`）必须同步，
否则显示 NaN。

### 验收 4：规模口径混淆 + 频率（概率高、后果中）

**典型错法**：把新浪 `nmc`/`mktcap` 当 `fundScale`
（那是**市值口径** = 份额 × 市价），并据此定义日频规模。

**验收标准**：
- `fundScale` 频率必须**季**
- `fundScale` = 份额 × **单位净值**。510300 验：37475587700 × 4.1176
  = **154,326,... 元**；若得 1533.5 亿（= 份额 × 4.092 市价）→ 判错
- `nmc`/`mktcap` **不得落库**到 `fundScale`

### 验收 5：T+0/T+1 与 securityType 判定（概率高、后果中）

**判定源可靠性排序**（基金侧判断）：

| 源 | 可靠性 | 原因 |
|---|---|---|
| 深交所 `groupId=8` | ❌ **最不可靠** | 只说明「是 ETF」，**不区分境内/跨境/债券/商品**，定不了 T+0 |
| 腾讯 smartbox 第5段 | ⚠️ 未知 | 拼音缩写（`hs300etfhtbr`）粒度不明 |
| **代码段 + 名称显式映射表** | ✅ **最可靠** | 产品分类是**制度性**的，必须**写死成表**，禁止运行时猜测 |

映射表示例（需实现时补全并用实测数据确认）：
`sh513xxx`/`sh501xxx` 跨境、`sh511xxx` 货币/债券、`sz159xxx` 需再分。

**验收标准**：
- 抽样断言：`510300=false`、`513100=true`、`511990`(华宝添益货币ETF)=true、
  `511010`(国债ETF)=true、`588000=false`
- `securityType` 必须是**枚举**（不得布尔或自由文本）
- `t0Tradable` 由 `securityType` **推导**，**不得独立硬编码**
- smartbox 与 pingzhongdata 的类型字段做**交叉校验**，
  不一致时以映射表为准并告警

---

## 十、待用户确认的开放问题

1. **跟踪误差是否本期做** —— 依赖业绩基准日收益率来源。若免费源不可得，是否接受「本期不做」？
2. **ETF 列表的缓存策略** —— 1715 只，建议缓存 12 小时。是否可接受？
3. **是否顺带修东财 push2 的现存问题** —— 与 ETF 解耦，可独立排期
