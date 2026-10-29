//go:build !web

package eventbus

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// WailsBus 扩展桌面模式能力：支持监听前端发来的订阅请求。
type WailsBus struct {
	ctx context.Context
}

// NewWailsBus 创建桌面模式事件总线。
func NewWailsBus(ctx context.Context) *WailsBus {
	return &WailsBus{ctx: ctx}
}

// Emit 向 Wails 前端发送事件。
func (b *WailsBus) Emit(ctx context.Context, name string, data any) {
	if ctx == nil {
		ctx = b.ctx
	}
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, name, data)
}

// On 在桌面模式下无需注册（前端自行 EventsOn 监听），返回空注销函数。
func (b *WailsBus) On(string, Handler) func() { return func() {} }

// Off 桌面模式无需处理。
func (b *WailsBus) Off(string) {}

// Close 桌面模式无需处理。
func (b *WailsBus) Close() {}

// Listen 监听前端发出的事件（如订阅请求）。
// 返回注销函数。
func (b *WailsBus) Listen(ctx context.Context, name string, fn SubscribeFunc) func() {
	if ctx == nil {
		ctx = b.ctx
	}
	if ctx == nil {
		return func() {}
	}
	runtime.EventsOn(ctx, name, fn)
	return func() {
		runtime.EventsOff(ctx, name)
	}
}
