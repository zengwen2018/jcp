//go:build web

package eventbus

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
)

// WebBus 是 Web 模式下的事件总线实现。
//
// 事件先投递到 Hub（按订阅键过滤），再由 SSE 写出端推给浏览器。
// 事件的 "key" 用于订阅过滤——例如股票行情事件以股票代码为 key，
// 只有订阅了该股票的浏览器连接才会收到。
type WebBus struct {
	hub *Hub
}

// NewWebBus 创建 Web 模式事件总线。
func NewWebBus() *WebBus {
	return &WebBus{hub: NewHub()}
}

// Hub 返回底层总线，供 HTTP 层创建 SSE 连接。
func (b *WebBus) Hub() *Hub { return b.hub }

// Emit 发布事件。
//
// 过滤规则按事件名显式声明，不靠负载推断：
//   - 全局事件（大盘指数、快讯）→ 所有连接都收到；
//   - 订阅事件（股票、盘口、K线）→ 仅当全局订阅命中才投递。
func (b *WebBus) Emit(ctx context.Context, name string, data any) {
	if !b.shouldDeliver(name, data) {
		return
	}
	b.hub.Emit(ctx, name, data, nil)
}

// shouldDeliver 判断当前全局订阅是否允许投递该事件。
func (b *WebBus) shouldDeliver(name string, data any) bool {
	if !isFilteredEvent(name) {
		return true
	}
	keys := eventKeys(data)
	if len(keys) == 0 {
		// 提取不到订阅键时不参与过滤，避免事件丢失。
		return true
	}
	for _, k := range keys {
		if b.hub.matchesGlobal(k) {
			return true
		}
	}
	return false
}

// filteredEvents 是需要按订阅过滤的事件名。
//
// 这些事件只与用户关注的个股相关，只推给订阅了对应股票的连接；
// 其余事件（大盘指数、快讯等）属于全局信息，一律广播。
var filteredEvents = map[string]bool{
	"market:stock:update":    true,
	"market:orderbook:update": true,
	"market:kline:update":     true,
}

// isFilteredEvent 判断该事件是否需要按订阅过滤。
func isFilteredEvent(name string) bool {
	return filteredEvents[name]
}

// On 注册进程内处理器。
//
// Web 模式下事件已由 SSE 通道送达浏览器，此处仅支持同进程内
// 其他组件（如 OpenClaw 服务）监听同一事件。
func (b *WebBus) On(name string, handler Handler) func() {
	cl := NewClient(b.hub)
	cl.On(name, handler)
	return func() {
		b.hub.RemoveClient(cl)
	}
}

// Off 桌面模式无需处理。
func (b *WebBus) Off(string) {}

// Close 关闭总线与全部连接。
func (b *WebBus) Close() { b.hub.Close() }

// Listen 在 Web 模式下监听"前端发来的请求"不是通过事件完成的。
//
// 浏览器的订阅请求走 HTTP 接口（如 POST /api/market/subscribe），
// 由 HTTP 层直接调用推送服务的对应方法，因此这里返回空注销函数。
func (b *WebBus) Listen(context.Context, string, SubscribeFunc) func() {
	return func() {}
}

// eventKeys 从事件负载中提取用于订阅过滤的候选键。
//
// 事件负载有多种形态，用反射统一处理：
//   - 结构体 / 指针：取 code 或 symbol 字段；
//   - map：取 code 或 symbol 键；
//   - 切片 / 数组：逐元素提取（股票列表可能包含多个代码）；
//   - 字符串：直接作为键。
//
// 提取不到时返回空切片，表示该事件不参与过滤（所有连接都能收到）。
func eventKeys(data any) []string {
	if data == nil {
		return nil
	}
	v := reflect.ValueOf(data)

	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return eventKeys(v.Elem().Interface())

	case reflect.Slice, reflect.Array:
		keys := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			keys = append(keys, eventKeys(v.Index(i).Interface())...)
		}
		return keys

	case reflect.Map:
		for _, field := range keyFields {
			if got := v.MapIndex(reflect.ValueOf(field)); got.IsValid() {
				if s, ok := got.Interface().(string); ok && s != "" {
					return []string{s}
				}
			}
		}
		return nil

	case reflect.Struct:
		for _, field := range keyFields {
			f := v.FieldByName(field)
			if f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
				return []string{f.String()}
			}
		}
		return nil

	case reflect.String:
		if v.String() == "" {
			return nil
		}
		return []string{v.String()}

	default:
		return nil
	}
}

// keyFields 是用于提取订阅键的候选字段名，按优先级排列。
var keyFields = []string{"Code", "Symbol", "code", "symbol"}

// EncodeMessage 将事件序列化为 SSE 的 data 字段内容。
func EncodeMessage(msg Message) (string, error) {
	payload, err := json.Marshal(msg.Data)
	if err != nil {
		return "", err
	}
	return "event: " + msg.Name + "\ndata: " + string(payload) + "\n\n", nil
}

// EncodeComment 生成 SSE 注释行，用于心跳保活。
func EncodeComment(text string) string {
	return ": " + strings.TrimSpace(text) + "\n\n"
}
