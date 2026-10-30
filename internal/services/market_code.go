package services

import "strings"

// 本文件是"代码解析"的统一出口。
//
// 既有的 f10_service.go / market_service.go 里散落着若干硬编码前缀判断，
// 本次不做大规模收口（避免回归），但新增的腾讯 Provider 与 ETF 工具
// 一律通过这里的函数解析代码，保证新代码只有一处口径。

// 场内基金（ETF/LOF）所在代码段。
//
// 沪市：51/52/53/55/56/58（与沪市 947 只 ETF 的全量实测分布一致）
// 深市：15/16/18
var (
	etfSHPrefixes = []string{"51", "52", "53", "55", "56", "58"}
	etfSZPrefixes = []string{"15", "16", "18"}
)

// ExchangeOf 返回代码所属交易所前缀（sh/sz/bj），无法判定时返回空串。
//
// 优先信任调用方给出的显式前缀（如 sh000001 这类沪市指数，其数字部分 0 开头
// 若按首位推断会被误判为深市）；没有前缀时才按首位推断：
// 5/6 → sh，0/1/3 → sz，4/8 → bj。
func ExchangeOf(code string) string {
	lower := strings.ToLower(strings.TrimSpace(code))
	if strings.HasPrefix(lower, "sh") || strings.HasPrefix(lower, "sz") || strings.HasPrefix(lower, "bj") {
		return lower[:2]
	}

	bare := bare6Code(code)
	if len(bare) != 6 {
		return ""
	}
	switch bare[0] {
	case '5', '6':
		return "sh"
	case '0', '1', '3':
		return "sz"
	case '4', '8':
		return "bj"
	}
	return ""
}

// MarketOf 返回所属市场标识。
//
// 目前仅支持 A股，恒返回 "cn"；保留函数是为将来扩展港股/美股留出调用点。
func MarketOf(code string) string {
	_ = code
	return "cn"
}

// IsETFCode 判断是否为场内基金（ETF/LOF）代码。
//
// 与前端 isETFCode 保持同一套前缀规则，避免前后端判定不一致。
func IsETFCode(code string) bool {
	bare := bare6Code(code)
	if len(bare) != 6 {
		return false
	}
	exchange := ExchangeOf(code)
	prefix := bare[:2]
	switch exchange {
	case "sh":
		return containsPrefix(etfSHPrefixes, prefix)
	case "sz":
		return containsPrefix(etfSZPrefixes, prefix)
	}
	return false
}

// AssetTypeOf 返回资产大类：etf / index / stock / unknown。
//
//   - 场内基金（ETF/LOF）→ etf
//   - 沪市 000 段、深市 399 段 → index
//   - 6/0/3 开头 → stock
func AssetTypeOf(code string) string {
	if IsETFCode(code) {
		return "etf"
	}

	bare := bare6Code(code)
	if len(bare) != 6 {
		return "unknown"
	}
	exchange := ExchangeOf(code)
	if exchange == "sh" && strings.HasPrefix(bare, "000") {
		return "index"
	}
	if exchange == "sz" && strings.HasPrefix(bare, "399") {
		return "index"
	}
	if bare[0] == '6' || bare[0] == '0' || bare[0] == '3' {
		return "stock"
	}
	return "unknown"
}

// SecurityTypeOf 返回证券细分类：etf / lof / index / stock / unknown。
//
// 注意判定顺序：LOF 必须先于通用场内基金判断 —— 深市 16 段与沪市 501/502 段
// 虽然同属"场内基金"，但细分应归为 LOF 而不是 ETF。
func SecurityTypeOf(code string) string {
	bare := bare6Code(code)
	if len(bare) == 6 {
		exchange := ExchangeOf(code)
		if exchange == "sz" && strings.HasPrefix(bare, "16") {
			return "lof"
		}
		if exchange == "sh" && (strings.HasPrefix(bare, "501") || strings.HasPrefix(bare, "502")) {
			return "lof"
		}
	}

	switch AssetTypeOf(code) {
	case "etf":
		return "etf"
	case "index":
		return "index"
	case "stock":
		return "stock"
	}
	return "unknown"
}

// ToProviderCode 把代码规范为「小写交易所前缀 + 6 位」，供行情接口直接使用。
//
// 例如 510300 → sh510300、000001（平安银行）→ sz000001。
// 无法判定交易所时原样返回裸代码（由调用方接口自行处理）。
func ToProviderCode(code string) string {
	bare := bare6Code(code)
	if len(bare) != 6 {
		return strings.ToLower(strings.TrimSpace(code))
	}
	exchange := ExchangeOf(code)
	if exchange == "" {
		return bare
	}
	return exchange + bare
}

func containsPrefix(prefixes []string, target string) bool {
	for _, prefix := range prefixes {
		if prefix == target {
			return true
		}
	}
	return false
}
