package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/proxy"
)

// 本文件实现深圳证券交易所（SZSE）直连行情客户端。
//
// 背景（A股 ETF 支持 · 里程碑 M2）：深交所官网提供两个官方 JSON 接口：
//   1) 实时行情 getTimeData：www.szse.cn/api/market/ssjjhq/getTimeData
//      —— 含 IOPV（netValue）、五档盘口（sellbuy5）、IOPV 分时序列（piciopv）、
//         涨跌停判断（change20PerLimit）。注意：必须带 Referer=https://www.szse.cn/ 与
//         浏览器 UA，否则返回 401/空。
//   2) 基金列表 ShowReport：www.szse.cn/api/report/ShowReport/data?CATALOGID=1105
//      —— 含 tzlb（投资类别，用于 T+0 判断）、jjlb（基金类别）、上市日、规模、管理人。
//      总量约 1083，分 55 页；返回顶层 JSON 数组，需取其 tabkey=="tab1" 的条目。
//
// 两个接口复用应用层代理 Client，并显式补齐 Referer/UA。

const (
	szseGetTimeDataURLFmt = "https://www.szse.cn/api/market/ssjjhq/getTimeData?random=0.5&marketId=1&code=%s"
	szseFundListURLFmt    = "https://www.szse.cn/api/report/ShowReport/data?SHOWTYPE=JSON&CATALOGID=1105&TABKEY=tab1&PAGENO=%d"
	szseReferer           = "https://www.szse.cn/"
	szseUA                = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
)

// htmlTagRe 用于剔除深交所列表里 HTML 包裹（如 <a ...><u>158000</u></a>）。
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// SZSEIOPVPoint IOPV 分时序列中的单个数据点。
type SZSEIOPVPoint struct {
	Time  string  `json:"time"`  // 时刻 HH:MM
	Value float64 `json:"value"` // IOPV 值
}

// SZSEFundQuote 深交所基金（含 ETF）实时行情（含 IOPV 与盘口）。
type SZSEFundQuote struct {
	Code             string           `json:"code"`             // 6 位代码
	Name             string           `json:"name"`             // 名称
	Open             float64          `json:"open"`             // 今开
	PrevClose        float64          `json:"prevClose"`        // 昨收（接口字段名 close）
	Now              float64          `json:"now"`              // 最新价
	High             float64          `json:"high"`             // 最高
	Low              float64          `json:"low"`              // 最低
	Volume           int64            `json:"volume"`           // 成交量（股）
	Amount           float64          `json:"amount"`           // 成交额（元）
	Delta            float64          `json:"delta"`            // 涨跌额
	DeltaPercent     float64          `json:"deltaPercent"`     // 涨跌幅（%）
	NetValue         float64          `json:"netValue"`         // IOPV（基金份额参考净值）
	Change20PerLimit bool             `json:"change20PerLimit"` // 是否触发涨跌停（±20%）
	GroupID          int              `json:"groupId"`          //  группировка（深交所 T+0 判定无用，保留）
	TradingPhase1    string           `json:"tradingPhase1"`    // 交易阶段码1
	TradingPhase2    string           `json:"tradingPhase2"`    // 交易阶段码2
	OrderBook        models.OrderBook `json:"orderBook"`        // 五档盘口（买五 + 卖五）
	IOPVHistory      []SZSEIOPVPoint  `json:"iopvHistory"`      // IOPV 分时序列
	DateTime         string           `json:"dateTime"`         // 行情时间 YYYY-MM-DD HH:MM
}

// SZSEFundListItem 深交所基金列表项（字段来自 ShowReport 1105）。
type SZSEFundListItem struct {
	Code       string `json:"code"`       // 基金代码
	Name       string `json:"name"`       // 基金简称
	FundType   string `json:"fundType"`   // jjlb：ETF / LOF / 封闭式 / 不动产基金
	InvestType string `json:"investType"` // tzlb：股票基金 / 债券基金 / 货币市场基金 ...（T+0 判断依据）
	ListDate   string `json:"listDate"`   // ssrq：上市日期
	ScaleWan   string `json:"scaleWan"`   // dqgm：当前规模（万份），原始含千分位
	Manager    string `json:"manager"`    // glrmc：基金管理人
}

// SZSEExchangeClient 深交所直连行情客户端。
type SZSEExchangeClient struct {
	httpClient *http.Client
}

// NewSZSEExchangeClient 创建深交所直连客户端。
func NewSZSEExchangeClient() *SZSEExchangeClient {
	return &SZSEExchangeClient{
		httpClient: proxy.GetManager().GetClientWithTimeout(10 * time.Second),
	}
}

// FetchFundQuote 获取单只深交所基金的实时行情（含 IOPV、五档盘口、IOPV 分时）。
func (c *SZSEExchangeClient) FetchFundQuote(code string) (*SZSEFundQuote, error) {
	bare := bare6Code(code)
	url := fmt.Sprintf(szseGetTimeDataURLFmt, bare)

	body, err := c.get(url)
	if err != nil {
		return nil, err
	}
	return parseSZSEGetTimeData(body)
}

// FetchFundList 获取深交所基金列表第 page 页（每页 20 条，总计约 1083 条 / 55 页）。
// 返回本页条目与全量记录总数（recordcount）。
func (c *SZSEExchangeClient) FetchFundList(page int) ([]SZSEFundListItem, int, error) {
	if page < 1 {
		page = 1
	}
	url := fmt.Sprintf(szseFundListURLFmt, page)

	body, err := c.get(url)
	if err != nil {
		return nil, 0, err
	}
	return parseSZSEFundList(body)
}

// get 发送带 Referer / UA 的 GET 请求并返回响应体。
func (c *SZSEExchangeClient) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", szseReferer)
	req.Header.Set("User-Agent", szseUA)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("szse 请求失败 %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("szse 返回非 200 状态: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("szse 读取响应失败: %w", err)
	}
	return data, nil
}

// szseLevel getTimeData 中盘口单档：价格与量可能为字符串，也可能为 null。
type szseLevel struct {
	Price  interface{} `json:"price"`
	Volume interface{} `json:"volume"`
}

// szseGetTimeDataResp 对应 getTimeData 接口结构。
type szseGetTimeDataResp struct {
	Datetime string `json:"datetime"`
	Code     string `json:"code"`
	Data     struct {
		Code              string      `json:"code"`
		Name              string      `json:"name"`
		Close             string      `json:"close"`
		Open              string      `json:"open"`
		Now               string      `json:"now"`
		High              string      `json:"high"`
		Low               string      `json:"low"`
		Volume            interface{} `json:"volume"`
		Amount            interface{} `json:"amount"`
		Delta             string      `json:"delta"`
		DeltaPercent      string      `json:"deltaPercent"`
		NetValue          string      `json:"netValue"`
		GroupID           int         `json:"groupId"`
		Change20PerLimit  bool        `json:"change20PerLimit"`
		TradingPhaseCode1 string      `json:"tradingPhaseCode1"`
		TradingPhaseCode2 string      `json:"tradingPhaseCode2"`
		SellBuy5          []szseLevel `json:"sellbuy5"`
		Piciopv           [][]interface{} `json:"piciopv"`
	} `json:"data"`
}

// parseSZSEGetTimeData 解析 getTimeData 响应体。
//
// 注意：OHLC / IOPV / 涨跌幅 在接口中以"字符串"返回，成交量/额为"数字"，
// 盘口价格可能为字符串或 null。统一经 toFloat / toInt64 防御性转换。
func parseSZSEGetTimeData(body []byte) (*SZSEFundQuote, error) {
	var resp szseGetTimeDataResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("szse getTimeData 解析失败: %w", err)
	}
	d := resp.Data
	if d.Code == "" {
		return nil, fmt.Errorf("szse getTimeData 返回空数据（代码 %s 可能不存在或非交易日）", resp.Code)
	}

	q := &SZSEFundQuote{
		Code:             d.Code,
		Name:             d.Name,
		Open:             exToFloat(d.Open),
		PrevClose:        exToFloat(d.Close),
		Now:              exToFloat(d.Now),
		High:             exToFloat(d.High),
		Low:              exToFloat(d.Low),
		Volume:           exToInt64(d.Volume),
		Amount:           exToFloat(d.Amount),
		Delta:            exToFloat(d.Delta),
		DeltaPercent:     exToFloat(d.DeltaPercent),
		NetValue:         exToFloat(d.NetValue),
		Change20PerLimit: d.Change20PerLimit,
		GroupID:          d.GroupID,
		TradingPhase1:    d.TradingPhaseCode1,
		TradingPhase2:    d.TradingPhaseCode2,
		DateTime:         resp.Datetime,
	}

	// 盘口：sellbuy5 共 10 档，前 5 档为买一~买五，后 5 档为卖一~卖五。
	// 单档 price 可能为 null（无报价），需跳过。
	q.OrderBook = parseSZSEOrderBook(d.SellBuy5)

	// IOPV 分时序列：每项为 [HH:MM, value]。
	q.IOPVHistory = parseSZSEIOPVHistory(d.Piciopv)

	return q, nil
}

// parseSZSEOrderBook 将 10 档 sellbuy5 拆为卖五 + 买五。
//
// 顺序（经实盘验证，2026-10-30 深交所 159919）：sellbuy5 为
// [卖一,卖二,卖三,卖四,卖五, 买一,买二,买三,买四,买五]，
// 即名称 "sell" 在前、"buy" 在后。以 now=4.237 为例，前 5 档价格
// 4.238~4.246（高于现价，委卖），后 5 档 4.237~4.233（低于现价，委买）。
func parseSZSEOrderBook(levels []szseLevel) models.OrderBook {
	bids := make([]models.OrderBookItem, 0, 5)
	asks := make([]models.OrderBookItem, 0, 5)
	for i, lv := range levels {
		price := exToFloat(lv.Price)
		size := exToInt64(lv.Volume)
		if price <= 0 {
			continue
		}
		item := models.OrderBookItem{Price: price, Size: size}
		if i < 5 {
			asks = append(asks, item) // 卖一~卖五
		} else {
			bids = append(bids, item) // 买一~买五
		}
	}
	return models.OrderBook{Bids: bids, Asks: asks}
}

// parseSZSEIOPVHistory 解析 IOPV 分时序列。
func parseSZSEIOPVHistory(raw [][]interface{}) []SZSEIOPVPoint {
	points := make([]SZSEIOPVPoint, 0, len(raw))
	for _, row := range raw {
		if len(row) < 2 {
			continue
		}
		t, _ := row[0].(string)
		points = append(points, SZSEIOPVPoint{Time: t, Value: exToFloat(row[1])})
	}
	return points
}

// szseFundListEnvelope ShowReport 返回的顶层数组中的单条（含 metadata + data）。
type szseFundListEnvelope struct {
	Metadata struct {
		TabKey       string `json:"tabkey"`
		PageCount    int    `json:"pagecount"`
		RecordCount  int    `json:"recordcount"`
		PageNo       int    `json:"pageno"`
	} `json:"metadata"`
	Data []struct {
		SysKey  string `json:"sys_key"`
		Jjjcurl string `json:"jjjcurl"`
		Jjlb    string `json:"jjlb"`
		Tzlb    string `json:"tzlb"`
		Ssrq    string `json:"ssrq"`
		Dqgm    string `json:"dqgm"`
		Glrmc   string `json:"glrmc"`
	} `json:"data"`
}

// parseSZSEFundList 解析基金列表响应体。
//
// 接口返回顶层 JSON 数组（通常 2 个元素，对应不同 tab），取 tabkey=="tab1" 的那条。
// 代码与名称在接口里被 HTML 包裹（<a ...><u>xxx</u></a>），需剔除标签。
func parseSZSEFundList(body []byte) ([]SZSEFundListItem, int, error) {
	var envelopes []szseFundListEnvelope
	if err := json.Unmarshal(body, &envelopes); err != nil {
		return nil, 0, fmt.Errorf("szse 基金列表解析失败: %w", err)
	}

	var chosen *szseFundListEnvelope
	for i := range envelopes {
		if envelopes[i].Metadata.TabKey == "tab1" {
			chosen = &envelopes[i]
			break
		}
	}
	if chosen == nil && len(envelopes) > 0 {
		chosen = &envelopes[0]
	}
	if chosen == nil {
		return nil, 0, fmt.Errorf("szse 基金列表无有效数据")
	}

	items := make([]SZSEFundListItem, 0, len(chosen.Data))
	for _, rec := range chosen.Data {
		items = append(items, SZSEFundListItem{
			Code:       stripHTML(rec.SysKey),
			Name:       stripHTML(rec.Jjjcurl),
			FundType:   strings.TrimSpace(rec.Jjlb),
			InvestType: strings.TrimSpace(rec.Tzlb),
			ListDate:   strings.TrimSpace(rec.Ssrq),
			ScaleWan:   strings.TrimSpace(rec.Dqgm),
			Manager:    strings.TrimSpace(rec.Glrmc),
		})
	}
	return items, chosen.Metadata.RecordCount, nil
}

// stripHTML 剔除字符串中的 HTML 标签并压缩空白。
func stripHTML(s string) string {
	return strings.TrimSpace(htmlTagRe.ReplaceAllString(s, ""))
}

// parseScaleWan 将规模字段（含千分位逗号，如 "3,192.58"）解析为 float64（万元）。
// 仅在需要时调用，列表项默认保留原始字符串。
func parseScaleWan(s string) (float64, error) {
	clean := strings.NewReplacer(",", "", " ", "").Replace(strings.TrimSpace(s))
	return strconv.ParseFloat(clean, 64)
}
