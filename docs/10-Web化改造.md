# 10 · Web 化改造

本文记录把 Wails 桌面应用改造成 Web 服务模式的完整过程：动机、方案、关键决策与踩坑。

改造提交：`e01c726`（基于 v0.3.5 / `44af274`），41 个文件，+3323 行。

---

## 1. 动机与约束

### 1.1 原始形态

jcp 是 Wails v2 桌面应用：

```
Go 后端（App，60 个方法）+ React 前端 + WebView 窗口
```

`main.go` 通过 `wails.Run()` 启动，直接开一个 1920×1080 的原生窗口。前端通过 Wails 生成的绑定（`frontend/wailsjs/go/main/App.js`）直接调用 Go 方法，65 个绑定方法全部是同步调用。

### 1.2 约束

**业务逻辑零改动**。`app.go` 有 1700 行、`internal/services` 有 9400 行，承载着行情抓取、多 Agent 编排、记忆管理等核心逻辑。重写不现实，也不必要。

### 1.3 目标形态

```
浏览器 ──REST──> HTTP API（反射绑定 60 个方法）
       ──SSE───> 事件流（替代 Wails 事件通道）
```

### 1.4 方案权衡

| 方案 | 工作量 | 结果 | 结论 |
|------|-------|------|------|
| **A. 前端构建 + Nginx + 重写 HTTP API** | 大 | 真正 Web 化 | ✅ **采纳** |
| B. Xvfb + x11vnc 跑桌面版 | 小 | 能用但延迟高、吃内存、体验差 | ❌ |
| C. 下载官方 Linux 版跑云桌面 | 极小 | 用不上服务器 | ❌ |

方案 A 的核心洞察：**业务方法本身与运行形态无关**，需要适配的只有"如何调用"和"如何接收事件"两个方向。

---

## 2. 改造总览

### 2.1 依赖倒置

```
改造前                                    改造后
─────────────────────────────            ─────────────────────────────
App 方法 ← 前端直接调 Wails 绑定         App 方法 ← 抽象层（EventBus/Desktop）
         ← runtime.EventsEmit 推事件              ← EventBus.Emit → SSE / Wails
```

`App` 不再直接依赖 Wails runtime，而是依赖两个接口：

```go
type EventBus interface {
    Emit(ctx context.Context, name string, data any)
    On(name string, handler Handler) func()
    Listen(ctx context.Context, name string, fn SubscribeFunc) func()
    Off(name string)
    Close()
}

type Desktop interface {
    OpenURL(ctx context.Context, url string)
    WindowMinimise(ctx context.Context)
    WindowToggleMaximise(ctx context.Context)
    Quit(ctx context.Context)
}
```

### 2.2 新增文件

| 文件 | 行数 | 职责 |
|------|------|------|
| `internal/pkg/eventbus/eventbus.go` | 317 | 接口、Hub、Client、订阅键过滤 |
| `internal/pkg/eventbus/wails_bus.go` | 54 | 桌面实现（转发 Wails runtime） |
| `internal/pkg/eventbus/web_bus.go` | 174 | Web 实现（SSE 分发） |
| `internal/pkg/eventbus/eventbus_test.go` | 109 | 单元测试 |
| `internal/pkg/platform/desktop.go` | 35 | 平台能力接口 + Nop 实现 |
| `internal/pkg/platform/wails_desktop.go` | 32 | 桌面实现 |
| `internal/pkg/platform/web_desktop.go` | 23 | Web 实现（空操作） |
| `internal/webapi/router.go` | 416 | 反射路由、参数解析、响应包装 |
| `internal/webapi/routes.go` | 100 | 60 个端点的路由表 |
| `internal/webapi/sse.go` | 181 | SSE 端点 + 订阅接口 |
| `internal/webapi/auth.go` | 144 | 鉴权 + CORS |
| `internal/webapi/secrets.go` | 138 | API Key 脱敏与还原 |
| `internal/webapi/middleware.go` | 14 | 中间件容器 |
| `web_main.go` | 191 | Web 模式入口 |

### 2.3 修改文件

| 文件 | 改动 |
|------|------|
| `app.go` | 移除 Wails runtime 依赖；`NewAppWithDeps(bus, desktop)` 注入；`startup/shutdown` 改为公开 |
| `main.go` | 加 `//go:build !web` |
| `internal/services/market_pusher.go` | 18 处 `runtime.EventsEmit` → `bus.Emit`；新增 3 个订阅方法 |
| `internal/services/update_service.go` | 移除 `wailsruntime` 依赖 |
| `internal/pkg/paths/paths.go` | 支持 `JCPDATA_DIR` 环境变量 |
| `frontend/src/api/client.ts` | 新增：fetch 封装、令牌管理 |
| `frontend/src/api/runtime.ts` | 新增：运行时探测 |
| `frontend/src/api/events.ts` | 新增：事件订阅抽象 |
| `frontend/src/services/*.ts`（8 个） | 全部改为 `callBackend()` |
| `frontend/src/hooks/useMarketEvents.ts` | 事件订阅改用 `api/events` |
| `frontend/src/hooks/useMarketStatus.ts` | 改用 `callBackend()` |

### 2.4 前端运行时适配

关键设计：**业务代码不感知运行形态**。

```typescript
// frontend/src/api/runtime.ts
export function isDesktop(): boolean {
  const bindings = (window as unknown as { go?: WailsBindings }).go;
  return Boolean(bindings?.main?.App);
}

export async function callBackend<T>(
  method: string, path: string, args: unknown[] = [],
  options?: { query?; body?; httpMethod? },
): Promise<T> {
  if (isDesktop()) {
    const fn = wailsApp()[method];
    return (await fn(...args)) as T;
  }
  // Web 模式：走 HTTP
}
```

Wails 桌面模式会把 `window.go.main.App` 注入全局；Web 模式下这个对象不存在，据此自动分流。

#### 桌面专属能力

窗口最小化/最大化/关闭、获取窗口尺寸这类操作只在桌面模式有意义。Web 模式下
统一降级为安全空操作，由 `frontend/src/api/runtime.ts` 集中适配：

| 函数 | 桌面模式 | Web 模式 |
|------|---------|---------|
| `windowGetSize()` | 转发 Wails runtime | 返回浏览器视口尺寸（语义最接近） |
| `windowSetSize(w, h)` | 转发 Wails runtime | 空操作 |
| `windowIsMaximised()` | 转发 Wails runtime | 恒为 `false` |
| `windowMinimize()` / `windowMaximize()` / `windowClose()` | 转发 Wails runtime | 空操作（用户自行关闭标签页） |
| `openURL(url)` | 转发 `App.OpenURL` | `window.open(url, '_blank', 'noopener,noreferrer')` |

#### 事件订阅

统一走 `frontend/src/api/events.ts` 的 `onEvent(name, handler)`：

- 桌面模式转 `runtime.EventsOn`
- Web 模式走 SSE 信封，由 `WebEventBus` 按 `event` 字段分发

回调签名兼容 Wails 的多参数形式：

```typescript
type EventHandler<T = unknown> = (data: T, ...rest: unknown[]) => void;
```

---

## 3. 关键技术决策

### 3.1 反射式路由绑定

**决策**：用反射统一适配，而不是为 60 个方法手写 handler。

```go
fn := b.receiver.MethodByName(rt.Method)
// 参数类型从 ft.In(i) 取得，逐个解析
// 返回值通过 out[0].Interface() 序列化
```

**代价**：失去了编译期类型检查。**缓解**：路由表显式声明参数个数，`Bind` 时校验：

```go
if argCount != len(rt.Args) {
    return fmt.Errorf("绑定 %s 失败：参数数量不匹配，方法需要 %d 个，路由表声明 %d 个",
        rt.Method, argCount, len(rt.Args))
}
```

**收益**：新增一个后端方法只需在路由表加一行，不用碰 handler 代码。

### 3.2 路由表作为唯一契约

`internal/webapi/routes.go` 是前后端之间的唯一真相来源：

```go
{Method: "GetKLineData", Path: "/api/stock/kline", Args: []Arg{Q("code"), Q("period"), Q("days")}},
```

配套两个校验脚本（`check_routes.py` / `check_api_paths.py`），改路由后必跑，防止前后端不匹配导致 404。

### 3.3 同路径多方法

标准库 `http.ServeMux` **不支持**同路径不同方法（第二次注册会 panic）：

```
panic: pattern "/api/strategies/active" conflicts with pattern "/api/strategies/active"
```

JCP 需要 `GET /api/config`（读）和 `PUT /api/config`（写）共存，因此自实现了方法分派：

```go
type Router struct {
    mux            *http.ServeMux
    methodHandlers map[string]map[string]http.HandlerFunc
    // registered 记录已向 mux 注册过入口的路径，
    // 保证同一路径只注册一次（ServeMux 不允许重复注册）。
    registered map[string]bool
}

func (r *Router) HandleMethod(pattern, method string, h http.HandlerFunc) {
    handlers, ok := r.methodHandlers[pattern]
    if !ok {
        handlers = make(map[string]http.HandlerFunc)
        r.methodHandlers[pattern] = handlers
    }
    handlers[method] = h

    // 每个路径只向 mux 注册一次入口。
    //
    // 必须用独立的 registered 集合判断，不能用"handlers 长度 == 1"：
    // 同一方法被注册两次时长度仍为 1，会导致 mux 重复注册而 panic；
    // 也不能用栈式 pendingPatterns：路由注册顺序不保证同一路径连续
    // （先注册 /api/agents(GET)、再 /api/config(PUT)、最后 /api/agents(DELETE)），
    // 栈顶已不是当前路径，判断会失败导致该方法未挂上（返回 404）。
    if !r.registered[pattern] {
        r.registered[pattern] = true
        r.mux.HandleFunc(pattern, func(w http.ResponseWriter, req *http.Request) {
            hs := r.methodHandlers[pattern]
            if handler, ok := hs[req.Method]; ok {
                handler(w, req)
                return
            }
            // ...
        })
    }
}
```

> **两种"看起来对"的实现都会出错**：`len(handlers) == 1` 会在同方法重复注册时
> panic；栈式 `pendingPatterns` 会在注册顺序不连续时漏挂方法（404）。
> 正确做法是用独立的 `registered map[string]bool`。
> 详见 [4.3 路由重复注册 panic](#43-路由重复注册-panic) 与
> [4.10 路由注册顺序导致的 404](#410-路由注册顺序导致的-404)。

### 3.4 SSE 统一信封

**决策**：不用 SSE 原生的 `event:` 行，改用统一信封 `{event, data}`。

**原因**：会议事件名带股票代码后缀（`meeting:message:sh600000`），浏览器侧无法预注册 `addEventListener`。用 `onmessage` 统一接收后按信封里的 `event` 字段分发，能处理任意动态事件名。

### 3.5 API Key 脱敏

见 [09-安全模型.md](09-安全模型.md) §3。要点：读取脱敏 + 保存还原，浏览器永远拿不到明文。

### 3.6 订阅走 HTTP 直连

Web 模式没有 Wails 事件通道，浏览器的订阅请求无法像桌面版那样通过 `EventsEmit` 到达 pusher。

**决策**：定义 `webapi.Subscriber` 接口，由 `App` 实现，HTTP 层直接调用：

```go
type Subscriber interface {
    SetStockSubscriptions(codes []string)
    SetOrderBookSubscription(code string)
    SetKLineSubscription(code, period string)
}
```

调用链：

```
POST /api/market/subscribe
  ├─► Hub.SetGlobalSubscription(codes)      事件过滤用
  └─► App.SetStockSubscriptions(codes)      → MarketDataPusher 决定推哪些股票
```

### 3.7 事件过滤按名称显式声明

**决策**：用 `filteredEvents` 映射表显式声明哪些事件按订阅过滤，而不是从负载推断。

原因见 §4.6 —— 靠负载推断会误过滤大盘指数事件。

### 3.8 事件键提取用反射

负载类型多样（结构体、切片、map、指针），用反射统一处理：

```go
var keyFields = []string{"Code", "Symbol", "code", "symbol"}
```

切片会**逐元素**提取（股票列表可能含多个代码），任一命中即投递。

---

## 4. 踩坑记录

以下问题都在实际运行中暴露，逐条记录以免重蹈。

### 4.1 反射 Value 嵌套

```go
// ❌ 错误：b.receiver 已经是 reflect.Value，再套 ValueOf 拿到的是「Value 的 Value」
fn := reflect.ValueOf(b.receiver).MethodByName(rt.Method)

// ✅ 正确
fn := b.receiver.MethodByName(rt.Method)
```

**症状**：`绑定 GetConfig 失败：App 上不存在该方法`

### 4.2 bound method 的参数个数

`MethodByName` 返回的 **bound method**，receiver 已从参数列表剥离：

```go
// ❌ 错误
argCount := ft.NumIn() - 1        // 变成 -1

// ✅ 正确
argCount := ft.NumIn()            // 就是业务参数数
```

调用时也不需要传 receiver：

```go
// ❌ 错误
in := make([]reflect.Value, e.ft.NumIn())
in[0] = b.receiver

// ✅ 正确
out := e.fn.Call(args)
```

**症状**：`参数数量不匹配，方法需要 -1 个，路由表声明 0 个`

### 4.3 路由重复注册 panic

用 `len(handlers) == 1` 判断"是否首次注册"是错的——**同一方法注册两次**时 len 仍为 1，会重复调 `mux.HandleFunc` 导致 panic。

**修复**：用独立的 `registered map[string]bool` 记录已注册入口的路径。

**症状**：`panic: pattern "/api/strategies/active" conflicts with pattern ...`

### 4.4 指针类型 body 参数

`UpdateConfig(config *models.AppConfig)` 的参数是指针：

```go
baseType := t
isPtr := baseType.Kind() == reflect.Ptr
if isPtr {
    baseType = baseType.Elem()
}
ptr := reflect.New(baseType)
json.Unmarshal(raw, ptr.Interface())
args[i] = ptr        // 指针类型直接给 ptr
// args[i] = ptr.Elem()   ← 错，值类型才用 Elem()
```

**症状**：`请求体解析失败: json: cannot unmarshal object into Go value of type models.AppConfig`

### 4.5 脱敏还原的包裹层

还原器的输出会被**直接反序列化进目标类型**。如果把整个请求体（含 `data` 包裹层）当作配置对象，字段会全部落空，**配置文件被清空**。

**修复**：还原器返回**内层对象**，而不是重新包一层：

```go
// ✅ 返回内层
restored, ok := restoreConfigJSON(raw, getOriginal())
return restored
```

**症状**：保存配置后 `config.json` 变成全零值：

```json
{"theme":"", "aiConfigs":null, "aiRetryCount":0, ...}
```

### 4.6 事件过滤误伤大盘指数

**原实现**：从负载提取 `code`/`symbol` 判断是否订阅。

**问题**：`MarketIndex` 也有 `Code` 字段（`sh000001`），于是大盘指数事件被当成"订阅了 sh000001 的事件"，而用户订阅的是 `sh600000` → **大盘指数被丢弃**。

**更隐蔽的问题**：`eventKey` 的 switch 只处理 `[]any`，而实际负载是 `[]models.Stock`（具体类型）→ 类型不匹配直接走 `default` 返回空 → 股票事件也过滤失效。

**修复**：改为按事件名显式声明 + 反射提取：

```go
var filteredEvents = map[string]bool{
    "market:stock:update":    true,
    "market:orderbook:update": true,
    "market:kline:update":     true,
}
```

**这个 bug 由单元测试捕获**：

```go
func TestWebBusDeliversEventWithoutKey(t *testing.T) {
    // 订阅 sh600000，push 一个 MarketIndex，应该能收到
}
```

### 4.7 推送时机的误判

收盘时段，股票推送是 `normalCount%10 == 0` 才执行，即**每 30 秒一次**。测试时观察 14 秒就断言"没推送"，误判为 bug。

**教训**：验证推送必须覆盖完整周期（≥40 秒）。

### 4.8 Windows 私钥权限

`icacls key /inheritance:r` + `/grant:r "$USER:(R)"` 在 Git Bash 下不生效——`$USER` 变量未展开，导致授权对象为空。

**修复**：写完整域用户名：

```bash
icacls jcp_deploy.pem /grant:r "ZENGWEN\57618:(R)" "BUILTIN\Administrators:(R)"
```

### 4.9 Go 安装的解压陷阱

`Expand-Archive` 解压不完整（只出 16 个目录，缺 `context`、`embed`、`net`、`http` 等），编译时报：

```
package context is not in std (D:\Program Files\go\src\context)
```

**修复**：用 Python 解压（14168 个成员，75 个目录）：

```python
import zipfile
zipfile.ZipFile('go.zip').extractall(r'D:\Program Files')
```

另外 Git Bash 的 `tar` 不认 zip 格式，`tar --force-local` 会报 "not a tar archive"。

---

### 4.10 路由注册顺序导致的 404

修完 4.3 的 panic 后换成了栈式 `pendingPatterns`，以为万无一失，实测仍有
`DELETE /api/agents` 返回 **404**。

**原因**：路由注册顺序**不保证同一路径连续**。实际顺序是

```
/api/agents       (GET)
/api/config       (PUT)
/api/agents       (DELETE)   ← 此时栈顶是 /api/config，不是 /api/agents
```

栈顶判断失败 → DELETE 分支从未挂到 mux 入口 → 404。

**修复**：改用独立的 `registered map[string]bool`，与注册顺序完全解耦。

**教训**：判断"是否已处理过"时，不要用依赖历史顺序的栈式结构，
用集合（set/map）表达"已存在"这个语义。

### 4.11 漏改相对路径的 Wails 引用，页面白屏

首次改造只清理了 `@wailsjs/` **别名路径**，漏掉了两类：

- **相对路径**导入：`../wailsjs/...`、`../../wailsjs/...`
- **全局对象直接访问**：`window.go.main.App`、`window.runtime.EventsOnMultiple`

Web 模式下 React 渲染时直接抛 TypeError：

```
Cannot read properties of undefined (reading 'main')
Cannot read properties of undefined (reading 'EventsOnMultiple')
```

页面停在"加载中…"或全黑。受影响 7 个文件：`App.tsx`、`AgentRoom.tsx`、
`SettingsDialog.tsx`、`HotTrendDialog.tsx`、`LongHuBangDialog.tsx`、
`MarketMovesDialog.tsx`、`WelcomePage.tsx`。

**教训**：清理框架依赖时必须**同时覆盖三类**：

1. 别名路径（`@wailsjs/...`）
2. 相对路径（`../wailsjs/...`）
3. 全局对象直接访问（`window.xxx`）

用 `grep -rn "wailsjs\|window\." --include="*.ts" --include="*.tsx"` 全量扫，
只 grep 别名路径会漏。

### 4.12 东方财富对云服务器 IP 的风控

盘口异动、板块资金流无数据，接口报 EOF。**同一 URL 在本机返回完整数据
（异动榜 5563 条），在服务器上连接被重置**。

**根因**：`push2.eastmoney.com` 对云服务器 IP 有较强风控。数据源层面的限制，
**与 Web 改造无关** —— 原版桌面版部署到同一台机器同样中招。

**缓解**（`internal/services/market_service.go`）：

- 新增 `fetchEastMoneyJSON`，失败时按 `push2` → `push2his` → `82.push2` →
  `48.push2` 依次回退，缓解轻度风控
- `BoardFundFlowList` / `StockMoveList` 增加 `Error` 字段，前端在无数据时
  透出后端的具体原因，而不是笼统的"暂无数据"

**绕过**：所有东财服务都走 `proxy.GetManager().GetClientWithTimeout()`，
在「设置 → 代理」填一个境内可用的 HTTP 代理即可，代理在这一层生效。

## 5. 验证

### 5.1 编译

```bash
go build .                      # 桌面模式
go build -tags web              # Web 模式
CGO_ENABLED=0 GOOS=linux go build -tags web -o jcp-server .   # 交叉编译
```

三种都通过。

### 5.2 静态检查

```bash
go vet -tags web ./internal/...     # 无告警
go test -tags web ./internal/pkg/eventbus/   # ok
```

### 5.3 契约校验

```bash
python check_routes.py       # 路由表 ↔ 方法签名
python check_api_paths.py    # 前端 46 处调用 ↔ 后端路由
python check_docs.py         # 文档 ↔ 代码语义一致
```

`check_docs.py` 是**文档一致性的守门脚本**，覆盖：

| 检查项 | 内容 |
|--------|------|
| 语义断言 | 代码里的关键实现特征 ↔ 文档描述，双向核对 |
| 数量断言 | 工具 37 / 端点 60 / service 8 / 组件 15，文档写的数字必须与代码一致 |
| 链接 | 文档间跳转的文件是否存在 |
| 锚点 | 文档内锚点链接是否有效 |
| 代码块 | Markdown 代码围栏是否闭合 |

> **改代码或配置后必须重跑它**。若报"代码已改，文档未同步"，
> 说明文档落后于实现，需补齐后再提交。

### 5.4 端到端实测

生产环境（轻量应用服务器）实测：

| 项目 | 结果 |
|------|------|
| **API 契约测试** | **45 / 45 通过**（鉴权边界、60 端点、写操作、错误处理、密钥脱敏） |
| **UI 交互测试** | **26 / 26 通过**（首屏、K线 4 周期、F10、3 个弹窗、设置面板、搜索、AI 输入、SSE） |
| **控制台** | 全程 **0 个 JS 错误、0 个 4xx 响应** |
| 鉴权 | 无令牌 401 / Bearer 200 / Query 200 / 错误令牌 401 |
| 行情数据 | 真实（浦发 9.99 +1.42%，平安 11.54 +2.03%，上证 3651.43） |
| 配置脱敏往返 | 写入存明文 → 读取 `********` → 掩码回传后明文保留、其它字段正常 |
| SSE | `connected` + `market:indices:update` + `market:stock:update` + 心跳 |
| 前端页面 | 三栏布局完整渲染，JS 产物 1MB 可访问 |

自动化测试脚本放在工作区，可直接重跑回归：

| 脚本 | 覆盖内容 |
|------|---------|
| `test_api.js` | 45 项：鉴权边界 5、只读端点 34、写端点 6、错误处理 3、密钥脱敏 |
| `test_ui.js` | 26 项：首屏、自选股、K线、F10、工具栏、设置、弹窗、搜索、AI 输入、SSE、控制台错误 |
| `test_secret.js` / `test_secret2.js` | 密钥脱敏全链路（含服务端文件核对） |

> 浏览器验证用 **Node 脚本调 playwright-core + 本机 Chrome**：
> `executablePath: 'C:/Users/57618/AppData/Local/Google/Chrome/Application/chrome.exe'`
> （本机缺 `chromium_headless_shell`）。本机代理需清空：
> `NO_PROXY=<host> HTTP_PROXY= HTTPS_PROXY=`

---

## 6. 改造后的目录结构

```
jcp/
├── app.go                    App（60 个方法）
├── main.go                   桌面入口（//go:build !web）
├── web_main.go               Web 入口（//go:build web）
├── internal/
│   ├── models/               领域模型
│   ├── services/             数据与业务服务
│   ├── adk/                  Agent 运行时、模型工厂、37 个工具、MCP
│   ├── meeting/              多 Agent 编排
│   ├── memory/               长期记忆
│   ├── agent/                专家容器
│   ├── openclaw/             OpenClaw 集成
│   ├── embed/                资源嵌入
│   ├── logger/               日志
│   ├── pkg/                  paths / proxy / eventbus / platform
│   └── webapi/               HTTP 路由、SSE、鉴权、密钥脱敏
├── frontend/
│   └── src/
│       ├── api/              client / runtime / events（新增）
│       ├── components/       20 个组件
│       ├── hooks/            useMarketEvents / useMarketStatus
│       ├── services/         8 个服务模块
│       └── types.ts
├── docs/                     本文档目录
├── deploy/                   nginx / systemd / 部署脚本 / 文档
├── Dockerfile.web
├── docker-compose.web.yml
├── check_routes.py           契约校验脚本
└── check_api_paths.py        契约校验脚本
```

---

## 7. 后续可做

| 方向 | 说明 |
|------|------|
| WebSocket 替代 SSE | SSE 是单向的，若未来需要客户端→服务端高频推送可考虑 |
| 响应压缩 | 大数据量响应（K 线、F10）可加 gzip |
| API Key 加密存储 | 当前 `config.json` 明文，可用系统 keyring 或加密文件 |
| 多用户支持 | 当前是单用户模型（全局订阅状态），多用户需改订阅隔离 |
| 限流与审计 | Nginx 层限流 + 操作审计日志 |
| 原子写 | 配置写入改为「临时文件 + rename」 |

---

## 回到索引

- [README.md](README.md) — 文档索引
- [08-部署运维.md](08-部署运维.md) — 部署步骤
- [09-安全模型.md](09-安全模型.md) — 安全机制
