# 05 · API 接口

本文档描述 Web 服务模式下的 HTTP 接口。

> 契约来源：`internal/webapi/routes.go`（唯一权威定义）。
> 改动路由后请运行 `python check_routes.py` 与 `python check_api_paths.py` 校验一致性。

---

## 1. 通用约定

### 1.1 基础路径

所有业务接口以 `/api/` 为前缀。

### 1.2 响应格式

成功：

```json
{ "data": <返回值> }
```

失败：

```json
{ "error": "错误信息" }
```

HTTP 状态码：

| 码 | 含义 |
|----|------|
| 200 | 成功 |
| 400 | 参数错误（JSON 解析失败、类型不匹配） |
| 401 | 未授权（令牌缺失或错误） |
| 405 | 方法不支持 |
| 500 | 服务端错误 |

> **业务级失败不一定体现在状态码上**：部分端点（如榜单类）在上游数据源
> 不可用时仍返回 **200**，失败原因放在 `data.error` 字段里。
> 调用方需同时检查 `data.items` 是否为空与 `data.error` 是否有值。

### 1.3 鉴权

除 `/api/health` 与静态资源外，所有接口都需要令牌。三种传递方式任选其一：

```http
Authorization: Bearer <token>      # 推荐
?token=<token>                     # EventSource 专用（无法设置请求头）
Cookie: jcp_token=<token>          # 前端会自动写入 localStorage 后转存
```

### 1.4 参数来源

| 类型 | 位置 | 说明 |
|------|------|------|
| 查询参数 | URL query | 标量参数，数组用逗号分隔 |
| 请求体 | JSON body | 结构体参数 |

### 1.5 HTTP 方法约定

| 操作 | 方法 |
|------|------|
| 读取 | GET |
| 创建 | POST |
| 更新 | PUT |
| 删除 | DELETE |

同一路径可承载多个方法（例：`GET /api/config` 读、`PUT /api/config` 写），
由 `Router.HandleMethod` 按 HTTP 方法分派。

---

## 2. 接口清单

### 配置

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/config` | 获取完整配置（含 AI 配置、自选股、策略等） |  |
| `PUT` | `/api/config` | 更新配置。Web 模式下 apiKey 字段若为掩码则保留服务端原值 | 请求体 |
| `GET` | `/api/openclaw/status` | 查询 OpenClaw 服务运行状态 |  |
| `POST` | `/api/ai/test` | 测试 AI 配置连通性，入参为完整 AIConfig | 请求体 |

### 自选股

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/watchlist` | 获取自选股列表 |  |
| `POST` | `/api/watchlist` | 添加自选股，入参为完整 Stock 对象 | 请求体 |
| `DELETE` | `/api/watchlist` | 删除自选股 | `股票代码` |

### 行情

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/stock/realtime` | 获取多只股票的实时行情 | `股票代码列表，逗号分隔` |
| `GET` | `/api/stock/kline` | 获取 K 线数据 | `股票代码（如 sh600000）`、`K 线周期：1m / 1d / 1w / 1mo`、`回溯天数` |
| `GET` | `/api/stock/orderbook` | 获取五档盘口 | `股票代码（如 sh600000）` |
| `GET` | `/api/stock/search` | 按关键词搜索股票 | `搜索关键词` |
| `GET` | `/api/market/status` | 获取当前交易状态 |  |
| `GET` | `/api/market/indices` | 获取大盘指数实时数据 |  |
| `GET` | `/api/market/tradedates` | 获取最近 N 个交易日 | `回溯天数` |
| `GET` | `/api/market/schedule` | 获取交易时段表 |  |

### F10 基本面

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/f10/overview` | 获取 F10 综合概览 | `股票代码（如 sh600000）` |
| `GET` | `/api/f10/valuation` | 获取估值指标 | `股票代码（如 sh600000）` |

### 会话与持仓

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/session` | 获取或创建某只股票的会话 | `股票代码`、`股票名称` |
| `GET` | `/api/session/messages` | 获取会话消息列表 | `股票代码` |
| `GET` | `/api/session/messages/clear` | 清空会话消息 | `股票代码` |
| `GET` | `/api/position` | 更新持仓信息 | `股票代码`、`持股数量`、`成本价` |

### Agent 配置

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/agents` | 获取所有专家配置 |  |
| `POST` | `/api/agents` | 新增专家配置 | 请求体 |
| `PUT` | `/api/agents` | 更新专家配置 | 请求体 |
| `DELETE` | `/api/agents` | 删除专家配置 | `记录 ID` |

### 策略

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/strategies` | 获取所有策略 |  |
| `GET` | `/api/strategies/active` | 获取当前激活策略 ID |  |
| `POST` | `/api/strategies/active` | 设置激活策略 | `记录 ID` |
| `POST` | `/api/strategies` | 新增策略 | 请求体 |
| `PUT` | `/api/strategies` | 更新策略 | 请求体 |
| `DELETE` | `/api/strategies` | 删除策略 | `记录 ID` |
| `POST` | `/api/strategies/generate` | 用 AI 生成策略，入参 {prompt} | 请求体 |
| `POST` | `/api/prompt/enhance` | 用 AI 增强角色提示词，入参 {originalPrompt, agentRole, agentName} | 请求体 |

### 会议室

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `POST` | `/api/meeting/send` | 发起 AI 会议并返回消息列表，入参 MeetingMessageRequest | 请求体 |
| `GET` | `/api/meeting/cancel` | 取消进行中的会议 | `股票代码` |
| `GET` | `/api/meeting/cancel-interrupted` | 放弃重试，保留已有结论 | `股票代码` |
| `GET` | `/api/meeting/retry` | 重试单个失败专家 | `股票代码`、`专家 ID`、`提问内容` |
| `GET` | `/api/meeting/retry-continue` | 重试失败专家并继续执行剩余专家 | `股票代码` |

### 快讯与工具

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/telegraph` | 获取财经快讯列表 |  |
| `GET` | `/api/tools` | 获取全部内置工具清单 |  |

### MCP

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/mcp/servers` | 获取 MCP 服务器配置列表 |  |
| `POST` | `/api/mcp/servers` | 新增 MCP 服务器 | 请求体 |
| `PUT` | `/api/mcp/servers` | 更新 MCP 服务器 | 请求体 |
| `DELETE` | `/api/mcp/servers` | 删除 MCP 服务器 | `记录 ID` |
| `GET` | `/api/mcp/status` | 获取所有 MCP 服务器连接状态 |  |
| `GET` | `/api/mcp/test` | 测试指定 MCP 服务器连接 | `MCP 服务器 ID` |
| `GET` | `/api/mcp/tools` | 获取指定 MCP 服务器提供的工具列表 | `MCP 服务器 ID` |

### 舆情热点

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/hottrend/platforms` | 获取支持的舆情平台列表 |  |
| `GET` | `/api/hottrend` | 获取指定平台的热搜榜单 | `舆情平台标识` |
| `GET` | `/api/hottrend/all` | 获取所有平台的热搜榜单 |  |

### 龙虎榜与板块

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/longhubang/list` | 获取龙虎榜列表 | `每页条数`、`pageNumber`、`交易日期 YYYY-MM-DD` |
| `GET` | `/api/longhubang/detail` | 获取个股龙虎榜营业部明细 | `股票代码（如 sh600000）`、`交易日期 YYYY-MM-DD` |
| `GET` | `/api/board/fundflow` | 获取板块资金流排行 | `板块分类：industry / concept / region`、`页码，从 1 开始`、`每页条数` |
| `GET` | `/api/board/moves` | 获取盘口异动榜单 | `异动类型：speed_up / speed_down / change / fund / turnover`、`页码，从 1 开始`、`每页条数` |
| `GET` | `/api/board/leaders` | 获取板块龙头候选 | `板块代码`、`返回条数上限` |

### 版本与更新

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/version` | 获取当前版本号 |  |
| `GET` | `/api/update/check` | 检查是否有新版本 |  |
| `GET` | `/api/update/apply` | 执行更新（下载并替换二进制） |  |
| `GET` | `/api/update/restart` | 重启应用 |  |

### 前端信号

| 方法 | 路径 | 说明 | 参数 |
|------|------|------|------|
| `GET` | `/api/frontend/ready` | 通知后端前端已就绪，启动推送循环 |  |

**合计 60 个业务端点**，另有 4 个 SSE/订阅端点（见 [06-事件流.md](06-事件流.md)）。

---

## 3. 详细说明

### 配置

#### `GET /api/config`

获取完整配置（含 AI 配置、自选股、策略等）

对应后端方法：`App.GetConfig`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/config"
```

#### `PUT /api/config`

更新配置。Web 模式下 apiKey 字段若为掩码则保留服务端原值

对应后端方法：`App.UpdateConfig`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/config"
```

#### `GET /api/openclaw/status`

查询 OpenClaw 服务运行状态

对应后端方法：`App.GetOpenClawStatus`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/openclaw/status"
```

#### `POST /api/ai/test`

测试 AI 配置连通性，入参为完整 AIConfig

对应后端方法：`App.TestAIConnection`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/ai/test"
```

### 自选股

#### `GET /api/watchlist`

获取自选股列表

对应后端方法：`App.GetWatchlist`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/watchlist"
```

#### `POST /api/watchlist`

添加自选股，入参为完整 Stock 对象

对应后端方法：`App.AddToWatchlist`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/watchlist"
```

#### `DELETE /api/watchlist`

删除自选股

对应后端方法：`App.RemoveFromWatchlist`

**参数**

- `symbol`（query）—— 股票代码

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/watchlist"
```

### 行情

#### `GET /api/stock/realtime`

获取多只股票的实时行情

对应后端方法：`App.GetStockRealTimeData`

**参数**

- `codes`（query）—— 股票代码列表，逗号分隔

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/stock/realtime?codes=<codes>"
```

#### `GET /api/stock/kline`

获取 K 线数据

对应后端方法：`App.GetKLineData`

**参数**

- `code`（query）—— 股票代码（如 sh600000）
- `period`（query）—— K 线周期：1m / 1d / 1w / 1mo
- `days`（query）—— 回溯天数

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/stock/kline?code=<code>&period=<period>&days=<days>"
```

#### `GET /api/stock/orderbook`

获取五档盘口

对应后端方法：`App.GetOrderBook`

**参数**

- `code`（query）—— 股票代码（如 sh600000）

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/stock/orderbook?code=<code>"
```

#### `GET /api/stock/search`

按关键词搜索股票

对应后端方法：`App.SearchStocks`

**参数**

- `keyword`（query）—— 搜索关键词

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/stock/search?keyword=<keyword>"
```

#### `GET /api/market/status`

获取当前交易状态

对应后端方法：`App.GetMarketStatus`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/market/status"
```

#### `GET /api/market/indices`

获取大盘指数实时数据

对应后端方法：`App.GetMarketIndices`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/market/indices"
```

#### `GET /api/market/tradedates`

获取最近 N 个交易日

对应后端方法：`App.GetTradeDates`

**参数**

- `days`（query）—— 回溯天数

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/market/tradedates?days=<days>"
```

#### `GET /api/market/schedule`

获取交易时段表

对应后端方法：`App.GetTradingSchedule`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/market/schedule"
```

### F10 基本面

#### `GET /api/f10/overview`

获取 F10 综合概览

对应后端方法：`App.GetF10Overview`

**参数**

- `code`（query）—— 股票代码（如 sh600000）

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/f10/overview?code=<code>"
```

#### `GET /api/f10/valuation`

获取估值指标

对应后端方法：`App.GetF10Valuation`

**参数**

- `code`（query）—— 股票代码（如 sh600000）

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/f10/valuation?code=<code>"
```

### 会话与持仓

#### `GET /api/session`

获取或创建某只股票的会话

对应后端方法：`App.GetOrCreateSession`

**参数**

- `stockCode`（query）—— 股票代码
- `stockName`（query）—— 股票名称

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/session?stockCode=<stockCode>&stockName=<stockName>"
```

#### `GET /api/session/messages`

获取会话消息列表

对应后端方法：`App.GetSessionMessages`

**参数**

- `stockCode`（query）—— 股票代码

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/session/messages?stockCode=<stockCode>"
```

#### `GET /api/session/messages/clear`

清空会话消息

对应后端方法：`App.ClearSessionMessages`

**参数**

- `stockCode`（query）—— 股票代码

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/session/messages/clear?stockCode=<stockCode>"
```

#### `GET /api/position`

更新持仓信息

对应后端方法：`App.UpdateStockPosition`

**参数**

- `stockCode`（query）—— 股票代码
- `shares`（query）—— 持股数量
- `costPrice`（query）—— 成本价

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/position?stockCode=<stockCode>&shares=<shares>&costPrice=<costPrice>"
```

### Agent 配置

#### `GET /api/agents`

获取所有专家配置

对应后端方法：`App.GetAgentConfigs`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/agents"
```

#### `POST /api/agents`

新增专家配置

对应后端方法：`App.AddAgentConfig`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/agents"
```

#### `PUT /api/agents`

更新专家配置

对应后端方法：`App.UpdateAgentConfig`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/agents"
```

#### `DELETE /api/agents`

删除专家配置

对应后端方法：`App.DeleteAgentConfig`

**参数**

- `id`（query）—— 记录 ID

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/agents"
```

### 策略

#### `GET /api/strategies`

获取所有策略

对应后端方法：`App.GetStrategies`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/strategies"
```

#### `GET /api/strategies/active`

获取当前激活策略 ID

对应后端方法：`App.GetActiveStrategyID`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/strategies/active"
```

#### `POST /api/strategies/active`

设置激活策略

对应后端方法：`App.SetActiveStrategy`

**参数**

- `id`（query）—— 记录 ID

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/strategies/active"
```

#### `POST /api/strategies`

新增策略

对应后端方法：`App.AddStrategy`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/strategies"
```

#### `PUT /api/strategies`

更新策略

对应后端方法：`App.UpdateStrategy`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/strategies"
```

#### `DELETE /api/strategies`

删除策略

对应后端方法：`App.DeleteStrategy`

**参数**

- `id`（query）—— 记录 ID

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/strategies"
```

#### `POST /api/strategies/generate`

用 AI 生成策略，入参 {prompt}

对应后端方法：`App.GenerateStrategy`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/strategies/generate"
```

#### `POST /api/prompt/enhance`

用 AI 增强角色提示词，入参 {originalPrompt, agentRole, agentName}

对应后端方法：`App.EnhancePrompt`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/prompt/enhance"
```

### 会议室

#### `POST /api/meeting/send`

发起 AI 会议并返回消息列表，入参 MeetingMessageRequest

对应后端方法：`App.SendMeetingMessage`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/meeting/send"
```

#### `GET /api/meeting/cancel`

取消进行中的会议

对应后端方法：`App.CancelMeeting`

**参数**

- `stockCode`（query）—— 股票代码

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/meeting/cancel?stockCode=<stockCode>"
```

#### `GET /api/meeting/cancel-interrupted`

放弃重试，保留已有结论

对应后端方法：`App.CancelInterruptedMeeting`

**参数**

- `stockCode`（query）—— 股票代码

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/meeting/cancel-interrupted?stockCode=<stockCode>"
```

#### `GET /api/meeting/retry`

重试单个失败专家

对应后端方法：`App.RetryAgent`

**参数**

- `stockCode`（query）—— 股票代码
- `agentId`（query）—— 专家 ID
- `query`（query）—— 提问内容

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/meeting/retry?stockCode=<stockCode>&agentId=<agentId>&query=<query>"
```

#### `GET /api/meeting/retry-continue`

重试失败专家并继续执行剩余专家

对应后端方法：`App.RetryAgentAndContinue`

**参数**

- `stockCode`（query）—— 股票代码

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/meeting/retry-continue?stockCode=<stockCode>"
```

### 快讯与工具

#### `GET /api/telegraph`

获取财经快讯列表

对应后端方法：`App.GetTelegraphList`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/telegraph"
```

#### `GET /api/tools`

获取全部内置工具清单

对应后端方法：`App.GetAvailableTools`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/tools"
```

### MCP

#### `GET /api/mcp/servers`

获取 MCP 服务器配置列表

对应后端方法：`App.GetMCPServers`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/mcp/servers"
```

#### `POST /api/mcp/servers`

新增 MCP 服务器

对应后端方法：`App.AddMCPServer`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/mcp/servers"
```

#### `PUT /api/mcp/servers`

更新 MCP 服务器

对应后端方法：`App.UpdateMCPServer`

**参数**

- 请求体 —— 完整结构体（JSON）

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/mcp/servers"
```

#### `DELETE /api/mcp/servers`

删除 MCP 服务器

对应后端方法：`App.DeleteMCPServer`

**参数**

- `id`（query）—— 记录 ID

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '<json>' \
  "http://<host>:<port>/api/mcp/servers"
```

#### `GET /api/mcp/status`

获取所有 MCP 服务器连接状态

对应后端方法：`App.GetMCPStatus`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/mcp/status"
```

#### `GET /api/mcp/test`

测试指定 MCP 服务器连接

对应后端方法：`App.TestMCPConnection`

**参数**

- `serverID`（query）—— MCP 服务器 ID

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/mcp/test?serverID=<serverID>"
```

#### `GET /api/mcp/tools`

获取指定 MCP 服务器提供的工具列表

对应后端方法：`App.GetMCPServerTools`

**参数**

- `serverID`（query）—— MCP 服务器 ID

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/mcp/tools?serverID=<serverID>"
```

### 舆情热点

#### `GET /api/hottrend/platforms`

获取支持的舆情平台列表

对应后端方法：`App.GetHotTrendPlatforms`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/hottrend/platforms"
```

#### `GET /api/hottrend`

获取指定平台的热搜榜单

对应后端方法：`App.GetHotTrend`

**参数**

- `platform`（query）—— 舆情平台标识

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/hottrend?platform=<platform>"
```

#### `GET /api/hottrend/all`

获取所有平台的热搜榜单

对应后端方法：`App.GetAllHotTrends`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/hottrend/all"
```

### 龙虎榜与板块

#### `GET /api/longhubang/list`

获取龙虎榜列表

对应后端方法：`App.GetLongHuBangList`

**参数**

- `pageSize`（query）—— 每页条数
- `pageNumber`（query）—— pageNumber
- `tradeDate`（query）—— 交易日期 YYYY-MM-DD

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/longhubang/list?pageSize=<pageSize>&pageNumber=<pageNumber>&tradeDate=<tradeDate>"
```

#### `GET /api/longhubang/detail`

获取个股龙虎榜营业部明细

对应后端方法：`App.GetLongHuBangDetail`

**参数**

- `code`（query）—— 股票代码（如 sh600000）
- `tradeDate`（query）—— 交易日期 YYYY-MM-DD

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/longhubang/detail?code=<code>&tradeDate=<tradeDate>"
```

#### `GET /api/board/fundflow`

获取板块资金流排行

对应后端方法：`App.GetBoardFundFlow`

**参数**

- `category`（query）—— 板块分类：industry / concept / region
- `page`（query）—— 页码，从 1 开始
- `pageSize`（query）—— 每页条数

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/board/fundflow?category=<category>&page=<page>&pageSize=<pageSize>"
```

**响应中的 `error` 字段**

上游数据获取失败时，`data.items` 为空且 `data.error` 携带具体原因：

```json
{
  "data": {
    "category": "industry",
    "items": null,
    "error": "东方财富接口全部域名均失败（最后错误: ...）"
  }
}
```

> 东方财富对云服务器 IP 有较强风控，榜单类接口可能拿不到数据。
> 前端会透出该原因，而不是笼统的"暂无数据"。绕过方式见
> [10-Web化改造.md §4.12](10-Web化改造.md#412-东方财富对云服务器-ip-的风控)。

#### `GET /api/board/moves`

获取盘口异动榜单

对应后端方法：`App.GetStockMoves`

**参数**

- `moveType`（query）—— 异动类型：speed_up / speed_down / change / fund / turnover
- `page`（query）—— 页码，从 1 开始
- `pageSize`（query）—— 每页条数

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/board/moves?moveType=<moveType>&page=<page>&pageSize=<pageSize>"
```

**响应中的 `error` 字段**

同 [`/api/board/fundflow`](#get-apiboardfundflow)：上游失败时 `data.error`
携带原因，`data.items` 为空。

#### `GET /api/board/leaders`

获取板块龙头候选

对应后端方法：`App.GetBoardLeaders`

**参数**

- `boardCode`（query）—— 板块代码
- `limit`（query）—— 返回条数上限

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/board/leaders?boardCode=<boardCode>&limit=<limit>"
```

### 版本与更新

#### `GET /api/version`

获取当前版本号

对应后端方法：`App.GetCurrentVersion`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/version"
```

#### `GET /api/update/check`

检查是否有新版本

对应后端方法：`App.CheckForUpdate`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/update/check"
```

#### `GET /api/update/apply`

执行更新（下载并替换二进制）

对应后端方法：`App.DoUpdate`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/update/apply"
```

#### `GET /api/update/restart`

重启应用

对应后端方法：`App.RestartApp`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/update/restart"
```

### 前端信号

#### `GET /api/frontend/ready`

通知后端前端已就绪，启动推送循环

对应后端方法：`App.NotifyFrontendReady`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://<host>:<port>/api/frontend/ready"
```

---

## 4. 特殊说明

### 4.1 `PUT /api/config` 的密钥保护

Web 模式下大模型 API Key **不会下发浏览器**：

1. `GET /api/config` 返回的 `aiConfigs[].apiKey` 为 `********`
2. `PUT /api/config` 收到的掩码值会被还原为服务端保存的明文

因此前端可以「读取配置 → 修改其它字段 → 原样保存」，不会误清空密钥。

### 4.2 幂等性

| 接口 | 幂等 |
|------|------|
| 所有 GET | 是 |
| `PUT /api/config` | 是（覆盖语义） |
| `PUT /api/agents`、`PUT /api/strategies` | 是 |
| `DELETE` 系列 | 是 |
| `POST /api/watchlist` | 否（重复添加会产生重复项） |
| `POST /api/meeting/send` | 否（每次都触发新讨论） |

### 4.3 耗时较长的接口

以下接口可能耗时数十秒，客户端需设置足够超时（Nginx 侧 `proxy_read_timeout` 已设 300s）：

- `POST /api/meeting/send` —— 多 Agent 讨论
- `POST /api/strategies/generate` —— AI 生成策略
- `POST /api/prompt/enhance` —— 提示词增强
- `POST /api/ai/test` —— 测试模型连通性

---

## 5. 校验脚本

改动路由表后**必须**运行：

```bash
python check_routes.py      # 路由表 ↔ App 方法签名、参数数量
python check_api_paths.py   # 前端调用 ↔ 后端路由（路径 + HTTP 方法）
```

两个脚本都会以退出码表示结果（0 = 通过）。

---

## 下一篇

- 想了解事件推送 → [06-事件流.md](06-事件流.md)
- 想了解存储 → [07-数据存储.md](07-数据存储.md)