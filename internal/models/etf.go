package models

// 本文件定义 A股 ETF 专属数据模型（里程碑 M3）。
//
// 设计原则（见 docs/A股ETF数据源可得性探查报告.md §5.3）：
//   - ETF 专有字段不塞进通用 Stock 结构体，使用独立结构体，避免污染个股/指数模型。
//   - 行情层（ETFQuote）聚合"二级市价 + 交易所 IOPV + 折溢价率 + 涨跌停 + T+0 标识"，
//     二级市价可由 TDX 主源提供，IOPV 由交易所直连（M2）提供，净值序列由东财 lsjz（M3.2）提供。
//   - 折溢价率公式：溢价率 = (市价 - IOPV) / IOPV × 100%（正=溢价，负=折价）。

// ETFQuote ETF 实时行情聚合（二级市价 + IOPV + 折溢价 + 涨跌停 + T+0）。
type ETFQuote struct {
	Symbol         string    `json:"symbol"`         // 带市场前缀代码，如 sh510300 / sz159915
	Name           string    `json:"name"`           // 名称
	Exchange       string    `json:"exchange"`       // sh / sz
	MarketPrice    float64   `json:"marketPrice"`    // 二级市场最新价（TDX/交易所）
	PreClose       float64   `json:"preClose"`       // 昨收
	Open           float64   `json:"open"`           // 今开
	High           float64   `json:"high"`           // 最高
	Low            float64   `json:"low"`            // 最低
	Volume         int64     `json:"volume"`         // 成交量（股）
	Amount         float64   `json:"amount"`         // 成交额（元）
	Change         float64   `json:"change"`         // 涨跌额（市价 - 昨收）
	ChangePercent  float64   `json:"changePercent"`  // 涨跌幅（%，基于市价）
	IOPV           float64   `json:"iopv"`           // 基金份额参考净值（交易所直连）
	NAV            float64   `json:"nav,omitempty"`  // 单位净值（东财 lsjz，T-1；可选）
	PremiumRate    float64   `json:"premiumRate"`    // 折溢价率（%，(市价-IOPV)/IOPV×100；正=溢价）
	LimitUpPrice   float64   `json:"limitUpPrice"`   // 涨停价（10%/20% 按品种）
	LimitDownPrice float64   `json:"limitDownPrice"` // 跌停价
	T0Flag         bool      `json:"t0Flag"`         // 是否 T+0 回转交易
	OrderBook      OrderBook `json:"orderBook"`      // 五档盘口（深交所直连提供；沪市回退空）
	DateTime       string    `json:"dateTime"`       // 行情时间
}

// ETFNavPoint 单条净值记录（来自东财 lsjz）。
type ETFNavPoint struct {
	Date            string  `json:"date"`            // 净值日期 yyyy-MM-dd（FSRQ）
	UnitNav         float64 `json:"unitNav"`         // 单位净值（DWJZ）
	AccumNav        float64 `json:"accumNav"`        // 累计净值（LJJZ）
	ChangePercent   float64 `json:"changePercent"`   // 日涨跌（%，JZZZL）
	SubscribeStatus string  `json:"subscribeStatus"` // 申购状态（SGZT，如 场内买入）
	RedeemStatus    string  `json:"redeemStatus"`    // 赎回状态（SHZT，如 场内卖出）
}

// ETFNavSeries ETF 净值历史序列（东财 lsjz）。
type ETFNavSeries struct {
	Code   string        `json:"code"`   // 基金代码（不带前缀）
	Name   string        `json:"name"`   // 名称
	Points []ETFNavPoint `json:"points"` // 按日期升序的净值序列
}

// ETFListEntry ETF 清单条目（多源合并后的统一目录项）。
type ETFListEntry struct {
	Code       string `json:"code"`       // 6 位代码
	Name       string `json:"name"`       // 名称
	Exchange   string `json:"exchange"`   // sh / sz
	FundType   string `json:"fundType"`   // 基金类别（jjlb：ETF / LOF / 封闭式 / 不动产基金）
	InvestType string `json:"investType"` // 投资类别（tzlb：股票基金 / 债券基金 / 其它基金(跨境QDII) ...）
	ScaleWan   string `json:"scaleWan"`   // 当前规模（万份），原始字符串（深市有，沪市空）
	Manager    string `json:"manager"`    // 基金管理人（深市有，沪市空）
	ListDate   string `json:"listDate"`   // 上市日期
	T0Flag     bool   `json:"t0Flag"`     // 是否 T+0（深市按 tzlb，沪市按代码规则）
}

// ETFProfile ETF 静态画像（规模/管理人/分类/上市日 + 近期净值序列）。
//
// 说明：沪市 ETF 在交易所公开接口中拿不到规模/管理人（见 doc §7），故沪市条目这些字段为空；
// 深市条目由深交所基金列表（M2）补全 scaleWan/manager/fundType/investType。
type ETFProfile struct {
	Code       string        `json:"code"`
	Name       string        `json:"name"`
	Exchange   string        `json:"exchange"`
	FundType   string        `json:"fundType"`
	InvestType string        `json:"investType"`
	ScaleWan   string        `json:"scaleWan"`
	Manager    string        `json:"manager"`
	ListDate   string        `json:"listDate"`
	T0Flag     bool          `json:"t0Flag"`
	NavSeries  []ETFNavPoint `json:"navSeries,omitempty"` // 近期净值（来自东财 lsjz，可选）
}

// ETFIOPVPoint IOPV 分时序列单点（深交所 getTimeData 的 piciopv 提供；沪市仅有瞬时值，无分时）。
type ETFIOPVPoint struct {
	Time  string  `json:"time"`  // 时刻 HH:MM
	Value float64 `json:"value"` // IOPV 值
}

// ETFListResult 分页 ETF 目录结果（合并清单 + 关键词搜索 + 分类筛选）。
type ETFListResult struct {
	Total int              `json:"total"` // 筛选后总数
	Page  int              `json:"page"`  // 当前页（从 1 起）
	Size  int              `json:"size"`  // 每页条数
	Items []ETFListEntry   `json:"items"` // 本页条目
}
