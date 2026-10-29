//go:build web

// 韭菜盘（JCP）Web 服务模式入口。
//
// 编译方式：
//
//	go build -tags web -o jcp-server .
//
// 与桌面模式的区别见 web_main.go 顶部说明。
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/run-bigpig/jcp/internal/models"
	"github.com/run-bigpig/jcp/internal/pkg/eventbus"
	"github.com/run-bigpig/jcp/internal/webapi"
)

//go:embed all:frontend/dist
var staticAssets embed.FS

func main() {
	if err := runWeb(); err != nil {
		stdlog.Fatalf("启动失败: %v", err)
	}
}

func runWeb() error {
	addr := envOr("JCP_ADDR", ":8080")

	// 事件总线：Web 模式使用 SSE 通道。
	bus := eventbus.NewWebBus()
	defer bus.Close()

	// 创建应用实例，注入 Web 事件总线。
	app := NewAppWithDeps(bus, nil)

	// 启动应用（等价于桌面模式的 startup）。
	app.Startup(context.Background())
	defer app.Shutdown(context.Background())

	router := webapi.NewRouter()
	mw := webapi.NewMiddleware()
	authCfg := webapi.AuthFromEnv()

	// 中间件顺序：CORS -> 鉴权 -> 业务处理。
	router.Use(mw.CORS(authCfg), mw.Auth(authCfg))

	// 健康检查（无需鉴权）。
	router.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprintf(w, `{"status":"ok","version":%q,"time":%q}`,
			Version, time.Now().Format(time.RFC3339))
	})

	// 业务 API。
	//
	// 脱敏钩子：读取配置时隐藏大模型 API Key，
	// 保存配置时把掩码还原为服务端保存的明文，
	// 避免浏览器拿到密钥明文。
	binder := webapi.NewBinder(router, app)
	binder.WithSanitizer(
		func(data any) any {
			if cfg, ok := data.(*models.AppConfig); ok {
				return webapi.MaskConfig(cfg)
			}
			return data
		},
		webapi.RequestSanitizer(func() *models.AppConfig {
			return app.GetConfig()
		}),
	)
	if err := binder.BindAll(webapi.Routes()); err != nil {
		return fmt.Errorf("注册 API 路由失败: %w", err)
	}

	// SSE 事件推送。
	webapi.NewSSEHandler(bus, app).Register(router)

	// 静态资源放在 API 之后注册，避免路径冲突。
	staticHandler, err := buildStaticHandler()
	if err != nil {
		return err
	}
	router.Handle("/", func(w http.ResponseWriter, r *http.Request) {
		staticHandler.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
		// SSE 需要长连接，因此不设置写超时与空闲超时。
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       0,
	}

	// 优雅退出。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		stdlog.Printf("jcp-server %s 已启动，监听 %s", Version, addr)
		if authCfg.Token == "" {
			stdlog.Printf("警告：未设置 JCP_AUTH_TOKEN，所有接口无需鉴权，仅建议本地调试使用")
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		stdlog.Printf("收到信号 %s，开始优雅退出", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

// buildStaticHandler 构建静态资源处理器。
//
// 优先使用 JCP_STATIC_DIR 指定的目录（部署时可用 Nginx 托管）；
// 否则使用编译期内嵌的前端产物。
func buildStaticHandler() (http.Handler, error) {
	if dir := os.Getenv("JCP_STATIC_DIR"); dir != "" {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			stdlog.Printf("使用外部静态资源目录: %s", dir)
			return &spaHandler{root: http.Dir(dir)}, nil
		}
		stdlog.Printf("警告：JCP_STATIC_DIR=%s 不存在，回退到内嵌资源", dir)
	}

	sub, err := fs.Sub(staticAssets, "frontend/dist")
	if err != nil {
		return nil, fmt.Errorf("挂载内嵌前端资源失败: %w", err)
	}
	return &spaHandler{root: http.FS(sub)}, nil
}

// spaHandler 为单页应用提供回退：
// 未匹配到静态文件时返回 index.html，交给前端路由处理。
type spaHandler struct {
	root http.FileSystem
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// API 路径不做 SPA 回退。
	if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
		http.NotFound(w, r)
		return
	}

	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}

	f, err := h.root.Open(path)
	if err == nil {
		f.Close()
		http.FileServer(h.root).ServeHTTP(w, r)
		return
	}

	// 回退到 index.html。
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/index.html"
	http.FileServer(h.root).ServeHTTP(w, r2)
}

// envOr 读取环境变量，为空时返回默认值。
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
