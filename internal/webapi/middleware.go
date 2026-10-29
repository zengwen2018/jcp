//go:build web

package webapi

// Middleware 承载 Web 模式的中间件实现。
//
// 单独成结构体而非散落的函数，是为了便于后续扩展
// （如限流、请求日志、指标采集）。
type Middleware struct{}

// NewMiddleware 创建中间件集合。
func NewMiddleware() *Middleware {
	return &Middleware{}
}
