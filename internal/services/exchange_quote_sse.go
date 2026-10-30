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

	"github.com/injoyai/tdx/protocol"
	"github.com/run-bigpig/jcp/internal/pkg/proxy"
)

// 本文件实现上海证券交易所（SSE）直连行情客户端。
//
// 背景（A股 ETF 支持 · 里程碑 M2）：交易所官网是最权威的 ETF 实时数据源，
// 尤其提供 TDX / 新浪等行情源不直接给的 IOPV（基金份额参考净值）。
// SSE 提供两个官方 HTTP 接口：
//   1) 实时快照 snap：yunhq.sse.com.cn:32041/v1/sh1/snap/{code}（含 iopv 字段）
//   2) ETF 列表：     yunhq.sse.com.cn:32041/v1/sh1/list/exchange/etf（总量 947）
// 两个接口均为 HTTP 明文返回，无需鉴权，直接复用应用层代理 Client 即可。
//
// 说明：SSE snap 仅覆盖沪市（sh1），故入参代码统一提取 6 位纯数字后拼接。

const (
	sseSnapURLFmt = "http://yunhq.sse.com.cn:32041/v1/sh1/snap/%s?select=name,last,prev_close,open,high,low,volume,amount,tradephase,iopv,nav"
	sseETFListURL = "http://yunhq.sse.com.cn:32041/v1/sh1/list/exchange/etf?select=code,name&page=1"
)

// sseBareCodeRe 提取代码中的 6 位连续数字（sh510300 / 510300 / sz159915 均取其数字部分）。
var sseBareCodeRe = regexp.MustCompile(`(\d{6})`)

func bare6Code(code string) string {
	if m := sseBareCodeRe.FindStringSubmatch(protocol.AddPrefix(code)); m != nil {
		return m[1]
	}
	return strings.TrimSpace(code)
}

// SSEETFQuote 上交所 ETF 实时快照（含 IOPV）。
type SSEETFQuote struct {
	Code       string  `json:"code"`       // 6 位代码（如 510300）
	Name       string  `json:"name"`       // 名称（如 300ETF）
	Last       float64 `json:"last"`       // 最新价
	PrevClose  float64 `json:"prevClose"`  // 昨收
	Open       float64 `json:"open"`       // 今开
	High       float64 `json:"high"`       // 最高
	Low        float64 `json:"low"`        // 最低
	Volume     int64   `json:"volume"`     // 成交量（股）
	Amount     float64 `json:"amount"`     // 成交额（元）
	TradePhase string  `json:"tradePhase"` // 交易阶段（如 U111 表示连续竞价）
	IOPV       float64 `json:"iopv"`       // 基金份额参考净值（IOPV）
	NAV        float64 `json:"nav"`        // 净值（沪市该接口恒为 0，IOPV 用 iopv 字段）
	Date       int     `json:"date"`       // 交易日期 yyyymmdd
	Time       int     `json:"time"`       // 交易时间 hhmmss
}

// SSEETFListItem 上交所 ETF 列表项。
type SSEETFListItem struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// SSEExchangeClient 上交所直连行情客户端。
type SSEExchangeClient struct {
	httpClient *http.Client
}

// NewSSEExchangeClient 创建上交所直连客户端。
// 复用应用层代理管理器的带超时 Client（默认 ProxyModeNone，不注入任何代理）。
func NewSSEExchangeClient() *SSEExchangeClient {
	return &SSEExchangeClient{
		httpClient: proxy.GetManager().GetClientWithTimeout(10 * time.Second),
	}
}

// FetchETFQuote 获取单只上交所 ETF 的实时快照（含 IOPV）。
func (c *SSEExchangeClient) FetchETFQuote(code string) (*SSEETFQuote, error) {
	bare := bare6Code(code)
	url := fmt.Sprintf(sseSnapURLFmt, bare)

	body, err := c.get(url)
	if err != nil {
		return nil, err
	}
	return parseSSESnap(body)
}

// FetchETFList 获取上交所全部 ETF 列表（接口一次返回全部，当前总量约 947）。
func (c *SSEExchangeClient) FetchETFList() ([]SSEETFListItem, error) {
	body, err := c.get(sseETFListURL)
	if err != nil {
		return nil, err
	}
	return parseSSEETFList(body)
}

// get 发送 GET 请求并返回响应体。
func (c *SSEExchangeClient) get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// 上交所 yunhq 接口对 UA 无强制要求，但带上更稳妥。
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; jcp/1.0)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sse 请求失败 %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sse 返回非 200 状态: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sse 读取响应失败: %w", err)
	}
	return data, nil
}

// sseSnapResp 对应 snap 接口的完整响应结构。
// snap 为变长数组，顺序固定：name,last,prev_close,open,high,low,volume,amount,tradephase,iopv,nav。
type sseSnapResp struct {
	Code string        `json:"code"`
	Date int           `json:"date"`
	Time int           `json:"time"`
	Snap []interface{} `json:"snap"`
}

// parseSSESnap 解析 snap 响应体。
func parseSSESnap(body []byte) (*SSEETFQuote, error) {
	var resp sseSnapResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("sse snap 解析失败: %w", err)
	}
	if len(resp.Snap) < 11 {
		return nil, fmt.Errorf("sse snap 字段不足，期望 >=11，实际 %d", len(resp.Snap))
	}

	s := resp.Snap
	q := &SSEETFQuote{
		Code:       resp.Code,
		Date:       resp.Date,
		Time:       resp.Time,
		Name:       exToString(s[0]),
		Last:       exToFloat(s[1]),
		PrevClose:  exToFloat(s[2]),
		Open:       exToFloat(s[3]),
		High:       exToFloat(s[4]),
		Low:        exToFloat(s[5]),
		Volume:     exToInt64(s[6]),
		Amount:     exToFloat(s[7]),
		TradePhase: strings.TrimSpace(exToString(s[8])),
		IOPV:       exToFloat(s[9]),
		NAV:        exToFloat(s[10]),
	}
	return q, nil
}

// sseETFListResp 对应 ETF 列表接口的响应结构。
type sseETFListResp struct {
	Total int         `json:"total"`
	Begin int         `json:"begin"`
	End   int         `json:"end"`
	List  [][]string  `json:"list"`
}

// parseSSEETFList 解析 ETF 列表响应体。
func parseSSEETFList(body []byte) ([]SSEETFListItem, error) {
	var resp sseETFListResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("sse etf 列表解析失败: %w", err)
	}
	items := make([]SSEETFListItem, 0, len(resp.List))
	for _, pair := range resp.List {
		if len(pair) < 2 {
			continue
		}
		items = append(items, SSEETFListItem{Code: pair[0], Name: pair[1]})
	}
	return items, nil
}

// exToFloat 将 JSON 反序列化后的混合类型（float64 / string / nil）统一转为 float64。
func exToFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}

// exToInt64 将 JSON 反序列化后的混合类型统一转为 int64。
func exToInt64(v interface{}) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case float32:
		return int64(t)
	case string:
		// 可能带千分位逗号（如 "3,192.58" 不会出现在成交量，但防御性处理）。
		clean := strings.NewReplacer(",", "", " ", "").Replace(strings.TrimSpace(t))
		i, _ := strconv.ParseInt(clean, 10, 64)
		return i
	case int:
		return int64(t)
	case int64:
		return t
	default:
		return 0
	}
}

// exToString 将 JSON 反序列化后的混合类型统一转为字符串。
func exToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
