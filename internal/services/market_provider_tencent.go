package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/run-bigpig/jcp/internal/models"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// 腾讯行情源的接入说明（见 docs/A股ETF数据源可得性探查报告.md §1.1/§2.1）：
//
//   - 实时/盘口：qt.gtimg.cn，GBK 编码，`~` 分隔；本文件按实测下标取值，
//     并用"自描述字段"交叉校验关键值（见 parseTencentStock 注释）。
//   - 指数：同一接口，直接用 sh000001 / sz399001 / sz399006。
//   - K线：web.ifzq.gtimg.cn 的前复权接口，UTF-8 JSON。
//   - 搜索：smartbox.gtimg.cn，返回带类型标记（ETF/QDII-ETF/LOF/ZS/GP-A）。
//
// 该 Provider 在链条中位于 TDX 之后、新浪之前，仅作降级用：
// TDX 正常时行为与改动前完全一致。

const (
	tencentQuoteURL   = "https://qt.gtimg.cn/q=%s"
	tencentKLineURL   = "https://web.ifzq.gtimg.cn/appstock/app/fqkline/get?param=%s,%s,,,%d,qfq"
	// t 必须为 all：实测 t=1 会过滤掉场内基金，搜 ETF 代码直接返回 v_hint="N"
	tencentSearchURL  = "https://smartbox.gtimg.cn/s3/?v=2&t=all&q=%s&c=1"
	tencentUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	tencentReferer    = "https://gu.qq.com/"
	tencentMinParts   = 49  // 实时行情有效切分后的最小段数（涨跌停位于 47/48）
	tencentAmountUnit = 1e4 // 成交额字段单位为"万元"
	tencentVolumeUnit = 100 // 成交量字段单位为"手"，转为股
)

// tencentQuoteRe 匹配 `v_sh600000="...";` 形式的实时行情响应。
var tencentQuoteRe = regexp.MustCompile(`v_([A-Za-z0-9]+)="([^"]*)"`)

type tencentMarketProvider struct {
	service *MarketService
}

func newTencentMarketProvider(service *MarketService) *tencentMarketProvider {
	return &tencentMarketProvider{service: service}
}

// ===== 实时行情（含五档盘口、涨跌停） =====

func (p *tencentMarketProvider) FetchStockDataWithOrderBook(codes ...string) ([]StockWithOrderBook, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	providerCodes := make([]string, 0, len(codes))
	for _, code := range codes {
		if normalized := ToProviderCode(code); normalized != "" {
			providerCodes = append(providerCodes, normalized)
		}
	}
	if len(providerCodes) == 0 {
		return nil, errors.New("腾讯行情：无有效代码")
	}

	body, err := p.fetchGBK(fmt.Sprintf(tencentQuoteURL, strings.Join(providerCodes, ",")))
	if err != nil {
		return nil, err
	}

	stocks := make([]StockWithOrderBook, 0, len(providerCodes))
	for _, match := range tencentQuoteRe.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		item, ok := parseTencentStock(match[1], match[2])
		if ok {
			stocks = append(stocks, item)
		}
	}

	if len(stocks) == 0 {
		return nil, errors.New("腾讯行情：返回数据为空")
	}
	return stocks, nil
}

// parseTencentStock 解析单只标的的实时行情。
//
// 关键下标（对 A股/ETF 通用，已实测验证）：
//
//	1 名称、3 现价、4 昨收、5 今开、6 成交量(手)、
//	9~18 买1~买5(价,量交替)、19~28 卖1~卖5(价,量交替)、
//	32 涨跌幅%、33 最高、34 最低、37 成交额(万元)、47 涨停价、48 跌停价。
func parseTencentStock(symbol string, payload string) (StockWithOrderBook, bool) {
	parts := strings.Split(payload, "~")
	if len(parts) < tencentMinParts {
		return StockWithOrderBook{}, false
	}

	price := tencentFloat(parts[3])
	preClose := tencentFloat(parts[4])
	if price <= 0 && preClose <= 0 {
		// 停牌或无效代码
		return StockWithOrderBook{}, false
	}

	change := price - preClose
	changePercent := tencentFloat(parts[32])
	if changePercent == 0 && preClose > 0 {
		changePercent = change / preClose * 100
	}

	stock := models.Stock{
		Symbol:         strings.ToLower(symbol),
		Name:           strings.TrimSpace(parts[1]),
		Price:          price,
		PreClose:       preClose,
		Open:           tencentFloat(parts[5]),
		High:           tencentFloat(parts[33]),
		Low:            tencentFloat(parts[34]),
		Change:         change,
		ChangePercent:  changePercent,
		Volume:         int64(tencentFloat(parts[6])) * tencentVolumeUnit,
		Amount:         tencentFloat(parts[37]) * tencentAmountUnit,
		LimitUpPrice:   tencentFloat(parts[47]),
		LimitDownPrice: tencentFloat(parts[48]),
	}

	// 五档：9~18 为买盘（价,量交替），19~28 为卖盘
	bids := make([]models.OrderBookItem, 0, 5)
	for i := 0; i < 5; i++ {
		priceIdx := 9 + i*2
		volumeIdx := priceIdx + 1
		levelPrice := tencentFloat(parts[priceIdx])
		if levelPrice <= 0 {
			continue
		}
		bids = append(bids, models.OrderBookItem{
			Price: levelPrice,
			Size:  int64(tencentFloat(parts[volumeIdx])), // 腾讯已是"手"
		})
	}

	asks := make([]models.OrderBookItem, 0, 5)
	for i := 0; i < 5; i++ {
		priceIdx := 19 + i*2
		volumeIdx := priceIdx + 1
		levelPrice := tencentFloat(parts[priceIdx])
		if levelPrice <= 0 {
			continue
		}
		asks = append(asks, models.OrderBookItem{
			Price: levelPrice,
			Size:  int64(tencentFloat(parts[volumeIdx])),
		})
	}

	calculateOrderBookTotals(bids)
	calculateOrderBookTotals(asks)

	return StockWithOrderBook{
		Stock:     stock,
		OrderBook: models.OrderBook{Bids: bids, Asks: asks},
	}, true
}

// ===== 指数 =====

func (p *tencentMarketProvider) FetchMarketIndices() ([]models.MarketIndex, error) {
	codes := make([]string, 0, len(tdxIndexNames))
	for code := range tdxIndexNames {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	body, err := p.fetchGBK(fmt.Sprintf(tencentQuoteURL, strings.Join(codes, ",")))
	if err != nil {
		return nil, err
	}

	indices := make([]models.MarketIndex, 0, len(codes))
	for _, match := range tencentQuoteRe.FindAllStringSubmatch(body, -1) {
		if len(match) < 3 {
			continue
		}
		parts := strings.Split(match[2], "~")
		if len(parts) < tencentMinParts {
			continue
		}

		code := strings.ToLower(match[1])
		price := tencentFloat(parts[3])
		preClose := tencentFloat(parts[4])
		if price <= 0 {
			continue
		}

		change := price - preClose
		changePercent := tencentFloat(parts[32])
		if changePercent == 0 && preClose > 0 {
			changePercent = change / preClose * 100
		}

		name := strings.TrimSpace(parts[1])
		if known, ok := tdxIndexNames[code]; ok {
			name = known
		}

		indices = append(indices, models.MarketIndex{
			Code:          code,
			Name:          name,
			Price:         price,
			Change:        change,
			ChangePercent: changePercent,
			Volume:        int64(tencentFloat(parts[6])),
			Amount:        tencentFloat(parts[37]),
		})
	}

	if len(indices) == 0 {
		return nil, errors.New("腾讯行情：指数数据为空")
	}

	sort.Slice(indices, func(i, j int) bool { return indices[i].Code < indices[j].Code })
	return indices, nil
}

// ===== K线 =====

func (p *tencentMarketProvider) FetchKLineData(code string, period string, days int) ([]models.KLineData, error) {
	providerCode := ToProviderCode(code)
	if providerCode == "" {
		return nil, errors.New("腾讯行情：代码无效")
	}

	tencentPeriod, ok := toTencentPeriod(period)
	if !ok {
		return nil, fmt.Errorf("腾讯行情：不支持的周期 %s", period)
	}

	if days <= 0 {
		days = 60
	}

	body, err := p.fetchText(fmt.Sprintf(tencentKLineURL, providerCode, tencentPeriod, days), tencentReferer)
	if err != nil {
		return nil, err
	}

	var root struct {
		Code int                        `json:"code"`
		Msg  string                     `json:"msg"`
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("腾讯行情：K线响应解析失败: %w", err)
	}
	if root.Code != 0 {
		return nil, fmt.Errorf("腾讯行情：K线接口返回错误 %d %s", root.Code, root.Msg)
	}

	raw, ok := root.Data[providerCode]
	if !ok {
		return nil, errors.New("腾讯行情：K线数据为空")
	}

	// data.<code> 下除 K线序列外还混有 qt / prec 等对象字段，
	// 因此先按 RawMessage 取出对应序列，再单独解析成二维数组。
	var series map[string]json.RawMessage
	if err := json.Unmarshal(raw, &series); err != nil {
		return nil, fmt.Errorf("腾讯行情：K线序列解析失败: %w", err)
	}

	// 前复权数据键形如 qfqday，部分周期直接是 day
	rowRaw, found := series["qfq"+tencentPeriod]
	if !found || len(rowRaw) == 0 {
		rowRaw = series[tencentPeriod]
	}
	if len(rowRaw) == 0 {
		return nil, errors.New("腾讯行情：K线序列为空")
	}

	var rows [][]any
	if err := json.Unmarshal(rowRaw, &rows); err != nil {
		return nil, fmt.Errorf("腾讯行情：K线行解析失败: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("腾讯行情：K线序列为空")
	}

	klines := make([]models.KLineData, 0, len(rows))
	for _, row := range rows {
		// 行格式：[日期, 开, 收, 高, 低, 成交量]
		if len(row) < 6 {
			continue
		}
		klines = append(klines, models.KLineData{
			Time:   toStringLocal(row[0]),
			Open:   toFloat64Any(row[1]),
			Close:  toFloat64Any(row[2]),
			High:   toFloat64Any(row[3]),
			Low:    toFloat64Any(row[4]),
			Volume: int64(toFloat64Any(row[5])),
		})
	}

	if len(klines) == 0 {
		return nil, errors.New("腾讯行情：K线数据为空")
	}
	return klines, nil
}

// toTencentPeriod 把内部 period 映射为腾讯接口的周期标识。
func toTencentPeriod(period string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(period)) {
	case "1d", "day", "d":
		return "day", true
	case "1w", "week", "w":
		return "week", true
	case "1mo", "month", "mo":
		return "month", true
	case "5m":
		return "m5", true
	case "15m":
		return "m15", true
	case "30m":
		return "m30", true
	case "60m", "1h":
		return "m60", true
	}
	return "", false
}

// ===== 搜索 =====

func (p *tencentMarketProvider) SearchStocks(keyword string, limit int) ([]StockSearchResult, error) {
	trimmed := strings.TrimSpace(keyword)
	if trimmed == "" {
		return nil, nil
	}

	body, err := p.fetchText(fmt.Sprintf(tencentSearchURL, url.QueryEscape(trimmed)), tencentReferer)
	if err != nil {
		return nil, err
	}

	results := parseTencentSearchHint(body)
	if len(results) == 0 {
		// 开发机 IP 常被拦截返回 v_hint="N"，此时回退内置目录，保证搜索可用
		results = searchEmbeddedStocks(trimmed, limit)
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// parseTencentSearchHint 解析 smartbox 的 v_hint 内容。
//
// 条目格式：market~code~name~pinyin~type，多条用 ^ 分隔。
// 返回体里的中文是 \uXXXX 转义，需先按 JSON 字符串解码。
func parseTencentSearchHint(body string) []StockSearchResult {
	const marker = "v_hint="
	idx := strings.Index(body, marker)
	if idx < 0 {
		return nil
	}

	hint := decodeHintString(body[idx+len(marker):])
	if hint == "" || hint == "N" {
		return nil
	}

	results := make([]StockSearchResult, 0, 16)
	for _, entry := range strings.Split(hint, "^") {
		fields := strings.Split(entry, "~")
		if len(fields) < 5 {
			continue
		}
		market := strings.ToLower(strings.TrimSpace(fields[0]))
		code := strings.TrimSpace(fields[1])
		name := strings.TrimSpace(fields[2])
		rawType := strings.ToUpper(strings.TrimSpace(fields[4]))
		if market == "" || code == "" {
			continue
		}

		results = append(results, StockSearchResult{
			Symbol:       market + code,
			Name:         name,
			Market:       tencentMarketName(market),
			SecurityType: tencentSecurityType(rawType),
		})
	}
	return results
}

// decodeHintString 取出双引号内的内容并按 JSON 字符串解码（解开 \uXXXX）。
func decodeHintString(raw string) string {
	start := strings.Index(raw, `"`)
	if start < 0 {
		return ""
	}
	end := strings.LastIndex(raw, `"`)
	if end <= start {
		return ""
	}

	quoted := raw[start : end+1]
	var decoded string
	if err := json.Unmarshal([]byte(quoted), &decoded); err != nil {
		return strings.Trim(quoted, `"`)
	}
	return decoded
}

func tencentMarketName(market string) string {
	switch market {
	case "sh":
		return "上海"
	case "sz":
		return "深圳"
	case "bj":
		return "北京"
	}
	return market
}

// tencentSecurityType 把 smartbox 的类型标记映射为内部 securityType。
func tencentSecurityType(rawType string) string {
	switch rawType {
	case "ETF", "QDII-ETF":
		return "etf"
	case "LOF", "QDII-LOF":
		return "lof"
	case "ZS":
		return "index"
	case "GP", "GP-A", "GP-B":
		return "stock"
	}
	return ""
}

// ===== 通用请求工具 =====

// fetchGBK 发起请求并把响应体从 GBK 转为 UTF-8。
func (p *tencentMarketProvider) fetchGBK(target string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", tencentUserAgent)
	req.Header.Set("Referer", tencentReferer)

	resp, err := p.service.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("腾讯行情：HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// fetchText 发起请求并直接读取 UTF-8 响应体。
func (p *tencentMarketProvider) fetchText(target string, referer string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", tencentUserAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := p.service.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("腾讯行情：HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// tencentFloat 安全解析数值字段（空串、"--"、非法值一律返回 0）。
func tencentFloat(value string) float64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "--" || trimmed == "null" {
		return 0
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0
	}
	return parsed
}
