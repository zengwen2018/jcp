package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/paths"
	"github.com/run-bigpig/jcp/internal/pkg/proxy"
)

// 本文件实现东财基金净值历史客户端（A股 ETF 里程碑 M3.2）。
//
// 数据源：api.fund.eastmoney.com/f10/lsjz（东财基金域）。
// 实测（2026-10-30）：fundCode=510300 可返回 LSJZList（FSRQ/DWJZ/LJJZ/JZZZL/SGZT/SHZT），
// 即单位净值、累计净值、日涨跌、申赎状态。ETF 交易代码（如 510300）在东财 lsjz 直认，无需映射母基金代码。
//
// 风险与对策：东财有风控（push2 已被封），故净值数据做**内存缓存 + 落盘**双重缓存，
// 落盘文件在缓存目录 <cache>/etf/<code>_nav.json，TTL 默认 6h（净值按日更新，6h 足够）。

const (
	emLSJZURLFmt   = "https://api.fund.eastmoney.com/f10/lsjz?fundCode=%s&pageIndex=1&pageSize=%d"
	emLSJZReferer  = "https://fundf10.eastmoney.com/"
	emNavUA        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"
	emNavDefaultTTL = 6 * time.Hour
)

// emLSJZResp 对应 lsjz 接口的响应结构。
type emLSJZResp struct {
	Data struct {
		LSJZList []struct {
			FSRQ  string `json:"FSRQ"`  // 净值日期 yyyy-MM-dd
			DWJZ  string `json:"DWJZ"`  // 单位净值
			LJJZ  string `json:"LJJZ"`  // 累计净值
			JZZZL string `json:"JZZZL"` // 日涨跌（%）
			SGZT  string `json:"SGZT"`  // 申购状态
			SHZT  string `json:"SHZT"`  // 赎回状态
		} `json:"LSJZList"`
		TotalCount int `json:"TotalCount"`
	} `json:"Data"`
}

// cachedNav 内存缓存条目。
type cachedNav struct {
	UpdatedAt time.Time
	Series    *models.ETFNavSeries
}

// diskNavPayload 落盘缓存文件结构。
type diskNavPayload struct {
	UpdatedAt time.Time          `json:"updatedAt"`
	Series    models.ETFNavSeries `json:"series"`
}

// ETFNavService 东财净值历史服务（带内存 + 落盘缓存）。
type ETFNavService struct {
	httpClient *http.Client
	cacheDir   string
	ttl        time.Duration
	mu         sync.Mutex
	mem        map[string]cachedNav
}

// NewETFNavService 创建净值服务。
func NewETFNavService() *ETFNavService {
	return &ETFNavService{
		httpClient: proxy.GetManager().GetClientWithTimeout(10 * time.Second),
		cacheDir:   paths.EnsureCacheDir("etf"),
		ttl:        emNavDefaultTTL,
		mem:        make(map[string]cachedNav),
	}
}

// FetchNavSeries 获取基金（ETF）净值历史序列，返回按日期升序的最近 days 条。
// 缓存优先级：内存 → 落盘 → 网络。网络失败时回退到任一可用缓存；均无则报错。
func (s *ETFNavService) FetchNavSeries(code string, days int) (*models.ETFNavSeries, error) {
	if days <= 0 {
		days = 30
	}
	// 落盘/接口按 6 位纯数字基金代码；交易所代码前缀忽略。
	bare := bare6Code(code)

	// 1) 内存缓存
	if series := s.fromMem(bare); series != nil {
		return trimNavSeries(series, days), nil
	}
	// 2) 落盘缓存
	if series := s.fromDisk(bare); series != nil {
		s.toMem(bare, series)
		return trimNavSeries(series, days), nil
	}
	// 3) 网络
	body, err := s.fetchRaw(bare)
	if err != nil {
		return nil, err
	}
	series, err := parseEMNav(body, bare)
	if err != nil {
		return nil, err
	}
	s.toMem(bare, series)
	s.toDisk(bare, series)
	return series, nil
}

// fetchRaw 直连东财 lsjz 返回原始响应体。
func (s *ETFNavService) fetchRaw(code string) ([]byte, error) {
	url := fmt.Sprintf(emLSJZURLFmt, code, 90) // 单次取足 90 条，调用方再按 days 截断
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", emLSJZReferer)
	req.Header.Set("User-Agent", emNavUA)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("东财 lsjz 请求失败 %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("东财 lsjz 返回非 200: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// parseEMNav 解析 lsjz 响应体为净值序列（接口返回按日期降序 → 转为升序）。
func parseEMNav(body []byte, code string) (*models.ETFNavSeries, error) {
	var em emLSJZResp
	if err := json.Unmarshal(body, &em); err != nil {
		return nil, fmt.Errorf("东财 lsjz 解析失败: %w", err)
	}
	list := em.Data.LSJZList
	if len(list) == 0 {
		return nil, fmt.Errorf("东财 lsjz 无净值数据（代码 %s 可能不存在或非基金）", code)
	}
	points := make([]models.ETFNavPoint, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		row := list[i]
		points = append(points, models.ETFNavPoint{
			Date:            row.FSRQ,
			UnitNav:         exToFloat(row.DWJZ),
			AccumNav:        exToFloat(row.LJJZ),
			ChangePercent:   exToFloat(row.JZZZL),
			SubscribeStatus: row.SGZT,
			RedeemStatus:    row.SHZT,
		})
	}
	return &models.ETFNavSeries{Code: code, Points: points}, nil
}

// trimNavSeries 截取最近 days 条（points 已升序）。
func trimNavSeries(series *models.ETFNavSeries, days int) *models.ETFNavSeries {
	if days <= 0 || len(series.Points) <= days {
		return series
	}
	series.Points = series.Points[len(series.Points)-days:]
	return series
}

// fromMem 读取内存缓存（未过期）。
func (s *ETFNavService) fromMem(code string) *models.ETFNavSeries {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.mem[code]; ok && time.Since(c.UpdatedAt) < s.ttl {
		return c.Series
	}
	return nil
}

func (s *ETFNavService) toMem(code string, series *models.ETFNavSeries) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mem[code] = cachedNav{UpdatedAt: time.Now(), Series: series}
}

// fromDisk 读取落盘缓存（未过期）。
func (s *ETFNavService) fromDisk(code string) *models.ETFNavSeries {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadDiskLocked(code)
}

func (s *ETFNavService) loadDiskLocked(code string) *models.ETFNavSeries {
	data, err := os.ReadFile(s.diskPath(code))
	if err != nil {
		return nil
	}
	var c diskNavPayload
	if err := json.Unmarshal(data, &c); err != nil {
		return nil
	}
	if time.Since(c.UpdatedAt) >= s.ttl {
		return nil
	}
	return &c.Series
}

// toDisk 写入落盘缓存。
func (s *ETFNavService) toDisk(code string, series *models.ETFNavSeries) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.cacheDir, 0755); err != nil {
		log.Warn("创建 ETF 缓存目录失败: %v", err)
		return
	}
	payload := diskNavPayload{UpdatedAt: time.Now(), Series: *series}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		log.Warn("序列化 ETF 净值缓存失败: %v", err)
		return
	}
	if err := os.WriteFile(s.diskPath(code), data, 0644); err != nil {
		log.Warn("写入 ETF 净值缓存失败: %v", err)
	}
}

func (s *ETFNavService) diskPath(code string) string {
	return filepath.Join(s.cacheDir, fmt.Sprintf("%s_nav.json", code))
}
