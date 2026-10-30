package services

import (
	"strings"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/proxy"
)

type marketProvider interface {
	FetchStockDataWithOrderBook(codes ...string) ([]StockWithOrderBook, error)
	FetchKLineData(code string, period string, days int) ([]models.KLineData, error)
	FetchMarketIndices() ([]models.MarketIndex, error)
	SearchStocks(keyword string, limit int) ([]StockSearchResult, error)
}

func NewMarketServiceWithProviders(primary marketProvider, fallback marketProvider) *MarketService {
	ms := newMarketService()
	ms.primaryProvider = primary
	ms.fallbackProvider = fallback
	return ms
}

func newMarketService() *MarketService {
	ms := &MarketService{
		client:        proxy.GetManager().GetClientWithTimeout(5 * time.Second),
		cache:         make(map[string]*stockCache),
		cacheTTL:      2 * time.Second,
		klineCache:    make(map[string]*klineCache),
		klineCacheTTL: klineCacheTTLDefault,
	}
	go ms.cleanCacheLoop()
	return ms
}

func (ms *MarketService) fetchStockDataWithFallback(codes ...string) ([]StockWithOrderBook, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 按 providers 优先级依次尝试，第一个返回非空数据的源胜出。
	for _, provider := range ms.providers {
		if provider == nil {
			continue
		}
		data, err := provider.FetchStockDataWithOrderBook(codes...)
		if err == nil && len(data) > 0 {
			return ms.fillMissingFromFallback(codes, data), nil
		}
		log.Warn("行情源获取实时数据失败，切换下一个源: %v", err)
	}

	// providers 未配置时（旧调用方）退回 primary/fallback 二元逻辑
	if len(ms.providers) == 0 {
		if ms.primaryProvider != nil {
			data, err := ms.primaryProvider.FetchStockDataWithOrderBook(codes...)
			if err == nil && len(data) > 0 {
				return ms.fillMissingFromFallback(codes, data), nil
			}
			log.Warn("主行情源获取实时数据失败，切换兜底源: %v", err)
		}
		if ms.fallbackProvider != nil {
			return ms.fallbackProvider.FetchStockDataWithOrderBook(codes...)
		}
	}

	return nil, nil
}

// fillMissingFromFallback 用兜底源补齐主源缺失的代码。
//
// 主源可能因为个别代码不可用而少返回部分数据（例如 TDX 目录里没有该代码、
// 或价格合理性校验未通过）。此时不能直接当成成功返回，否则这些代码会静默消失，
// 前端表现为"某些自选股没有行情"且没有任何错误提示。
func (ms *MarketService) fillMissingFromFallback(codes []string, data []StockWithOrderBook) []StockWithOrderBook {
	if ms.fallbackProvider == nil || len(data) >= len(codes) {
		return data
	}

	present := make(map[string]struct{}, len(data))
	for _, item := range data {
		present[strings.ToLower(item.Symbol)] = struct{}{}
	}

	missing := make([]string, 0, len(codes)-len(data))
	for _, code := range codes {
		if _, ok := present[strings.ToLower(code)]; !ok {
			missing = append(missing, code)
		}
	}
	if len(missing) == 0 {
		return data
	}

	extra, err := ms.fallbackProvider.FetchStockDataWithOrderBook(missing...)
	if err != nil {
		log.Warn("兜底源补齐缺失行情失败（%d 个代码）: %v", len(missing), err)
		return data
	}
	if len(extra) == 0 {
		return data
	}
	return append(data, extra...)
}

func (ms *MarketService) fetchKLineDataWithFallback(code string, period string, days int) ([]models.KLineData, error) {
	for _, provider := range ms.providers {
		if provider == nil {
			continue
		}
		data, err := provider.FetchKLineData(code, period, days)
		if err == nil && len(data) > 0 {
			return data, nil
		}
		log.Warn("行情源获取K线失败，切换下一个源: %v", err)
	}

	if len(ms.providers) == 0 {
		if ms.primaryProvider != nil {
			data, err := ms.primaryProvider.FetchKLineData(code, period, days)
			if err == nil && len(data) > 0 {
				return data, nil
			}
			log.Warn("主行情源获取K线失败，切换兜底源: %v", err)
		}
		if ms.fallbackProvider != nil {
			return ms.fallbackProvider.FetchKLineData(code, period, days)
		}
	}

	return nil, nil
}

func (ms *MarketService) fetchMarketIndicesWithFallback() ([]models.MarketIndex, error) {
	for _, provider := range ms.providers {
		if provider == nil {
			continue
		}
		data, err := provider.FetchMarketIndices()
		if err == nil && len(data) > 0 {
			return data, nil
		}
		log.Warn("行情源获取指数失败，切换下一个源: %v", err)
	}

	if len(ms.providers) == 0 {
		if ms.primaryProvider != nil {
			data, err := ms.primaryProvider.FetchMarketIndices()
			if err == nil && len(data) > 0 {
				return data, nil
			}
			log.Warn("主行情源获取指数失败，切换兜底源: %v", err)
		}
		if ms.fallbackProvider != nil {
			return ms.fallbackProvider.FetchMarketIndices()
		}
	}

	return nil, nil
}

func (ms *MarketService) searchStocksWithFallback(keyword string, limit int) []StockSearchResult {
	for _, provider := range ms.providers {
		if provider == nil {
			continue
		}
		results, err := provider.SearchStocks(keyword, limit)
		if err == nil && len(results) > 0 {
			return results
		}
		if err != nil {
			log.Warn("行情源搜索股票失败，切换下一个源: %v", err)
		}
	}

	if len(ms.providers) == 0 {
		if ms.primaryProvider != nil {
			results, err := ms.primaryProvider.SearchStocks(keyword, limit)
			if err == nil && len(results) > 0 {
				return results
			}
			log.Warn("主行情源搜索股票失败，切换兜底源: %v", err)
		}
		if ms.fallbackProvider != nil {
			results, err := ms.fallbackProvider.SearchStocks(keyword, limit)
			if err == nil {
				return results
			}
		}
	}

	return nil
}

type sinaMarketProvider struct {
	service *MarketService
}

func newSinaMarketProvider(service *MarketService) *sinaMarketProvider {
	return &sinaMarketProvider{service: service}
}

func (p *sinaMarketProvider) FetchStockDataWithOrderBook(codes ...string) ([]StockWithOrderBook, error) {
	return p.service.fetchStockDataWithOrderBookFromSina(codes...)
}

func (p *sinaMarketProvider) FetchKLineData(code string, period string, days int) ([]models.KLineData, error) {
	return p.service.fetchKLineDataFromSina(code, period, days)
}

func (p *sinaMarketProvider) FetchMarketIndices() ([]models.MarketIndex, error) {
	return p.service.fetchMarketIndicesFromSina()
}

func (p *sinaMarketProvider) SearchStocks(keyword string, limit int) ([]StockSearchResult, error) {
	return searchEmbeddedStocks(keyword, limit), nil
}
