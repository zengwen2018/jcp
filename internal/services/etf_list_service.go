package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
)

// 本文件实现 ETF 清单三源合并 + T+0 判定（A股 ETF 里程碑 M3.3）。
//
// 数据源（均在 M2 落地）：
//   - 上交所 ETF 清单（yunhq:32041）：SSEExchangeClient.FetchETFList() → 仅 code+name（947 条）。
//   - 深交所基金清单（ShowReport 1105）：SZSEExchangeClient.FetchFundList() → code+name+jjlb+tzlb+规模+管理人（1083 条，分 55 页）。
//
// T+0 判定：
//   - 深市（官方可靠）：tzlb ∈ {其它基金(跨境QDII), 债券基金, 货币市场基金, ABS} → T+0。
//   - 沪市（代码规则，best-effort，因 smartbox 在本环境不可用）：前缀 511/513/518/519 视为 T+0
//     （货币/债券/跨境QDII/黄金/商品类）；其余沪市 ETF 按 T+1。
//   说明：沪市 T+0 无官方公开字段，代码规则为启发式，已在 doc §7 标注为限制。

// szseT0InvestTypes 深市 T+0 投资类别集合（来自 doc §1.2/§7 实测）。
var szseT0InvestTypes = map[string]bool{
	"其它基金(跨境QDII)": true,
	"债券基金":        true,
	"货币市场基金":      true,
	"ABS":          true,
}

// shT0Prefixes 沪市 T+0 代码前缀（启发式）。
var shT0Prefixes = []string{"511", "513", "518", "519"}

// ETFListService ETF 清单合并服务（带内存 + 落盘缓存）。
type ETFListService struct {
	sse       *SSEExchangeClient
	szse      *SZSEExchangeClient
	cacheFile string
	ttl       time.Duration
	mu        sync.Mutex
	catalog   []models.ETFListEntry
	builtAt   time.Time
}

// NewETFListService 创建清单服务。
func NewETFListService() *ETFListService {
	return &ETFListService{
		sse:       NewSSEExchangeClient(),
		szse:      NewSZSEExchangeClient(),
		cacheFile: filepath.Join(paths.EnsureCacheDir("etf"), "catalog.json"),
		ttl:       12 * time.Hour,
	}
}

// GetCatalog 返回合并后的 ETF 目录（内存 → 落盘 → 重建）。
func (s *ETFListService) GetCatalog() ([]models.ETFListEntry, error) {
	s.mu.Lock()
	if time.Since(s.builtAt) < s.ttl && len(s.catalog) > 0 {
		c := append([]models.ETFListEntry(nil), s.catalog...)
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock()

	if items := s.loadDisk(); items != nil {
		s.mu.Lock()
		s.catalog = items
		s.builtAt = time.Now()
		s.mu.Unlock()
		return items, nil
	}

	items, err := s.buildCatalog()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.catalog = items
	s.builtAt = time.Now()
	s.mu.Unlock()
	s.saveDisk(items)
	return items, nil
}

// buildCatalog 拉取两所清单并合并。
func (s *ETFListService) buildCatalog() ([]models.ETFListEntry, error) {
	sseItems, err := s.sse.FetchETFList()
	if err != nil {
		return nil, err
	}
	szseAll, err := s.fetchAllSZSE()
	if err != nil {
		return nil, err
	}
	return mergeLists(sseItems, szseAll), nil
}

// fetchAllSZSE 翻页拉取深交所全部基金列表（约 55 页 × 20 条）。
func (s *ETFListService) fetchAllSZSE() ([]SZSEFundListItem, error) {
	var all []SZSEFundListItem
	page := 1
	for page <= 60 { // 安全上限，避免接口异常时死循环
		items, total, err := s.szse.FetchFundList(page)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(all) >= total || len(items) == 0 {
			break
		}
		page++
	}
	return all, nil
}

// mergeLists 合并两所清单为统一目录项（纯函数，便于单测）。
func mergeLists(sse []SSEETFListItem, szse []SZSEFundListItem) []models.ETFListEntry {
	out := make([]models.ETFListEntry, 0, len(sse)+len(szse))
	for _, it := range sse {
		out = append(out, models.ETFListEntry{
			Code:     it.Code,
			Name:     it.Name,
			Exchange: "sh",
			FundType: "ETF",
			T0Flag:   deriveSHT0(it.Code),
		})
	}
	for _, it := range szse {
		out = append(out, models.ETFListEntry{
			Code:       it.Code,
			Name:       it.Name,
			Exchange:   "sz",
			FundType:   it.FundType,
			InvestType: it.InvestType,
			ScaleWan:   it.ScaleWan,
			Manager:    it.Manager,
			ListDate:   it.ListDate,
			T0Flag:     deriveSZSET0(it.InvestType),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Exchange != out[j].Exchange {
			return out[i].Exchange < out[j].Exchange
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// deriveSZSET0 深市 T+0 判定（官方 tzlb）。
func deriveSZSET0(investType string) bool {
	return szseT0InvestTypes[investType]
}

// deriveSHT0 沪市 T+0 判定（代码规则启发式）。
func deriveSHT0(code string) bool {
	c := bare6Code(code)
	for _, p := range shT0Prefixes {
		if strings.HasPrefix(c, p) {
			return true
		}
	}
	return false
}

// GetEntry 按 6 位代码（可带 sh/sz 前缀）从目录取单条。
func (s *ETFListService) GetEntry(code string) (*models.ETFListEntry, error) {
	catalog, err := s.GetCatalog()
	if err != nil {
		return nil, err
	}
	bare := bare6Code(code)
	for i := range catalog {
		if catalog[i].Code == bare {
			e := catalog[i]
			return &e, nil
		}
	}
	return nil, nil
}

// Search 按关键词（代码或名称子串）过滤目录。
func (s *ETFListService) Search(keyword string, limit int) []models.ETFListEntry {
	catalog, err := s.GetCatalog()
	if err != nil {
		return nil
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return nil
	}
	results := make([]models.ETFListEntry, 0)
	for _, it := range catalog {
		if strings.Contains(strings.ToLower(it.Code), kw) || strings.Contains(strings.ToLower(it.Name), kw) {
			results = append(results, it)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results
}

// loadDisk 读取落盘目录（未过期）。
func (s *ETFListService) loadDisk() []models.ETFListEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.cacheFile)
	if err != nil {
		return nil
	}
	var c struct {
		UpdatedAt time.Time             `json:"updatedAt"`
		Catalog   []models.ETFListEntry `json:"catalog"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	if time.Since(c.UpdatedAt) >= s.ttl || len(c.Catalog) == 0 {
		return nil
	}
	return c.Catalog
}

// saveDisk 写入落盘目录。
func (s *ETFListService) saveDisk(catalog []models.ETFListEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.cacheFile), 0755); err != nil {
		log.Warn("创建 ETF 目录缓存目录失败: %v", err)
		return
	}
	payload := struct {
		UpdatedAt time.Time             `json:"updatedAt"`
		Catalog   []models.ETFListEntry `json:"catalog"`
	}{UpdatedAt: time.Now(), Catalog: catalog}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		log.Warn("序列化 ETF 目录缓存失败: %v", err)
		return
	}
	if err := os.WriteFile(s.cacheFile, data, 0644); err != nil {
		log.Warn("写入 ETF 目录缓存失败: %v", err)
	}
}
