package services

import (
	"fmt"
	"strings"

	"github.com/run-bigpig/jcp/internal/models"
)

// 本文件实现 A股 ETF 行情聚合服务（里程碑 M3.4）。
//
// 设计（见 docs/A股ETF数据源可得性探查报告.md §4/§5.4）：
//   - 实时市价 + IOPV + 五档盘口：交易所直连（沪 yunhq:32041 / 深 www.szse.cn），
//     二者同源 → 折溢价率口径一致（避免混用 TDX 市价与交易所 IOPV 的刷新节奏差）。
//   - 单位净值 NAV（T-1）：东财 lsjz（ETFNavService，6h 内存 + 落盘双缓存）。
//   - ETF 清单 + T+0 判定：ETFListService（SSE+SZSE 合并，12h 缓存 + 落盘）。
//
// 关于"5 个端点"的落地方式：JCP 是 Wails v2 桌面应用，后端↔前端经 app.go 绑定的
// App 方法暴露（main.go 的 Bind: []interface{}{app}），并不走 /api HTTP 路由。
// 因此 doc §5.4 的 5 个端点在本仓库落地为 5 个 App 绑定方法（见 app.go），
// 本文件提供其背后的聚合逻辑。

// ETFService A股 ETF 行情聚合服务。
type ETFService struct {
	sse  *SSEExchangeClient
	szse *SZSEExchangeClient
	nav  *ETFNavService
	list *ETFListService
}

// NewETFService 创建 ETF 聚合服务（复用 M2 交易所客户端与 M3.2/M3.3 服务）。
func NewETFService() *ETFService {
	return &ETFService{
		sse:  NewSSEExchangeClient(),
		szse: NewSZSEExchangeClient(),
		nav:  NewETFNavService(),
		list: NewETFListService(),
	}
}

// exchangeOf 由 6 位代码推断交易所：5/6 开头→沪市，其余（0/1/3 等）→深市。
func exchangeOf(bare string) string {
	if len(bare) == 0 {
		return "sz"
	}
	switch bare[0] {
	case '5', '6':
		return "sh"
	default:
		return "sz"
	}
}

// GetQuote 批量获取 ETF 实时行情（市价 + IOPV + 折溢价 + 涨跌停 + T+0）。
// 入参 codes 可带 sh/sz 前缀或裸 6 位；逐只取数，单只失败不影响其它。
func (s *ETFService) GetQuote(codes []string) []models.ETFQuote {
	out := make([]models.ETFQuote, 0, len(codes))
	for _, c := range codes {
		out = append(out, s.quoteOne(c))
	}
	return out
}

// quoteOne 拉取单只 ETF 行情并聚合。交易所客户端同时给出市价与 IOPV，口径一致。
func (s *ETFService) quoteOne(code string) models.ETFQuote {
	bare := bare6Code(code)
	ex := exchangeOf(bare)
	q := models.ETFQuote{
		Symbol:   ex + bare,
		Exchange: ex,
	}

	var (
		name      string
		price, prevClose, open, high, low float64
		volume    int64
		amount    float64
		iopv      float64
		ob        models.OrderBook
		dt        string
		limit20   bool
	)

	if ex == "sh" {
		sq, err := s.sse.FetchETFQuote(bare)
		if err != nil {
			log.Warn("ETF 沪市行情获取失败 %s: %v", bare, err)
			return q
		}
		name, price, prevClose, open, high, low = sq.Name, sq.Last, sq.PrevClose, sq.Open, sq.High, sq.Low
		volume, amount, iopv = sq.Volume, sq.Amount, sq.IOPV
		dt = fmt.Sprintf("%08d %06d", sq.Date, sq.Time)
		// 沪市无官方涨跌停字段（腾讯 field47/48 未在本里程碑引入），按板块启发式：
		// 科创板 ETF（588 开头）为 20% 涨跌幅，其余 10%。
		limit20 = strings.HasPrefix(bare, "588")
	} else {
		fq, err := s.szse.FetchFundQuote(bare)
		if err != nil {
			log.Warn("ETF 深市行情获取失败 %s: %v", bare, err)
			return q
		}
		name, price, prevClose, open, high, low = fq.Name, fq.Now, fq.PrevClose, fq.Open, fq.High, fq.Low
		volume, amount, iopv = fq.Volume, fq.Amount, fq.NetValue
		ob, dt, limit20 = fq.OrderBook, fq.DateTime, fq.Change20PerLimit
	}

	t0 := s.deriveT0(ex, bare)
	return buildETFQuote(ex+bare, name, ex, price, prevClose, open, high, low, volume, amount, iopv, ob, dt, limit20, t0)
}

// deriveT0 取 T+0 标识：深市依赖清单 investType（权威），沪市用代码规则；清单不可用时回退规则。
func (s *ETFService) deriveT0(ex, bare string) bool {
	if ex == "sh" {
		return deriveSHT0(bare)
	}
	if entry, err := s.list.GetEntry(bare); err == nil && entry != nil {
		return entry.T0Flag
	}
	return false
}

// buildETFQuote 纯函数：由交易所原始字段计算 ETFQuote（涨跌 / 折溢价 / 涨跌停），
// 不触发任何网络，便于单元测试。
func buildETFQuote(symbol, name, ex string, price, prevClose, open, high, low float64, volume int64, amount, iopv float64, ob models.OrderBook, dt string, limit20, t0 bool) models.ETFQuote {
	change := price - prevClose
	changePercent := 0.0
	if prevClose > 0 {
		changePercent = change / prevClose * 100
	}
	premiumRate := 0.0
	if iopv > 0 {
		premiumRate = (price - iopv) / iopv * 100
	}
	pct := 0.10
	if limit20 {
		pct = 0.20
	}
	var limitUp, limitDown float64
	if prevClose > 0 {
		limitUp = round3(prevClose * (1 + pct))
		limitDown = round3(prevClose * (1 - pct))
	}
	return models.ETFQuote{
		Symbol:         symbol,
		Name:           name,
		Exchange:       ex,
		MarketPrice:    price,
		PreClose:       prevClose,
		Open:           open,
		High:           high,
		Low:            low,
		Volume:         volume,
		Amount:         amount,
		Change:         change,
		ChangePercent:  changePercent,
		IOPV:           iopv,
		PremiumRate:    premiumRate,
		LimitUpPrice:   limitUp,
		LimitDownPrice: limitDown,
		T0Flag:         t0,
		OrderBook:      ob,
		DateTime:       dt,
	}
}

// round3 保留 3 位小数（ETF 价格精度均为 3 位）。
func round3(v float64) float64 {
	return float64(int64(v*1000+0.5)) / 1000
}

// GetIOPVSeries 返回 IOPV 分时序列（仅深交所提供 piciopv，沪市返回 nil）。
func (s *ETFService) GetIOPVSeries(code string) []models.ETFIOPVPoint {
	bare := bare6Code(code)
	if exchangeOf(bare) != "sz" {
		return nil
	}
	fq, err := s.szse.FetchFundQuote(bare)
	if err != nil {
		log.Warn("ETF IOPV 序列获取失败 %s: %v", bare, err)
		return nil
	}
	pts := make([]models.ETFIOPVPoint, 0, len(fq.IOPVHistory))
	for _, p := range fq.IOPVHistory {
		pts = append(pts, models.ETFIOPVPoint{Time: p.Time, Value: p.Value})
	}
	return pts
}

// GetNav 返回 ETF 净值历史序列（东财 lsjz，按日期升序的最近 days 条）。
func (s *ETFService) GetNav(code string, days int) (*models.ETFNavSeries, error) {
	return s.nav.FetchNavSeries(code, days)
}

// GetProfile 返回 ETF 静态画像（清单项 + 近期净值序列）。净值获取失败时仅返回清单项。
func (s *ETFService) GetProfile(code string) (*models.ETFProfile, error) {
	bare := bare6Code(code)
	entry, err := s.list.GetEntry(bare)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, fmt.Errorf("ETF %s 不在合并清单中（可能代码错误或已退市）", bare)
	}
	prof := models.ETFProfile{
		Code:       entry.Code,
		Name:       entry.Name,
		Exchange:   entry.Exchange,
		FundType:   entry.FundType,
		InvestType: entry.InvestType,
		ScaleWan:   entry.ScaleWan,
		Manager:    entry.Manager,
		ListDate:   entry.ListDate,
		T0Flag:     entry.T0Flag,
	}
	if nav, err := s.nav.FetchNavSeries(bare, 30); err == nil && nav != nil {
		prof.NavSeries = nav.Points
	}
	return &prof, nil
}

// GetList 返回分页后的 ETF 目录（支持关键词搜索 + 分类筛选）。
// category 可为空；非空时按 FundType 或 InvestType 包含匹配（不区分大小写）。
func (s *ETFService) GetList(page, size int, keyword, category string) (models.ETFListResult, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	catalog, err := s.list.GetCatalog()
	if err != nil {
		return models.ETFListResult{}, err
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	cat := strings.ToLower(strings.TrimSpace(category))
	filtered := make([]models.ETFListEntry, 0, len(catalog))
	for _, it := range catalog {
		if kw != "" {
			if !strings.Contains(strings.ToLower(it.Code), kw) && !strings.Contains(strings.ToLower(it.Name), kw) {
				continue
			}
		}
		if cat != "" {
			if !strings.Contains(strings.ToLower(it.FundType), cat) && !strings.Contains(strings.ToLower(it.InvestType), cat) {
				continue
			}
		}
		filtered = append(filtered, it)
	}
	total := len(filtered)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	return models.ETFListResult{
		Total: total,
		Page:  page,
		Size:  size,
		Items: filtered[start:end],
	}, nil
}
