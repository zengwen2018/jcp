// Package platform 抽象与运行形态相关的桌面专属能力。
//
// 打开外部浏览器、窗口最小化/最大化、退出应用等操作只在桌面模式有意义。
// Web 模式下这些接口被替换为等价实现（打开浏览器仍然可用，其余为无操作）。
package platform

import "context"

// Desktop 提供桌面专属能力。
type Desktop interface {
	// OpenURL 在默认浏览器中打开链接。
	OpenURL(ctx context.Context, url string)
	// WindowMinimise 最小化窗口。
	WindowMinimise(ctx context.Context)
	// WindowToggleMaximise 切换窗口最大化状态。
	WindowToggleMaximise(ctx context.Context)
	// Quit 退出应用。
	Quit(ctx context.Context)
}

// NopDesktop 是 Web 模式下的实现：
// OpenURL 退化为返回不支持，其余操作为空操作。
type NopDesktop struct{}

// OpenURL 在 Web 模式下不执行任何动作。
func (NopDesktop) OpenURL(context.Context, string) {}

// WindowMinimise 无操作。
func (NopDesktop) WindowMinimise(context.Context) {}

// WindowToggleMaximise 无操作。
func (NopDesktop) WindowToggleMaximise(context.Context) {}

// Quit 无操作——Web 模式由 HTTP 服务生命周期管理。
func (NopDesktop) Quit(context.Context) {}
