package webapi

// Routes 返回 App 全部绑定方法对应的 HTTP 路由表。
//
// 路径约定：
//   - 读操作使用 GET，参数走查询字符串；
//   - 写操作使用 POST，结构体参数走 JSON body。
//
// 该表是前端与后端之间的唯一契约来源，前端 api/client.ts 与之保持一致。
func Routes() []Route {
	return []Route{
		// ---------- 配置 ----------
		{Method: "GetConfig", Path: "/api/config"},
		{Method: "UpdateConfig", Path: "/api/config", Args: []Arg{B()}, HTTPMethod: "PUT"},
		{Method: "GetOpenClawStatus", Path: "/api/openclaw/status"},
		{Method: "TestAIConnection", Path: "/api/ai/test", Args: []Arg{B()}},

		// ---------- 自选股 ----------
		{Method: "GetWatchlist", Path: "/api/watchlist"},
		{Method: "AddToWatchlist", Path: "/api/watchlist", Args: []Arg{B()}, HTTPMethod: "POST"},
		{Method: "RemoveFromWatchlist", Path: "/api/watchlist", Args: []Arg{Q("symbol")}, HTTPMethod: "DELETE"},

		// ---------- 行情 ----------
		{Method: "GetStockRealTimeData", Path: "/api/stock/realtime", Args: []Arg{Q("codes")}},
		{Method: "GetKLineData", Path: "/api/stock/kline", Args: []Arg{Q("code"), Q("period"), Q("days")}},
		{Method: "GetOrderBook", Path: "/api/stock/orderbook", Args: []Arg{Q("code")}},
		{Method: "SearchStocks", Path: "/api/stock/search", Args: []Arg{Q("keyword")}},
		{Method: "GetMarketStatus", Path: "/api/market/status"},
		{Method: "GetMarketIndices", Path: "/api/market/indices"},
		{Method: "GetTradeDates", Path: "/api/market/tradedates", Args: []Arg{Q("days")}},
		{Method: "GetTradingSchedule", Path: "/api/market/schedule"},

		// ---------- F10 基本面 ----------
		{Method: "GetF10Overview", Path: "/api/f10/overview", Args: []Arg{Q("code")}},
		{Method: "GetF10Valuation", Path: "/api/f10/valuation", Args: []Arg{Q("code")}},

		// ---------- 会话与持仓 ----------
		{Method: "GetOrCreateSession", Path: "/api/session", Args: []Arg{Q("stockCode"), Q("stockName")}},
		{Method: "GetSessionMessages", Path: "/api/session/messages", Args: []Arg{Q("stockCode")}},
		{Method: "ClearSessionMessages", Path: "/api/session/messages/clear", Args: []Arg{Q("stockCode")}},
		{Method: "UpdateStockPosition", Path: "/api/position", Args: []Arg{Q("stockCode"), Q("shares"), Q("costPrice")}},

		// ---------- Agent 配置 ----------
		{Method: "GetAgentConfigs", Path: "/api/agents"},
		{Method: "AddAgentConfig", Path: "/api/agents", Args: []Arg{B()}, HTTPMethod: "POST"},
		{Method: "UpdateAgentConfig", Path: "/api/agents", Args: []Arg{B()}, HTTPMethod: "PUT"},
		{Method: "DeleteAgentConfig", Path: "/api/agents", Args: []Arg{Q("id")}, HTTPMethod: "DELETE"},

		// ---------- 策略 ----------
		{Method: "GetStrategies", Path: "/api/strategies"},
		{Method: "GetActiveStrategyID", Path: "/api/strategies/active"},
		{Method: "SetActiveStrategy", Path: "/api/strategies/active", Args: []Arg{Q("id")}, HTTPMethod: "POST"},
		{Method: "AddStrategy", Path: "/api/strategies", Args: []Arg{B()}, HTTPMethod: "POST"},
		{Method: "UpdateStrategy", Path: "/api/strategies", Args: []Arg{B()}, HTTPMethod: "PUT"},
		{Method: "DeleteStrategy", Path: "/api/strategies", Args: []Arg{Q("id")}, HTTPMethod: "DELETE"},
		{Method: "GenerateStrategy", Path: "/api/strategies/generate", Args: []Arg{B()}},
		{Method: "EnhancePrompt", Path: "/api/prompt/enhance", Args: []Arg{B()}},

		// ---------- 会议室（AI 智库）----------
		{Method: "SendMeetingMessage", Path: "/api/meeting/send", Args: []Arg{B()}},
		{Method: "CancelMeeting", Path: "/api/meeting/cancel", Args: []Arg{Q("stockCode")}},
		{Method: "CancelInterruptedMeeting", Path: "/api/meeting/cancel-interrupted", Args: []Arg{Q("stockCode")}},
		{Method: "RetryAgent", Path: "/api/meeting/retry", Args: []Arg{Q("stockCode"), Q("agentId"), Q("query")}},
		{Method: "RetryAgentAndContinue", Path: "/api/meeting/retry-continue", Args: []Arg{Q("stockCode")}},

		// ---------- 快讯与工具 ----------
		{Method: "GetTelegraphList", Path: "/api/telegraph"},
		{Method: "GetAvailableTools", Path: "/api/tools"},

		// ---------- MCP ----------
		{Method: "GetMCPServers", Path: "/api/mcp/servers"},
		{Method: "AddMCPServer", Path: "/api/mcp/servers", Args: []Arg{B()}, HTTPMethod: "POST"},
		{Method: "UpdateMCPServer", Path: "/api/mcp/servers", Args: []Arg{B()}, HTTPMethod: "PUT"},
		{Method: "DeleteMCPServer", Path: "/api/mcp/servers", Args: []Arg{Q("id")}, HTTPMethod: "DELETE"},
		{Method: "GetMCPStatus", Path: "/api/mcp/status"},
		{Method: "TestMCPConnection", Path: "/api/mcp/test", Args: []Arg{Q("serverID")}},
		{Method: "GetMCPServerTools", Path: "/api/mcp/tools", Args: []Arg{Q("serverID")}},

		// ---------- 热点舆情 ----------
		{Method: "GetHotTrendPlatforms", Path: "/api/hottrend/platforms"},
		{Method: "GetHotTrend", Path: "/api/hottrend", Args: []Arg{Q("platform")}},
		{Method: "GetAllHotTrends", Path: "/api/hottrend/all"},

		// ---------- 龙虎榜与板块 ----------
		{Method: "GetLongHuBangList", Path: "/api/longhubang/list", Args: []Arg{Q("pageSize"), Q("pageNumber"), Q("tradeDate")}},
		{Method: "GetLongHuBangDetail", Path: "/api/longhubang/detail", Args: []Arg{Q("code"), Q("tradeDate")}},
		{Method: "GetBoardFundFlow", Path: "/api/board/fundflow", Args: []Arg{Q("category"), Q("page"), Q("pageSize")}},
		{Method: "GetStockMoves", Path: "/api/board/moves", Args: []Arg{Q("moveType"), Q("page"), Q("pageSize")}},
		{Method: "GetBoardLeaders", Path: "/api/board/leaders", Args: []Arg{Q("boardCode"), Q("limit")}},

		// ---------- 版本与更新 ----------
		{Method: "GetCurrentVersion", Path: "/api/version"},
		{Method: "CheckForUpdate", Path: "/api/update/check"},
		{Method: "DoUpdate", Path: "/api/update/apply"},
		{Method: "RestartApp", Path: "/api/update/restart"},

		// ---------- 前端就绪信号 ----------
		{Method: "NotifyFrontendReady", Path: "/api/frontend/ready"},
	}
}
