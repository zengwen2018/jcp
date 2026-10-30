package models

import "time"

// ============================================================================
// A股 ETF 支持：类型枚举与数据契约（草案）
//
// 用途：可直接拷入 internal/models/ 作为 ETF 能力的模型层起点。
// 来源：基金侧口径定义 —— docs/etf/ETF指标口径与数据契约.md
// 裁决：docs/etf/Phase2-口径裁决记录.md（字段级取舍：保留/降级/替代/剔除，以该文档为准）
// 实测验证日期：2026-10-30
//
// 范围说明：
//   本期范围 = 仅 A股 ETF。美股/多市场方向已于 2026-10-30 由用户决定剔除，
//   相关类型（USProfile、Session 枚举、us_* 细分类）已删除。
//   Market 维度仍保留（本期唯一值 "CN"），为未来扩展留位。
//
// 本期不实现（因免费源不可得，字段已在契约中预留，未来接入数据源即可启用）：
//   ETFProfile.managementFee / custodyFee       管理费、托管费年费率
//   ETFProfile.replicationStrategy               指数复制策略
//   ETFProfile.constituentWeights[]              成分股权重
//   ETFProfile.industryWeights[]                 行业分布
//   Quote.ps / epsTtm / bvps                      ETF 不适用
//   Quote.dividendYield / high52Week / low52Week  免费源不可得
//   ETFProfile.trackingError / annualTrackingDeviation
//                                                 必须自算（裁决 D-08），
//                                                 业绩基准日收益率来源待确认
//
// 强制约束（裁决 §七）：
//   1. 不改口径 —— 字段名、公式、单位以契约文档为准
//   2. symbol 是不透明 ID，前端永不解析；语义由 market/assetType/currency/securityType 承载
//   3. symbol 解析只允许发生在一处（SymbolResolver），各数据源适配器只消费 ResolvedSymbol
//   4. 禁止用 strings.HasPrefix(code, "sh") 决定显示单位或时段
//   5. 风险提示必须在 UI 可见，且 Trigger 必须含实测数值
// ============================================================================

// ---------------------------------------------------------------------------
// 枚举
// ---------------------------------------------------------------------------

// Market 市场
type Market string

const (
	MarketCN Market = "CN" // 中国内地（沪/深/北）—— 本期唯一取值
	// 未来扩展预留：新增市场时同步补齐 TradingCalendar 与交易日历
)

// AssetType 资产类型
type AssetType string

const (
	AssetTypeStock AssetType = "stock" // 个股
	AssetTypeETF   AssetType = "etf"   // 场内交易基金（ETF/LOF）
	AssetTypeIndex AssetType = "index" // 指数（对应现有 models.MarketIndex，本期不合并，预留）
)

// SecurityType 证券细分类（A股）
// 用途：区分境内股票ETF / 跨境(QDII)ETF / 债券ETF / 商品ETF，
// 决定 T+0/T+1 判定与风险提示触发条件
type SecurityType string

const (
	SecTypeCNMain     SecurityType = "cn_main"      // 主板
	SecTypeCNGEM      SecurityType = "cn_gem"       // 创业板
	SecTypeCNSTAR     SecurityType = "cn_star"      // 科创板
	SecTypeCNBSE      SecurityType = "cn_bse"       // 北交所
	SecTypeCNFundLOF  SecurityType = "cn_fund_lof"  // LOF
	SecTypeCNFundQDII SecurityType = "cn_fund_qdii" // QDII ETF（跨境）→ T+0，触发 ETF_FX_RISK / ETF_QDII_QUOTA
	SecTypeCNFundBond SecurityType = "cn_fund_bond" // 债券/货币/商品 ETF → T+0
)

// Currency 计价货币（ISO 4217）
// 裁决 D-03：本期恒为 CNY，但字段先建 —— 现在加成本最低
type Currency string

const (
	CurrencyCNY Currency = "CNY"
)

// TradePhase 交易时段（A股）
type TradePhase string

const (
	PhasePreMarket  TradePhase = "pre_market"  // 盘前
	PhaseAuction    TradePhase = "auction"     // 集合竞价
	PhaseTrading    TradePhase = "trading"     // 交易中
	PhaseLunchBreak TradePhase = "lunch_break" // 午间休市
	PhaseClosed     TradePhase = "closed"      // 已收盘 / 休市
)

// ---------------------------------------------------------------------------
// 通用行情层
// ---------------------------------------------------------------------------

// Quote A股个股 / ETF 共用行情
type Quote struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`

	Market       Market       `json:"market"`       // CN（本期唯一值）
	AssetType    AssetType    `json:"assetType"`    // stock / etf
	SecurityType SecurityType `json:"securityType"` // cn_main / cn_fund_qdii / ...
	Exchange     string       `json:"exchange"`     // SSE / SZSE / BSE
	Currency     Currency     `json:"currency"`     // CNY

	Price        float64 `json:"price"`
	PreClose     float64 `json:"preClose"`
	Open         float64 `json:"open"`
	High         float64 `json:"high"`
	Low          float64 `json:"low"`
	Change       float64 `json:"change"`
	ChangePct    float64 `json:"changePercent"`
	Amplitude    float64 `json:"amplitude,omitempty"`
	Volume       int64   `json:"volume"` // 股(个股) / 份(ETF) —— 语义不同，数值口径不变
	Amount       float64 `json:"amount"` // 货币单位
	AvgPrice     float64 `json:"avgPrice,omitempty"`
	TurnoverRate float64 `json:"turnoverRate,omitempty"`
	VolumeRatio  float64 `json:"volumeRatio,omitempty"`
	BidVolume    int64   `json:"bidVolume,omitempty"`
	AskVolume    int64   `json:"askVolume,omitempty"`

	TotalShares  float64 `json:"totalShares,omitempty"`  // 总股本(个股) / 总份额(ETF)
	FloatShares  float64 `json:"floatShares,omitempty"`  // 流通股本 / 流通份额
	MarketCap    float64 `json:"marketCap,omitempty"`    // 单位：货币元（裁决 D-04：由 string 改为 float64）
	FloatCap     float64 `json:"floatMarketCap,omitempty"`
	MarketCapTxt string  `json:"marketCapDisplay,omitempty"` // 可读文案（裁决 D-04 新增）

	// ETF 无市盈率/市净率语义，固定为 0 并在 UI 隐藏（裁决 3.1 降级）
	PeTtm float64 `json:"peTtm,omitempty"`
	Pb    float64 `json:"pb,omitempty"`

	// 以下字段口径保留但本期剔除，不写入响应体（见文件头说明）
	// Ps           float64 `json:"ps,omitempty"`
	// EpsTtm       float64 `json:"epsTtm,omitempty"`
	// Bvps         float64 `json:"bvps,omitempty"`
	// DivYield     float64 `json:"dividendYield,omitempty"`
	// High52Week   float64 `json:"high52Week,omitempty"`
	// Low52Week    float64 `json:"low52Week,omitempty"`

	QuoteTime  string     `json:"quoteTime"`          // RFC3339 带时区偏移（裁决 D-05）
	TradePhase TradePhase `json:"tradePhase"`         // A股交易时段
	DataSource string     `json:"dataSource"`         // tdx/sina/sse/szse/eastmoney
	DataDelayed bool      `json:"dataDelayed"`        // 是否为延迟行情
}

// ---------------------------------------------------------------------------
// ETF 专属层
// ---------------------------------------------------------------------------

// ETFProfile ETF 产品层信息
// 取数通路与行情端点解耦（裁决 D-01）：IOPV 来自交易所直连接口，
// 与行情源不同来源、不同刷新节奏（15s）、不同失败模式
type ETFProfile struct {
	Symbol string `json:"symbol"`

	// ---- 净值层 ----
	IOPV      float64 `json:"iopv"`            // 盘中参考净值，交易所每 15 秒播报
	NAV       float64 `json:"nav"`             // 前一交易日单位净值
	AccNAV    float64 `json:"accNav"`          // 累计单位净值（跟踪误差必须用它或做分红再投资调整）
	NAVDate   string  `json:"navDate"`         // YYYY-MM-DD
	NavSource string  `json:"navSource,omitempty"`

	// ---- 折溢价（两个口径不可混用）----
	// premiumRate:       盘中口径，分母 = IOPV。公式 (price - iopv)/iopv*100
	// closePremiumRate:  日频口径，分母 = 前收单位净值。公式 (close - nav)/nav*100
	PremiumRate      float64            `json:"premiumRate"`
	ClosePremiumRate float64            `json:"closePremiumRate,omitempty"`
	PremiumRatePctile float64           `json:"premiumRatePercentile,omitempty"` // 历史分位 %（冷启动先给空值）
	PremiumRateHistory []PremiumRatePoint `json:"premiumRateHistory,omitempty"`
	IOPVHistory       []IOPVPoint        `json:"iopvHistory,omitempty"`

	// ---- 产品层 ----
	FundScale         float64 `json:"fundScale,omitempty"`   // 净资产，货币单位
	ShareCount        float64 `json:"shareCount,omitempty"`  // 基金份额，份
	InceptionDate     string  `json:"inceptionDate,omitempty"`
	TrackingIndexCode string  `json:"trackingIndexCode,omitempty"`
	TrackingIndexName string  `json:"trackingIndexName,omitempty"`
	ManagerName       string  `json:"managerName,omitempty"`
	FundManagerName   string  `json:"fundManagerName,omitempty"`
	// 以下字段口径保留但本期剔除（免费源不可得）：
	// ManagementFee     float64 `json:"managementFee,omitempty"`      // 管理费年费率 %
	// CustodyFee        float64 `json:"custodyFee,omitempty"`         // 托管费年费率 %
	// Replication       string  `json:"replicationStrategy,omitempty"` // 完全复制 / 抽样复制

	// ---- 跟踪误差（裁决 D-08：必须自算，禁用东财评分字段）----
	// 口径见契约文档 A4.2：
	//   TD_t = R_P,t - R_B,t
	//   annualTrackingDeviation = (1/n)Σ|TD_t|
	//   trackingError = sqrt(Σ(TD_t-TD̄)²/(n-1)) × sqrt(250)   // 减均值的样本标准差
	TrackingError            float64 `json:"trackingError,omitempty"`
	AnnualTrackingDeviation  float64 `json:"annualTrackingDeviation,omitempty"`
	TrackingErrorWindow      string  `json:"trackingErrorWindow,omitempty"`
	TrackingErrorStdType     string  `json:"trackingErrorStdType,omitempty"` // sample_std_dev_minus_mean

	// ---- 持仓层 ----
	HoldingsCount  int     `json:"holdingsCount,omitempty"`
	Top10Weight    float64 `json:"top10Weight,omitempty"`
	EquityPosition float64 `json:"equityPosition,omitempty"` // 股票占净值比 %
	// 以下字段口径保留但本期剔除（权重需定期报告/F10）：
	// ConstituentWeights []ConstituentWeight `json:"constituentWeights,omitempty"`
	// IndustryWeights    []IndustryWeight    `json:"industryWeights,omitempty"`

	// ---- 交易规则 ----
	// 境内股票ETF = T+1；跨境/债券/黄金/货币/商品ETF = T+0（裁决 D-10）
	T0Tradable bool `json:"t0Tradable"`

	// ---- 源标识 ----
	IOPVSource string `json:"iopvSource,omitempty"` // sse / szse
}

// ConstituentWeight 成分股权重（本期剔除，结构预留）
type ConstituentWeight struct {
	Symbol   string  `json:"symbol"`   // 成分股 symbol（统一格式，如 sh600519）
	Name     string  `json:"name"`     // 成分股名称
	Weight   float64 `json:"weight"`   // 权重(%)
	AsOfDate string  `json:"asOfDate"` // 报告期 YYYY-MM-DD
}

// IndustryWeight 行业分布（本期剔除，结构预留）
type IndustryWeight struct {
	Industry string  `json:"industry"` // 行业名称
	Weight   float64 `json:"weight"`   // 权重(%)
	AsOfDate string  `json:"asOfDate"` // 报告期 YYYY-MM-DD
}

// PremiumRatePoint 折溢价率历史点
type PremiumRatePoint struct {
	Date             string  `json:"date"`             // YYYY-MM-DD
	ClosePrice       float64 `json:"closePrice"`       // 收盘价
	NAV              float64 `json:"nav"`              // 单位净值
	ClosePremiumRate float64 `json:"closePremiumRate"` // (close−nav)/nav×100
	IntradayHigh     float64 `json:"intradayHighPremiumRate,omitempty"` // 盘中最高溢价率
	IntradayLow      float64 `json:"intradayLowPremiumRate,omitempty"`  // 盘中最低溢价率
}

// IOPVPoint IOPV 分时点
type IOPVPoint struct {
	Time string  `json:"time"` // 市场本地时间 HH:mm:ss
	IOPV float64 `json:"iopv"`
}

// ---------------------------------------------------------------------------
// 交易日历与市场状态
// ---------------------------------------------------------------------------

// MarketStatus 市场状态
type MarketStatus struct {
	Market      Market     `json:"market"`
	Status      TradePhase `json:"status"`
	StatusText  string     `json:"statusText"`
	IsTradeDay  bool       `json:"isTradeDay"`
	HolidayName string     `json:"holidayName"`
	Timezone    string     `json:"timezone"` // Asia/Shanghai
	LocalTime   string     `json:"localTime"`  // RFC3339 市场本地时间
	BeijingTime string     `json:"beijingTime"` // RFC3339
}

// TradingPeriod 交易时段
type TradingPeriod struct {
	Status  TradePhase `json:"status"`
	Text    string     `json:"text"`
	Start   string     `json:"startTime"` // 市场本地时间 HH:mm
	End     string     `json:"endTime"`
}

// TradingSchedule 交易时间表
type TradingSchedule struct {
	Market      Market          `json:"market"`
	IsTradeDay  bool            `json:"isTradeDay"`
	HolidayName string          `json:"holidayName"`
	Timezone    string          `json:"timezone"`
	Periods     []TradingPeriod `json:"periods"`
	TradeDates  []string        `json:"tradeDates,omitempty"` // YYYY-MM-DD
}

// TradingCalendar 交易日历接口
// 实现：CNTradingCalendar —— 由现有 MarketService 的
// isTradeDay / GetMarketStatus / GetTradingSchedule / GetTradeDates 迁入（逻辑不变）
type TradingCalendar interface {
	Market() Market
	IsTradeDay(date time.Time) (bool, string)
	GetMarketStatus(now time.Time) MarketStatus
	GetTradingSchedule() TradingSchedule
	GetTradeDates(days int) ([]string, error)
	// 扩展位：未来加新市场时补充
	// IsEarlyClose(date time.Time) bool
	// LocalTime(now time.Time) time.Time
}

// ---------------------------------------------------------------------------
// 风险提示
// ---------------------------------------------------------------------------

// RiskAlert 风险提示条目
// 强制约束：Trigger 必须含实测数值 —— 无实测值的告警等于噪音
type RiskAlert struct {
	Code       string `json:"code"`       // 如 ETF_PREMIUM_HIGH
	Level      string `json:"level"`      // info / warning / critical
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Trigger    string `json:"trigger"`    // 触发依据（含实测数值）
	Suggestion string `json:"suggestion"` // 处置建议
	AsOf       string `json:"asOf"`
}

// RiskProfile 风险提示集合
type RiskProfile struct {
	Symbol string      `json:"symbol"`
	Alerts []RiskAlert `json:"alerts"`
}

// ---------------------------------------------------------------------------
// 标的解析结果（symbol 解析的唯一出口）
// ---------------------------------------------------------------------------

// ResolvedSymbol 标的解析结果
// 铁律：symbol 解析只允许发生在一处，各数据源适配器只消费本结构
type ResolvedSymbol struct {
	Symbol       string       `json:"symbol"`
	Market       Market       `json:"market"`
	Code         string       `json:"code"` // 原始代码，如 600519 / 510300
	AssetType    AssetType    `json:"assetType"`
	SecurityType SecurityType `json:"securityType"`
	Exchange     string       `json:"exchange"`
	Currency     Currency     `json:"currency"`
}
