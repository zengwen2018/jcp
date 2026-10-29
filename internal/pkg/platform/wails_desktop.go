//go:build !web

package platform

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// WailsDesktop 是桌面模式下的实现，直接转发到 Wails 运行时。
type WailsDesktop struct{}

// OpenURL 在默认浏览器中打开链接。
func (WailsDesktop) OpenURL(ctx context.Context, url string) {
	runtime.BrowserOpenURL(ctx, url)
}

// WindowMinimise 最小化窗口。
func (WailsDesktop) WindowMinimise(ctx context.Context) {
	runtime.WindowMinimise(ctx)
}

// WindowToggleMaximise 切换窗口最大化状态。
func (WailsDesktop) WindowToggleMaximise(ctx context.Context) {
	runtime.WindowToggleMaximise(ctx)
}

// Quit 退出应用。
func (WailsDesktop) Quit(ctx context.Context) {
	runtime.Quit(ctx)
}
