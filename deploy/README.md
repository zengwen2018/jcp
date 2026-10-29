# JCP 韭菜盘 —— Web 服务模式部署指南

本文档说明如何把 jcp（AI 股票分析桌面应用）改造后的 **Web 服务版本**部署到 Linux 服务器。

改造的核心思路：原本的 Wails 桌面应用（Go 后端 + WebView 窗口）拆成了
**HTTP API 服务 + SSE 事件流 + 浏览器前端** 三部分，前端代码保持一份，两种形态通用。

---

## 一、架构总览

```
浏览器
  │
  │  ① REST 请求      GET/POST /api/**
  │  ② SSE 长连接     GET /api/events
  ▼
┌─────────────────────────────────────────┐
│  Nginx（可选）                           │
│  · TLS 终止                             │
│  · SSE 关闭缓冲（关键）                 │
│  · 静态资源缓存                         │
└─────────────────────────────────────────┘
  │
  ▼
┌─────────────────────────────────────────┐
│  jcp-server（Go 二进制）                 │
│  ├─ webapi     HTTP 路由 + 反射绑定      │
│  ├─ eventbus   事件总线（SSE 分发）       │
│  ├─ App        原 63 个业务方法（未改动） │
│  └─ services   行情 / AI / 记忆 / MCP    │
└─────────────────────────────────────────┘
```

**关键设计**：原有的 `App` 结构体和 63 个绑定方法**一行业务逻辑都没改**，
只是在它们外面加了一层 HTTP 适配。这样既保证了行为一致，又不用重写上千行代码。

---

## 二、代码改动清单

### 后端（Go）

| 文件 | 说明 |
|------|------|
| `internal/pkg/eventbus/eventbus.go` | 新增。事件总线抽象：接口、Hub、Client、订阅键过滤 |
| `internal/pkg/eventbus/wails_bus.go` | 新增。桌面模式实现，转发到 Wails runtime |
| `internal/pkg/eventbus/web_bus.go` | 新增。Web 模式实现，SSE 分发 + 全局订阅 |
| `internal/pkg/platform/desktop.go` | 新增。桌面专属能力抽象（开链接、窗口控制、退出） |
| `internal/pkg/platform/wails_desktop.go` | 新增。桌面实现 |
| `internal/pkg/platform/web_desktop.go` | 新增。Web 实现（空操作） |
| `internal/webapi/router.go` | 新增。反射式 HTTP 路由 + 参数解析 + 响应包装 |
| `internal/webapi/routes.go` | 新增。63 个方法的路由表（路径与参数名的唯一契约） |
| `internal/webapi/sse.go` | 新增。SSE 长连接端点 + 订阅接口 |
| `internal/webapi/auth.go` | 新增。Bearer/Query/Cookie 三种鉴权方式 + CORS |
| `internal/webapi/secrets.go` | 新增。API Key 脱敏与还原 |
| `internal/webapi/middleware.go` | 新增。中间件容器 |
| `web_main.go` | 新增。Web 模式入口（`//go:build web`） |
| `main.go` | 修改。加 `//go:build !web`，改用 `app.Startup/Shutdown` |
| `app.go` | 修改。移除 Wails runtime 依赖，改用 eventbus/platform 注入 |
| `internal/services/market_pusher.go` | 修改。18 处 `runtime.EventsEmit` → `bus.Emit` |
| `internal/services/update_service.go` | 修改。移除 `wailsruntime` 依赖 |

### 前端（TypeScript）

| 文件 | 说明 |
|------|------|
| `frontend/src/api/client.ts` | 新增。fetch 封装、令牌管理、错误处理 |
| `frontend/src/api/runtime.ts` | 新增。运行时适配：桌面走 Wails 绑定，Web 走 HTTP |
| `frontend/src/api/events.ts` | 新增。事件订阅：桌面走 EventsOn，Web 走 EventSource |
| `frontend/src/services/*.ts` | 修改。8 个服务文件全部改为 `callBackend()` 统一调用 |
| `frontend/src/hooks/useMarketEvents.ts` | 修改。事件订阅改为 `api/events` 抽象 |
| `frontend/src/hooks/useMarketStatus.ts` | 修改。同上 |

---

## 三、API 契约

路由表是前后端之间的唯一契约来源，定义在 `internal/webapi/routes.go`。

### 响应格式

成功：

```json
{ "data": <返回值> }
```

失败：

```json
{ "error": "错误信息" }
```

### HTTP 方法约定

| 操作 | 方法 | 路径示例 |
|------|------|---------|
| 读取 | GET | `GET /api/watchlist` |
| 创建 | POST | `POST /api/watchlist` |
| 更新 | PUT | `PUT /api/config` |
| 删除 | DELETE | `DELETE /api/watchlist?symbol=sh600000` |

同一路径可承载多个方法（如 `GET /api/config` 读、`PUT /api/config` 写），
由 `Router.HandleMethod` 按方法分派。

### 关键端点

| 路径 | 说明 |
|------|------|
| `GET /api/health` | 健康检查，**无需鉴权** |
| `GET /api/events` | SSE 事件流，令牌可走 `?token=` |
| `POST /api/market/subscribe` | 订阅股票，body `{"codes":["sh600000"]}` |
| `POST /api/market/orderbook/subscribe` | 订阅盘口，body `{"code":"sh600000"}` |
| `POST /api/market/kline/subscribe` | 订阅 K 线，body `{"code":"...","period":"1d"}` |
| `GET /api/frontend/ready` | 通知后端前端已就绪，启动推送循环 |

### 一致性校验脚本

改动路由后务必运行这两个脚本，否则会出现前后端不匹配导致的 404：

```bash
python check_routes.py      # 校验路由表与方法签名、参数数量匹配
python check_api_paths.py   # 校验前端调用与后端路由的路径/方法一致
```

---

## 四、本地构建与验证

### 前置条件

- Go 1.24+
- Node.js 18+（推荐 22）

### 构建前端

```bash
cd frontend
npm install
npm run build          # 产物在 frontend/dist
```

### 构建 Web 服务二进制

```bash
# Linux（部署到服务器用这个）
CGO_ENABLED=0 GOOS=linux go build -tags web -ldflags "-s -w" -o jcp-server .

# Windows（本地调试用）
go build -tags web -o jcp-server.exe .
```

> **本机 Go 安装**：`winget install GoLang.Go`（国内网络建议从
> [阿里云镜像](https://mirrors.aliyun.com/golang/) 下载 zip 解压，
> winget 直连 go.dev 经常失败）。
> `GOPROXY` 建议设为 `https://goproxy.cn,direct` 加速依赖下载。

> **关键**：`-tags web` 是必须的。不加这个 tag 会编译出桌面版（需要图形界面）。
> 桌面版仍可正常构建：`go build .` 或 `wails build`。

### 本地运行

```bash
export JCP_ADDR=":8080"
export JCP_AUTH_TOKEN="your-secret-token"
./jcp-server
```

浏览器打开 `http://localhost:8080/?token=your-secret-token`

### 验证要点

```bash
# 健康检查（无需鉴权）
curl http://localhost:8080/api/health

# 配置接口（应返回掩码后的 API Key）
curl -H "Authorization: Bearer your-secret-token" http://localhost:8080/api/config

# SSE 事件流
curl -N -H "Authorization: Bearer your-secret-token" http://localhost:8080/api/events
```

---

## 五、服务器部署

### 方式 A：Docker Compose（推荐）

```bash
# 1. 上传代码
scp -r jcp/ root@<服务器IP>:/opt/jcp-src/

# 2. 创建环境变量文件
cd /opt/jcp-src
cat > .env <<'EOF'
JCP_AUTH_TOKEN=CHANGE_ME_TO_RANDOM_TOKEN
JCP_CORS_ORIGINS=https://your-domain.com
VERSION=0.3.5-web
EOF

# 3. 生成强随机令牌
sed -i "s/CHANGE_ME_TO_RANDOM_TOKEN/$(openssl rand -hex 32)/" .env

# 4. 构建并启动
docker compose -f docker-compose.web.yml up -d --build

# 5. 查看日志
docker compose -f docker-compose.web.yml logs -f
```

### 方式 B：systemd + 二进制

```bash
# 1. 上传二进制
scp jcp-server root@<服务器IP>:/tmp/

# 2. 安装
ssh root@<服务器IP> <<'EOF'
useradd -r -s /sbin/nologin jcp
mkdir -p /opt/jcp/data /opt/jcp/static
mv /tmp/jcp-server /opt/jcp/
cp /opt/jcp-src/frontend/dist/* /opt/jcp/static/
cp /opt/jcp-src/deploy/jcp-web.service /etc/systemd/system/
chown -R jcp:jcp /opt/jcp
EOF

# 3. 修改令牌并启动
ssh root@<服务器IP> <<'EOF'
sed -i "s/CHANGE_ME/$(openssl rand -hex 32)/" /etc/systemd/system/jcp-web.service
systemctl daemon-reload
systemctl enable --now jcp-web
systemctl status jcp-web
EOF
```

### Nginx 配置

```bash
scp deploy/nginx.conf root@<服务器IP>:/etc/nginx/conf.d/jcp.conf
ssh root@<服务器IP> "nginx -t && systemctl reload nginx"
```

**SSE 相关配置不能删**，否则实时行情会延迟几十秒才批量到达：

```nginx
location /api/events {
    proxy_pass http://jcp_backend;
    proxy_http_version 1.1;
    proxy_buffering off;        # 必须关闭缓冲
    proxy_read_timeout 3600s;   # 长连接超时
    proxy_set_header Connection "";
}
```

### 防火墙

轻量应用服务器需在**控制台**放行端口：

| 端口 | 用途 | 建议 |
|------|------|------|
| 22 | SSH | 仅限你的 IP |
| 80 | HTTP | 开放 |
| 443 | HTTPS | 开放（配置证书后） |

应用本身监听 8080，**不要**直接对公网开放，只让 Nginx 访问。

---

## 六、安全要点

### 1. 访问令牌

`JCP_AUTH_TOKEN` 必须设置，且必须是强随机值：

```bash
openssl rand -hex 32
```

三种传递方式任选其一：

| 方式 | 写法 | 适用场景 |
|------|------|---------|
| Bearer 头 | `Authorization: Bearer <token>` | 普通 fetch 请求 |
| 查询参数 | `?token=<token>` | EventSource（无法自定义请求头） |
| Cookie | `jcp_token=<token>` | 手动设置后长期使用 |

前端会自动把 URL 里的 `?token=` 存入 localStorage，后续请求自动带上。

### 2. API Key 不下发浏览器

大模型 API Key 保存在服务端配置文件中，浏览器永远拿不到明文：

- `GET /api/config` 返回的 `apiKey` 字段被替换为 `********`
- `POST /api/config` 收到的掩码值会被还原为服务端的明文

这样即使接口被扫描，攻击者也拿不到你的 OpenAI / DeepSeek 密钥。

### 3. 访问日志

Nginx 访问日志会记录请求路径。**注意**：如果用 `?token=` 方式访问，令牌会出现在日志里。
生产环境建议改用 Cookie 或让用户首次访问后由前端存储。

---

## 七、资源占用参考

2 核 2GB / 50GB SSD 的轻量应用服务器实测可用：

| 项目 | 占用 |
|------|------|
| 内存（空闲） | 约 60-90 MB |
| 内存（运行中） | 约 150-250 MB |
| 磁盘（完整镜像） | 约 300-500 MB |
| CPU（空闲） | 约 1% |

注意：
- Go 编译期内存需求远高于运行期，2GB 机器**建议用 Docker 构建或本地交叉编译**后上传，
  不要在服务器上 `go build`（会被 OOM Kill）
- SSE 长连接会为每个客户端保留一个连接，客户端数量多时考虑改用 WebSocket

---

## 八、常见问题

### SSE 事件收不到 / 延迟几十秒
Nginx 没关缓冲。检查 `proxy_buffering off;` 是否生效。

### 页面提示 401
令牌不对。注意令牌区分大小写，且服务器重启后如果用了随机生成的令牌需要同步更新前端。

### WebSocket / MCP 连接失败
如果配置了 MCP 服务器（stdio 类型），它会在服务器本地启动进程。
stdio 类型的 MCP 在容器里需要额外安装对应运行时。

### AI 会议一直转圈
检查大模型 API 配置是否正确，以及服务器能否访问对应 API 域名。
可以在服务器上 `curl` 验证连通性。

### 升级到新版本
```bash
cd /opt/jcp-src && git pull
docker compose -f docker-compose.web.yml up -d --build
```
数据目录 `./data` 是挂载的，升级不会丢配置。
