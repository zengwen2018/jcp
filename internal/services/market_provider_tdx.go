package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
)

var tdxIndexNames = map[string]string{
	"sh000001": "上证指数",
	"sz399001": "深证成指",
	"sz399006": "创业板指",
}

type stockCatalogCache struct {
	UpdatedAt time.Time           `json:"updatedAt"`
	Items     []StockSearchResult `json:"items"`
}

// tdxCodeCatalogCache TDX 代码目录缓存（价格换算所需的 Decimal 等信息）。
//
// 与 stockCatalogCache 分开存放：搜索目录关心"名称与市场"，
// 而换算目录关心"Decimal/Multiple"，两者的刷新节奏与体积都不同。
type tdxCodeCatalogCache struct {
	UpdatedAt time.Time               `json:"updatedAt"`
	Entries   map[string]tdxCodeEntry `json:"entries"`
}

type tdxMarketProvider struct {
	mu           sync.RWMutex
	clientMu     sync.Mutex
	client       tdxClient
	dialClient   func() (tdxClient, error)
	catalog      []StockSearchResult
	nameBySymbol map[string]string
	updatedAt    time.Time
	cacheTTL     time.Duration
	cachePath    string

	// codes 是自维护的 tdx.ICodes 实现，为 tdx 库提供价格换算所需的 Decimal。
	// 详见 tdx_codes.go 的说明。
	codes *tdxSelfCodes
	// codesInstalled 标记 tdx.DefaultCodes 是否已安装，避免重复写全局变量。
	codesInstalled atomic.Bool
	// codesMu 保护 codes 的安装与权威目录加载过程。
	codesMu sync.Mutex
	// codeCatalogReady 标记权威目录是否已加载完成。
	codeCatalogReady atomic.Bool
	// codeCatalogPath 是代码目录缓存文件路径。
	codeCatalogPath string
}

func newTDXMarketProvider() *tdxMarketProvider {
	provider := &tdxMarketProvider{
		nameBySymbol:   make(map[string]string),
		cacheTTL:       12 * time.Hour,
		cachePath:      filepath.Join(paths.EnsureCacheDir("market"), "tdx_stock_catalog.json"),
		codeCatalogPath: filepath.Join(paths.EnsureCacheDir("market"), "tdx_code_catalog.json"),
		codes:          newTDXSelfCodes(),
		dialClient: func() (tdxClient, error) {
			return tdx.DialDefault(tdx.WithRedial())
		},
	}
	// 尽早安装 tdx.DefaultCodes：只要有 ETF 进入报价链路，
	// tdx 库就会调用它做价格换算，未安装会让整批报价失败。
	provider.ensureCodesInstalled()
	return provider
}

// ensureCodesInstalled 把自维护的目录实现安装到 tdx.DefaultCodes。
//
// tdx 库是全局单例，这里只在首次调用时写入一次。
// 安装本身不依赖网络，权威目录在后台异步加载，因此不会阻塞首次行情请求。
func (p *tdxMarketProvider) ensureCodesInstalled() {
	if p.codesInstalled.Load() {
		return
	}
	p.codesMu.Lock()
	defer p.codesMu.Unlock()
	if p.codesInstalled.Load() {
		return
	}
	tdx.DefaultCodes = p.codes
	p.codesInstalled.Store(true)
	go p.refreshCodeCatalog()
}

// refreshCodeCatalog 加载权威代码目录（Decimal 等）。
//
// 优先读本地缓存；缓存缺失或过期时向 TDX 拉取全量目录并落盘。
// 该过程在后台执行，失败不影响行情主链路 —— 规则兜底已能覆盖 ETF 与指数。
func (p *tdxMarketProvider) refreshCodeCatalog() {
	if p.codeCatalogReady.Load() {
		return
	}
	p.codesMu.Lock()
	defer p.codesMu.Unlock()
	if p.codeCatalogReady.Load() {
		return
	}

	if cache, err := p.loadCodeCatalogCache(); err == nil && len(cache.Entries) > 0 && time.Since(cache.UpdatedAt) < p.cacheTTL {
		p.codes.setEntries(cache.Entries)
		p.codeCatalogReady.Store(true)
		return
	}

	entries, err := p.fetchCodeCatalogFromTDX()
	if err != nil {
		log.Warn("加载 TDX 代码目录失败，将使用规则兜底推断 Decimal: %v", err)
		return
	}
	p.codes.setEntries(entries)
	p.codeCatalogReady.Store(true)
	p.saveCodeCatalogCache(entries)
}

// fetchCodeCatalogFromTDX 拉取 TDX 全量目录，返回 symbol -> 目录条目。
func (p *tdxMarketProvider) fetchCodeCatalogFromTDX() (map[string]tdxCodeEntry, error) {
	cli, err := p.getClient()
	if err != nil {
		return nil, err
	}

	entries := make(map[string]tdxCodeEntry, 60000)
	for _, exchange := range []protocol.Exchange{protocol.ExchangeSH, protocol.ExchangeSZ, protocol.ExchangeBJ} {
		resp, err := cli.GetCodeAll(exchange)
		if err != nil {
			return nil, err
		}
		for _, item := range resp.List {
			symbol := exchange.String() + item.Code
			entries[symbol] = tdxCodeEntry{
				Name:     strings.TrimSpace(item.Name),
				Code:     item.Code,
				Exchange: exchange.String(),
				Multiple: item.Multiple,
				Decimal:  item.Decimal,
			}
		}
	}
	return entries, nil
}

func (p *tdxMarketProvider) loadCodeCatalogCache() (*tdxCodeCatalogCache, error) {
	data, err := os.ReadFile(p.codeCatalogPath)
	if err != nil {
		return nil, err
	}
	var cache tdxCodeCatalogCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func (p *tdxMarketProvider) saveCodeCatalogCache(entries map[string]tdxCodeEntry) {
	cache := tdxCodeCatalogCache{
		UpdatedAt: time.Now(),
		Entries:   entries,
	}
	data, err := json.Marshal(cache)
	if err != nil {
		log.Warn("保存 TDX 代码目录缓存失败: %v", err)
		return
	}
	if err := os.WriteFile(p.codeCatalogPath, data, 0644); err != nil {
		log.Warn("写入 TDX 代码目录缓存失败: %v", err)
	}
}

// tdx 报价合理区间。
//
// TDX 价格字段的换算错误（例如 Decimal 取错导致 ETF 价格被放大 10 倍）
// 会产生量级明显异常的数值。必须在返回前拦截，否则会把错价推到前端与 AI 工具。
// 上界 10000 覆盖了沪市价格最高的 ETF（货币 ETF 约 100 元）与全部个股；
// 下界取 0 是因为停牌或无成交时价格合法地为 0。
const (
	tdxMinPlausiblePrice = 0.0
	tdxMaxPlausiblePrice = 10000.0
)

// isPlausibleQuotePrice 判断价格是否在合理区间内。
func isPlausibleQuotePrice(price float64) bool {
	return price >= tdxMinPlausiblePrice && price <= tdxMaxPlausiblePrice
}

func (p *tdxMarketProvider) FetchStockDataWithOrderBook(codes ...string) ([]StockWithOrderBook, error) {
	// tdx 库在报价非个股时需要 DefaultCodes 做价格换算，未安装会让整批失败。
	p.ensureCodesInstalled()

	cli, err := p.getClient()
	if err != nil {
		return nil, err
	}

	// 个股与场内基金必须分组请求。
	// tdx 库的 GetQuote 是全有全无的：只要有一个代码无法解析（目录缺失、
	// Decimal 未知等），整个调用就返回错误，同批次的正常个股会一起失败。
	// 分组后两类互不影响，组内再由 fetchQuotesResilient 逐个降级兜底。
	stockCodes := make([]string, 0, len(codes))
	fundCodes := make([]string, 0, len(codes))
	for _, code := range codes {
		if protocol.IsStock(code) {
			stockCodes = append(stockCodes, code)
		} else {
			fundCodes = append(fundCodes, code)
		}
	}

	quotes := make(protocol.QuotesResp, 0, len(codes))
	quotes = append(quotes, p.fetchQuotesResilient(cli, stockCodes)...)
	quotes = append(quotes, p.fetchQuotesResilient(cli, fundCodes)...)

	if len(quotes) == 0 {
		return nil, errors.New("tdx returned empty quote data")
	}

	if _, err := p.loadCatalog(); err != nil {
		log.Warn("加载 TDX 股票目录失败，名称将回退为代码: %v", err)
	}

	stocks := make([]StockWithOrderBook, 0, len(quotes))
	for _, quote := range quotes {
		symbol := quote.Exchange.String() + quote.Code
		price := quote.K.Close.Float64()
		preClose := quote.K.Last.Float64()

		// 价格合理性校验：换算失败时宁可丢弃该代码（由上层用兜底源补齐），
		// 也绝不把错值返回出去。
		if !isPlausibleQuotePrice(price) || !isPlausibleQuotePrice(preClose) {
			log.Warn("TDX 报价价格异常，已丢弃 %s: price=%v preClose=%v（疑似 Decimal 换算错误）", symbol, price, preClose)
			continue
		}

		change := price - preClose
		changePercent := 0.0
		if preClose > 0 {
			changePercent = change / preClose * 100
		}

		stock := models.Stock{
			Symbol:        symbol,
			Name:          p.lookupName(symbol),
			Price:         price,
			Change:        change,
			ChangePercent: changePercent,
			Volume:        int64(quote.TotalHand),
			Amount:        quote.Amount,
			Open:          quote.K.Open.Float64(),
			High:          quote.K.High.Float64(),
			Low:           quote.K.Low.Float64(),
			PreClose:      preClose,
		}

		bids := make([]models.OrderBookItem, 0, 5)
		asks := make([]models.OrderBookItem, 0, 5)
		for _, level := range quote.BuyLevel {
			if level.Price <= 0 {
				continue
			}
			price := level.Price.Float64()
			if !isPlausibleQuotePrice(price) {
				price = 0
			}
			if price <= 0 {
				continue
			}
			bids = append(bids, models.OrderBookItem{
				Price: price,
				Size:  int64(level.Number),
			})
		}
		for _, level := range quote.SellLevel {
			if level.Price <= 0 {
				continue
			}
			price := level.Price.Float64()
			if !isPlausibleQuotePrice(price) {
				price = 0
			}
			if price <= 0 {
				continue
			}
			asks = append(asks, models.OrderBookItem{
				Price: price,
				Size:  int64(level.Number),
			})
		}

		calculateOrderBookTotals(bids)
		calculateOrderBookTotals(asks)

		stocks = append(stocks, StockWithOrderBook{
			Stock:     stock,
			OrderBook: models.OrderBook{Bids: bids, Asks: asks},
		})
	}

	return stocks, nil
}

// fetchQuotesResilient 批量取报价，失败时降级为逐个请求。
//
// tdx 库的 GetQuote 是全有全无的：一个坏代码会让整批返回错误。
// 这里保证单个坏代码最多影响它自己，同批次的其他代码仍能拿到数据。
func (p *tdxMarketProvider) fetchQuotesResilient(cli tdxClient, codes []string) protocol.QuotesResp {
	if len(codes) == 0 {
		return nil
	}

	quotes, err := cli.GetQuote(codes...)
	if err == nil && len(quotes) == len(codes) {
		return quotes
	}

	if len(codes) == 1 {
		log.Warn("TDX 获取报价失败 %v: %v", codes[0], err)
		return nil
	}

	log.Warn("TDX 批量获取报价失败（%d 个代码），降级为逐个请求: %v", len(codes), err)
	var result protocol.QuotesResp
	for _, code := range codes {
		single, singleErr := cli.GetQuote(code)
		if singleErr != nil {
			log.Warn("TDX 获取报价失败 %v: %v", code, singleErr)
			continue
		}
		result = append(result, single...)
	}
	return result
}

func (p *tdxMarketProvider) FetchKLineData(code string, period string, days int) ([]models.KLineData, error) {
	cli, err := p.getClient()
	if err != nil {
		return nil, err
	}

	var resp *protocol.KlineResp
	switch period {
	case "1m":
		resp, err = cli.GetKlineMinuteAll(code)
	case "1d":
		resp, err = cli.GetKlineDayAll(code)
	case "1w":
		resp, err = cli.GetKlineWeekAll(code)
	case "1mo":
		resp, err = cli.GetKlineMonthAll(code)
	default:
		resp, err = cli.GetKlineDayAll(code)
	}
	if err != nil {
		return nil, err
	}
	if resp == nil || len(resp.List) == 0 {
		return nil, errors.New("tdx returned empty kline data")
	}

	sort.Slice(resp.List, func(i, j int) bool {
		return resp.List[i].Time.Before(resp.List[j].Time)
	})

	klines := make([]models.KLineData, 0, len(resp.List))
	for _, item := range resp.List {
		timeValue := item.Time.Format("2006-01-02")
		if period == "1m" {
			timeValue = item.Time.Format("2006-01-02 15:04:05")
		}
		klines = append(klines, models.KLineData{
			Time:   timeValue,
			Open:   item.Open.Float64(),
			High:   item.High.Float64(),
			Low:    item.Low.Float64(),
			Close:  item.Close.Float64(),
			Volume: item.Volume,
			Amount: item.Amount.Float64(),
		})
	}

	if period == "1m" {
		klines = filterTodayKLines(klines)
		klines = trimKLines(klines, days)
		klines = calculateAvgLineByLotVolume(klines)
		return klines, nil
	}

	klines = trimKLines(klines, days)
	applyMovingAverages(klines)
	return klines, nil
}

func (p *tdxMarketProvider) FetchMarketIndices() ([]models.MarketIndex, error) {
	cli, err := p.getClient()
	if err != nil {
		return nil, err
	}

	indices := make([]models.MarketIndex, 0, len(tdxIndexNames))
	for code, name := range tdxIndexNames {
		resp, err := cli.GetIndexMinute(code, 0, 2)
		if err != nil {
			return nil, err
		}
		if resp == nil || len(resp.List) == 0 {
			return nil, errors.New("tdx returned empty index data")
		}

		dayResp, err := cli.GetIndexDay(code, 0, 2)
		if err != nil {
			return nil, err
		}
		if dayResp == nil || len(dayResp.List) == 0 {
			return nil, errors.New("tdx returned empty index day data")
		}

		sort.Slice(resp.List, func(i, j int) bool {
			return resp.List[i].Time.Before(resp.List[j].Time)
		})
		sort.Slice(dayResp.List, func(i, j int) bool {
			return dayResp.List[i].Time.Before(dayResp.List[j].Time)
		})

		latest := resp.List[len(resp.List)-1]
		daily := dayResp.List[len(dayResp.List)-1]
		preClose := daily.Last.Float64()
		if preClose == 0 && len(dayResp.List) > 1 {
			preClose = dayResp.List[len(dayResp.List)-2].Close.Float64()
		}
		if preClose == 0 {
			preClose = latest.Last.Float64()
		}
		if preClose == 0 && len(resp.List) > 1 {
			preClose = resp.List[0].Last.Float64()
		}
		if preClose == 0 && len(resp.List) > 1 {
			preClose = resp.List[len(resp.List)-2].Close.Float64()
		}
		price := latest.Close.Float64()
		change := price - preClose
		changePercent := 0.0
		if preClose > 0 {
			changePercent = change / preClose * 100
		}
		indices = append(indices, models.MarketIndex{
			Code:          code,
			Name:          name,
			Price:         price,
			Change:        change,
			ChangePercent: changePercent,
			Volume:        latest.Volume,
			Amount:        latest.Amount.Float64(),
		})
	}

	sort.Slice(indices, func(i, j int) bool {
		return indices[i].Code < indices[j].Code
	})
	return indices, nil
}

func (p *tdxMarketProvider) SearchStocks(keyword string, limit int) ([]StockSearchResult, error) {
	catalog, err := p.loadCatalog()
	if err != nil {
		return nil, err
	}
	return filterStockCatalog(catalog, keyword, limit), nil
}

func (p *tdxMarketProvider) getClient() (tdxClient, error) {
	p.mu.RLock()
	if p.client != nil {
		defer p.mu.RUnlock()
		return p.client, nil
	}
	p.mu.RUnlock()

	p.clientMu.Lock()
	defer p.clientMu.Unlock()

	p.mu.RLock()
	if p.client != nil {
		defer p.mu.RUnlock()
		return p.client, nil
	}
	p.mu.RUnlock()

	cli, err := p.dialClient()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.client = cli
	p.mu.Unlock()

	return cli, nil
}

type tdxClient interface {
	GetQuote(codes ...string) (protocol.QuotesResp, error)
	GetKlineMinuteAll(code string) (*protocol.KlineResp, error)
	GetKlineDayAll(code string) (*protocol.KlineResp, error)
	GetIndexDay(code string, start, count uint16) (*protocol.KlineResp, error)
	GetKlineWeekAll(code string) (*protocol.KlineResp, error)
	GetKlineMonthAll(code string) (*protocol.KlineResp, error)
	GetCodeAll(exchange protocol.Exchange) (*protocol.CodeResp, error)
	GetIndexMinute(code string, start, count uint16) (*protocol.KlineResp, error)
}

func (p *tdxMarketProvider) loadCatalog() ([]StockSearchResult, error) {
	p.mu.RLock()
	if len(p.catalog) > 0 && time.Since(p.updatedAt) < p.cacheTTL {
		items := append([]StockSearchResult(nil), p.catalog...)
		p.mu.RUnlock()
		return items, nil
	}
	p.mu.RUnlock()

	if cache, err := p.loadCatalogCacheFromFile(); err == nil && len(cache.Items) > 0 && time.Since(cache.UpdatedAt) < p.cacheTTL {
		p.setCatalog(cache.Items, cache.UpdatedAt)
		return append([]StockSearchResult(nil), cache.Items...), nil
	}

	items, err := p.fetchCatalogFromTDX()
	if err == nil && len(items) > 0 {
		p.setCatalog(items, time.Now())
		p.saveCatalogCacheToFile(items)
		return append([]StockSearchResult(nil), items...), nil
	}

	p.mu.RLock()
	if len(p.catalog) > 0 {
		items = append([]StockSearchResult(nil), p.catalog...)
		p.mu.RUnlock()
		return items, nil
	}
	p.mu.RUnlock()

	if cache, cacheErr := p.loadCatalogCacheFromFile(); cacheErr == nil && len(cache.Items) > 0 {
		p.setCatalog(cache.Items, cache.UpdatedAt)
		return append([]StockSearchResult(nil), cache.Items...), nil
	}

	return nil, err
}

func (p *tdxMarketProvider) fetchCatalogFromTDX() ([]StockSearchResult, error) {
	cli, err := p.getClient()
	if err != nil {
		return nil, err
	}

	results := make([]StockSearchResult, 0, 8000)
	for _, exchange := range []protocol.Exchange{protocol.ExchangeSH, protocol.ExchangeSZ, protocol.ExchangeBJ} {
		resp, err := cli.GetCodeAll(exchange)
		if err != nil {
			return nil, err
		}
		for _, item := range resp.List {
			symbol := exchange.String() + item.Code
			// 场内基金（ETF/LOF）一并纳入搜索目录。
			// 注意 TDX 目录里 ETF 的名称是简称（如 sh510300 叫 "300ETF"），
			// 展示用的全名由 M3 用新浪/腾讯的全名覆盖，这里先保证能被搜到。
			if !protocol.IsStock(symbol) && !protocol.IsETF(symbol) {
				continue
			}
			results = append(results, StockSearchResult{
				Symbol: symbol,
				Name:   strings.TrimSpace(item.Name),
				Market: exchange.Name(),
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Symbol < results[j].Symbol
	})
	return results, nil
}

func (p *tdxMarketProvider) loadCatalogCacheFromFile() (*stockCatalogCache, error) {
	data, err := os.ReadFile(p.cachePath)
	if err != nil {
		return nil, err
	}

	var cache stockCatalogCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func (p *tdxMarketProvider) saveCatalogCacheToFile(items []StockSearchResult) {
	cache := stockCatalogCache{
		UpdatedAt: time.Now(),
		Items:     items,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		log.Warn("保存 TDX 股票目录缓存失败: %v", err)
		return
	}
	if err := os.WriteFile(p.cachePath, data, 0644); err != nil {
		log.Warn("写入 TDX 股票目录缓存失败: %v", err)
	}
}

func (p *tdxMarketProvider) setCatalog(items []StockSearchResult, updatedAt time.Time) {
	nameBySymbol := make(map[string]string, len(items))
	for _, item := range items {
		nameBySymbol[item.Symbol] = item.Name
	}

	p.mu.Lock()
	p.catalog = append([]StockSearchResult(nil), items...)
	p.nameBySymbol = nameBySymbol
	p.updatedAt = updatedAt
	p.mu.Unlock()
}

func (p *tdxMarketProvider) lookupName(symbol string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if name := p.nameBySymbol[symbol]; name != "" {
		return name
	}
	return symbol
}

func trimKLines(klines []models.KLineData, limit int) []models.KLineData {
	if limit <= 0 || len(klines) <= limit {
		return klines
	}
	return klines[len(klines)-limit:]
}

func applyMovingAverages(klines []models.KLineData) {
	for i := range klines {
		klines[i].MA5 = calculateMovingAverage(klines, i, 5)
		klines[i].MA10 = calculateMovingAverage(klines, i, 10)
		klines[i].MA20 = calculateMovingAverage(klines, i, 20)
	}
}

func calculateMovingAverage(klines []models.KLineData, end int, period int) float64 {
	start := end - period + 1
	if start < 0 {
		return 0
	}

	sum := 0.0
	for i := start; i <= end; i++ {
		sum += klines[i].Close
	}
	return sum / float64(period)
}

func calculateAvgLineByLotVolume(klines []models.KLineData) []models.KLineData {
	if len(klines) == 0 {
		return klines
	}

	var totalAmount float64
	var totalVolume int64

	for i := range klines {
		totalAmount += klines[i].Amount
		totalVolume += klines[i].Volume
		if totalVolume > 0 {
			// TDX 分时成交量单位是“手”，换算成股后再计算均价。
			klines[i].Avg = totalAmount / float64(totalVolume*100)
		}
	}

	return klines
}
