package services

import (
	"iter"
	"sort"
	"strings"
	"sync"

	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
)

// tdxCodeEntry TDX 目录条目的精简信息。
//
// 只保留价格换算与名称展示需要的字段，避免把整份目录（4 万多条，含大量
// sh88xxxx 市场统计指标）原样写进缓存。
type tdxCodeEntry struct {
	Name     string `json:"name"`
	Code     string `json:"code"`
	Exchange string `json:"exchange"`
	Multiple uint16 `json:"multiple"`
	Decimal  int8   `json:"decimal"`
}

// tdxSelfCodes 是 tdx.ICodes 的自维护实现，为 tdx 库提供价格换算所需的 Decimal。
//
// 【为什么必须要有它】
// tdx 库的 GetQuote 对"非个股"的报价会做价格换算：
//
//	CodeModel.Price(p) = p * 10^(2 - Decimal)
//
// 而 protocol.Price.Float64() 固定除以 1000（单位厘）。
// TDX 协议中个股价格字段存的是 真实价×1000，ETF 存的是 真实价×10000，
// 因此 ETF（Decimal=3）需要再乘 10^(2-3)=0.1 才能还原真实价。
//
// 若 tdx.DefaultCodes 为 nil，tdx 库会直接返回 "DefaultCodes未初始化"，
// 且该批次的所有代码（哪怕是正常个股）都会失败 —— 这就是 ETF 一只就拖垮整批的原因。
//
// 【为什么不用 tdx.NewCodes()】
// 它会创建 sqlite 数据库并启动每日 9:01 的定时更新任务，是 jcp 原本没有的副作用，
// 且容器重启后目录可能丢失。这里沿用 jcp 既有的 JSON 缓存范式自行维护映射。
//
// 【Decimal 的两级保障】
//  1. 权威值：GetCodeAll 返回的 protocol.Code.Decimal，异步加载并落盘缓存；
//  2. 规则兜底：ETF 恒为 3、指数恒为 2。
//     该规则已对服务器上 TDX 全量目录（52670 条）实测验证：
//     ETF 2547/2547 命中，指数 555/555 命中。
//
// 规则覆盖不到的品种（如 sh88xxxx 市场统计指标）返回 nil，让 tdx 库以
// "未查询到代码"明确失败 —— 这比返回一个错误的 Decimal 导致价格静默错位安全得多。
type tdxSelfCodes struct {
	mu      sync.RWMutex
	entries map[string]tdxCodeEntry
}

func newTDXSelfCodes() *tdxSelfCodes {
	return &tdxSelfCodes{entries: make(map[string]tdxCodeEntry)}
}

// Get 返回指定代码的目录信息。
func (s *tdxSelfCodes) Get(code string) *tdx.CodeModel {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) != 8 {
		return nil
	}

	// 个股不参与 tdx 库的价格换算（库内部走个股分支），但若仍被调用到，
	// 目录里的 Decimal 未必可靠 —— 实测北交所个股目录 Decimal=0，
	// 而其价格字段精度与沪深个股一致，因此统一按 2 处理更安全。
	if protocol.IsStock(code) {
		return &tdx.CodeModel{
			Code:     code[2:],
			Exchange: code[:2],
			Decimal:  2,
		}
	}

	s.mu.RLock()
	entry, ok := s.entries[code]
	s.mu.RUnlock()
	if ok {
		return &tdx.CodeModel{
			Name:     entry.Name,
			Code:     entry.Code,
			Exchange: entry.Exchange,
			Multiple: entry.Multiple,
			Decimal:  entry.Decimal,
		}
	}

	decimal, ok := inferTDXDecimal(code)
	if !ok {
		return nil
	}
	return &tdx.CodeModel{
		Code:     code[2:],
		Exchange: code[:2],
		Decimal:  decimal,
	}
}

// inferTDXDecimal 按代码特征推断 Decimal。
//
// 依据是对 TDX 全量目录（52670 条）的实测结果：
//   - ETF（sh51/52/53/56/58 开头、sz15/16 开头）全部为 3，共 2547 只；
//   - 指数（sh000/999999、sz399）全部为 2，共 555 只；
//   - 其余品种（sh88xxxx 市场统计指标等）无法用规则判定，返回 false。
func inferTDXDecimal(code string) (int8, bool) {
	if len(code) != 8 {
		return 0, false
	}
	switch {
	case protocol.IsETF(code):
		return 3, true
	case protocol.IsIndex(code):
		return 2, true
	}
	return 0, false
}

// GetName 返回代码对应的名称。
func (s *tdxSelfCodes) GetName(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	s.mu.RLock()
	entry, ok := s.entries[code]
	s.mu.RUnlock()
	if ok {
		return entry.Name
	}
	return ""
}

// Iter 遍历全部目录条目。
func (s *tdxSelfCodes) Iter() iter.Seq2[string, *tdx.CodeModel] {
	return func(yield func(string, *tdx.CodeModel) bool) {
		s.mu.RLock()
		snapshot := make(map[string]tdxCodeEntry, len(s.entries))
		for k, v := range s.entries {
			snapshot[k] = v
		}
		s.mu.RUnlock()

		symbols := make([]string, 0, len(snapshot))
		for symbol := range snapshot {
			symbols = append(symbols, symbol)
		}
		sort.Strings(symbols)

		for _, symbol := range symbols {
			entry := snapshot[symbol]
			model := &tdx.CodeModel{
				Name:     entry.Name,
				Code:     entry.Code,
				Exchange: entry.Exchange,
				Multiple: entry.Multiple,
				Decimal:  entry.Decimal,
			}
			if !yield(symbol, model) {
				return
			}
		}
	}
}

// filterBy 按条件筛选目录条目。
func (s *tdxSelfCodes) filterBy(match func(string) bool, limit int) tdx.CodeModels {
	var result tdx.CodeModels
	for symbol, model := range s.Iter() {
		if !match(symbol) {
			continue
		}
		result = append(result, model)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

// GetStocks 返回个股目录条目。
func (s *tdxSelfCodes) GetStocks(limit ...int) tdx.CodeModels {
	return s.filterBy(protocol.IsStock, firstLimit(limit))
}

// GetStockCodes 返回个股代码列表。
func (s *tdxSelfCodes) GetStockCodes(limit ...int) []string {
	return s.GetStocks(limit...).Codes()
}

// GetETFs 返回场内基金目录条目。
func (s *tdxSelfCodes) GetETFs(limit ...int) tdx.CodeModels {
	return s.filterBy(protocol.IsETF, firstLimit(limit))
}

// GetETFCodes 返回场内基金代码列表。
func (s *tdxSelfCodes) GetETFCodes(limit ...int) []string {
	return s.GetETFs(limit...).Codes()
}

// GetIndexes 返回指数目录条目。
func (s *tdxSelfCodes) GetIndexes(limit ...int) tdx.CodeModels {
	return s.filterBy(protocol.IsIndex, firstLimit(limit))
}

// GetIndexCodes 返回指数代码列表。
func (s *tdxSelfCodes) GetIndexCodes(limit ...int) []string {
	return s.GetIndexes(limit...).Codes()
}

// setEntries 整体替换目录内容。
func (s *tdxSelfCodes) setEntries(entries map[string]tdxCodeEntry) {
	s.mu.Lock()
	s.entries = entries
	s.mu.Unlock()
}

// firstLimit 取第一个 limit 参数，缺省为 -1（不限制）。
func firstLimit(limits []int) int {
	if len(limits) == 0 {
		return -1
	}
	return limits[0]
}
