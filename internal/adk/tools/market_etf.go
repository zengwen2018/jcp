package tools

import (
	"fmt"
	"strings"

	"github.com/run-bigpig/jcp/internal/services"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// 本文件提供 ETF 专属的 AI 工具。
//
// 与个股工具（get_stock_realtime / get_f10_*）的区别：
//   - ETF 没有个股 F10，但有 IOPV / 折溢价率 / 涨跌停 / T+0 标识；
//   - 非 ETF 代码不报错，而是返回提示文本引导模型改用个股工具，
//     避免模型拿到空数据后自行编造结论。

// GetETFQuoteInput 获取 ETF 实时行情入参
type GetETFQuoteInput struct {
	Codes []string `json:"codes" jsonschema:"场内 ETF 代码列表，如 sh510300, sz159915"`
}

// GetETFQuoteOutput 获取 ETF 实时行情出参
type GetETFQuoteOutput struct {
	Data string `json:"data" jsonschema:"ETF 实时行情，含市价、涨跌幅、IOPV、折溢价率、涨跌停价与 T+0/T+1 交易规则"`
}

// createETFQuoteTool 创建 ETF 实时行情工具
func (r *Registry) createETFQuoteTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, input GetETFQuoteInput) (GetETFQuoteOutput, error) {
		if len(input.Codes) == 0 {
			return GetETFQuoteOutput{Data: "请提供场内 ETF 代码，如 sh510300"}, nil
		}

		quotes := r.etfService.GetQuote(input.Codes)
		if len(quotes) == 0 {
			return GetETFQuoteOutput{Data: "未获取到 ETF 行情数据，请确认代码是否正确"}, nil
		}

		var builder strings.Builder
		for _, quote := range quotes {
			if !services.IsETFCode(quote.Symbol) {
				fmt.Fprintf(&builder, "【%s】非场内 ETF 代码，个股行情请改用 get_stock_realtime\n", quote.Symbol)
				continue
			}
			if quote.MarketPrice <= 0 && quote.IOPV <= 0 {
				fmt.Fprintf(&builder, "【%s(%s)】暂无行情数据（可能停牌或非交易时段）\n", quote.Name, quote.Symbol)
				continue
			}

			tradeRule := "T+1"
			if quote.T0Flag {
				tradeRule = "T+0"
			}
			fmt.Fprintf(&builder,
				"【%s(%s)】市价:%.3f 涨跌:%.2f%% IOPV:%.4f 折溢价:%.2f%% 涨停:%.3f 跌停:%.3f 成交量:%d份 成交额:%.0f元 %s\n",
				quote.Name, quote.Symbol, quote.MarketPrice, quote.ChangePercent,
				quote.IOPV, quote.PremiumRate, quote.LimitUpPrice, quote.LimitDownPrice,
				quote.Volume, quote.Amount, tradeRule)
		}

		return GetETFQuoteOutput{Data: builder.String()}, nil
	}

	return functiontool.New(functiontool.Config{
		Name:        "get_etf_quote",
		Description: "获取场内 ETF 实时行情：市价、涨跌幅、IOPV（参考净值）、折溢价率、涨停价/跌停价、T+0 或 T+1 交易规则",
	}, handler)
}

// GetETFProfileInput 获取 ETF 画像入参
type GetETFProfileInput struct {
	Code string `json:"code" jsonschema:"场内 ETF 代码，如 sh510300"`
}

// GetETFProfileOutput 获取 ETF 画像出参
type GetETFProfileOutput struct {
	Data string `json:"data" jsonschema:"ETF 画像：基金类别、投资类别、规模、管理人、上市日、T+0 标识与最近净值"`
}

// createETFProfileTool 创建 ETF 画像工具
func (r *Registry) createETFProfileTool() (tool.Tool, error) {
	handler := func(ctx tool.Context, input GetETFProfileInput) (GetETFProfileOutput, error) {
		code := strings.TrimSpace(input.Code)
		if code == "" {
			return GetETFProfileOutput{Data: "请提供场内 ETF 代码，如 sh510300"}, nil
		}
		if !services.IsETFCode(code) {
			return GetETFProfileOutput{
				Data: fmt.Sprintf("%s 非场内 ETF 代码；个股基本面请改用 get_f10_overview", code),
			}, nil
		}

		profile, err := r.etfService.GetProfile(code)
		if err != nil {
			// 清单里查不到属于常见的"代码错误/已退市"，返回提示而非报错，避免打断对话
			return GetETFProfileOutput{Data: fmt.Sprintf("获取 ETF 画像失败: %v", err)}, nil
		}
		if profile == nil {
			return GetETFProfileOutput{Data: "未获取到 ETF 画像数据"}, nil
		}

		tradeRule := "T+1"
		if profile.T0Flag {
			tradeRule = "T+0"
		}

		var builder strings.Builder
		fmt.Fprintf(&builder,
			"【%s(%s)】交易所:%s 基金类别:%s 投资类别:%s 规模:%s 管理人:%s 上市日:%s 交易规则:%s\n",
			profile.Name, profile.Code, dashIfEmpty(profile.Exchange),
			dashIfEmpty(profile.FundType), dashIfEmpty(profile.InvestType),
			dashIfEmpty(profile.ScaleWan), dashIfEmpty(profile.Manager),
			dashIfEmpty(profile.ListDate), tradeRule)

		if len(profile.NavSeries) > 0 {
			latest := profile.NavSeries[len(profile.NavSeries)-1]
			fmt.Fprintf(&builder, "最近净值：日期:%s 单位净值:%.4f 累计净值:%.4f 日涨跌:%.2f%%\n",
				latest.Date, latest.UnitNav, latest.AccumNav, latest.ChangePercent)
		} else {
			builder.WriteString("最近净值：暂无数据\n")
		}

		return GetETFProfileOutput{Data: builder.String()}, nil
	}

	return functiontool.New(functiontool.Config{
		Name:        "get_etf_profile",
		Description: "获取场内 ETF 产品画像：基金类别、投资类别、规模、基金管理人、上市日期、T+0/T+1 交易规则与最近净值",
	}, handler)
}

// dashIfEmpty 空值统一显示为 --，避免模型把空串误读为 0。
func dashIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "--"
	}
	return value
}
