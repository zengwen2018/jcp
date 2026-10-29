//go:build web

package platform

import "context"

// WebDesktop 是 Web 模式下的实现。
//
// 打开浏览器在 Web 模式下没有意义（浏览器已经是客户端），
// 窗口控制与退出也由服务端生命周期管理，因此全部为空操作。
type WebDesktop struct{}

// OpenURL 无操作。
func (WebDesktop) OpenURL(context.Context, string) {}

// WindowMinimise 无操作。
func (WebDesktop) WindowMinimise(context.Context) {}

// WindowToggleMaximise 无操作。
func (WebDesktop) WindowToggleMaximise(context.Context) {}

// Quit 无操作。
func (WebDesktop) Quit(context.Context) {}
