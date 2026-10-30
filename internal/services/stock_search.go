package services

import (
	"encoding/json"
	"strings"

	"github.com/run-bigpig/jcp/internal/embed"
)

type stockBasicData struct {
	Data struct {
		Fields []string        `json:"fields"`
		Items  [][]interface{} `json:"items"`
	} `json:"data"`
}

type StockSearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Industry string `json:"industry"`
	Market   string `json:"market"`
	// SecurityType 证券细分类：stock / etf / lof / index；为空表示未知（如内嵌兜底源）
	SecurityType string `json:"securityType,omitempty"`
}

func searchEmbeddedStocks(keyword string, limit int) []StockSearchResult {
	if keyword == "" {
		return nil
	}

	var basicData stockBasicData
	if err := json.Unmarshal(embed.StockBasicJSON, &basicData); err != nil {
		return nil
	}

	var symbolIdx, nameIdx, industryIdx, tsCodeIdx int = -1, -1, -1, -1
	for i, field := range basicData.Data.Fields {
		switch field {
		case "symbol":
			symbolIdx = i
		case "name":
			nameIdx = i
		case "industry":
			industryIdx = i
		case "ts_code":
			tsCodeIdx = i
		}
	}
	if symbolIdx < 0 || nameIdx < 0 {
		return nil
	}

	results := make([]StockSearchResult, 0, limit)
	upperKeyword := strings.ToUpper(keyword)
	for _, item := range basicData.Data.Items {
		if limit > 0 && len(results) >= limit {
			break
		}

		symbol, _ := item[symbolIdx].(string)
		name, _ := item[nameIdx].(string)
		if !matchStockKeyword(upperKeyword, symbol, name) {
			continue
		}

		industry := ""
		if industryIdx >= 0 && industryIdx < len(item) {
			industry, _ = item[industryIdx].(string)
		}

		market := ""
		fullSymbol := symbol
		if tsCodeIdx >= 0 && tsCodeIdx < len(item) {
			tsCode, _ := item[tsCodeIdx].(string)
			switch {
			case strings.HasSuffix(tsCode, ".SH"):
				market = "上海"
				fullSymbol = "sh" + symbol
			case strings.HasSuffix(tsCode, ".SZ"):
				market = "深圳"
				fullSymbol = "sz" + symbol
			case strings.HasSuffix(tsCode, ".BJ"):
				market = "北京"
				fullSymbol = "bj" + symbol
			}
		}

		results = append(results, StockSearchResult{
			Symbol:   fullSymbol,
			Name:     name,
			Industry: industry,
			Market:   market,
		})
	}
	return results
}

func filterStockCatalog(catalog []StockSearchResult, keyword string, limit int) []StockSearchResult {
	if keyword == "" {
		return nil
	}

	upperKeyword := strings.ToUpper(keyword)
	results := make([]StockSearchResult, 0, limit)
	for _, item := range catalog {
		if limit > 0 && len(results) >= limit {
			break
		}
		if matchStockKeyword(upperKeyword, item.Symbol, item.Name) {
			results = append(results, item)
		}
	}
	return results
}

func matchStockKeyword(keyword string, symbol string, name string) bool {
	upperSymbol := strings.ToUpper(symbol)
	upperName := strings.ToUpper(name)
	return strings.Contains(upperSymbol, keyword) || strings.Contains(upperName, keyword)
}
