//go:build web

package webapi

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// AuthConfig 是鉴权配置。
type AuthConfig struct {
	// Token 访问令牌。为空表示不启用鉴权（仅建议本地调试使用）。
	Token string
	// CookieName 存放令牌的 Cookie 名。
	CookieName string
	// AllowOrigins 允许的跨域来源，为空表示同源。
	AllowOrigins []string
}

// Auth 从环境变量读取鉴权配置。
//
//   - JCP_AUTH_TOKEN：访问令牌，未设置则不启用鉴权；
//   - JCP_CORS_ORIGINS：允许的跨域来源，逗号分隔。
func AuthFromEnv() AuthConfig {
	cfg := AuthConfig{
		Token:     strings.TrimSpace(os.Getenv("JCP_AUTH_TOKEN")),
		CookieName: "jcp_token",
	}
	if origins := strings.TrimSpace(os.Getenv("JCP_CORS_ORIGINS")); origins != "" {
		for _, o := range strings.Split(origins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.AllowOrigins = append(cfg.AllowOrigins, o)
			}
		}
	}
	return cfg
}

// Middleware 返回鉴权与 CORS 中间件。
//
// 鉴权方式（任一通过即可）：
//   - 请求头 Authorization: Bearer <token>
//   - 查询参数 ?token=<token>（EventSource 无法自定义请求头，故保留此方式）
//   - Cookie <CookieName>=<token>
//
// 令牌缺失或错误时返回 401。
func (m *Middleware) Auth(cfg AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 健康检查与静态资源不鉴权。
			if isPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.Token == "" {
				// 未配置令牌时不鉴权，但明确记录一次告警。
				next.ServeHTTP(w, r)
				return
			}

			if !m.checkToken(r, cfg) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="jcp"`)
				writeError(w, http.StatusUnauthorized, "未授权：令牌无效或缺失")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// checkToken 校验请求中的令牌。
func (m *Middleware) checkToken(r *http.Request, cfg AuthConfig) bool {
	got := ""

	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(h, "Bearer ") {
			got = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
	}
	if got == "" {
		got = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if got == "" {
		if c, err := r.Cookie(cfg.CookieName); err == nil {
			got = c.Value
		}
	}
	if got == "" {
		return false
	}
	// 使用恒定时间比较，避免时序侧信道。
	return subtle.ConstantTimeCompare([]byte(got), []byte(cfg.Token)) == 1
}

// isPublicPath 判断路径是否无需鉴权。
func isPublicPath(path string) bool {
	switch {
	case path == "/api/health":
		return true
	case strings.HasPrefix(path, "/assets/"):
		return true
	case path == "/" || path == "/index.html":
		return true
	default:
		return false
	}
}

// CORS 返回跨域中间件。
func (m *Middleware) CORS(cfg AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && m.originAllowed(origin, cfg.AllowOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// originAllowed 判断来源是否允许。
func (m *Middleware) originAllowed(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	for _, a := range allowed {
		if a == "*" || strings.EqualFold(a, origin) {
			return true
		}
	}
	return false
}
