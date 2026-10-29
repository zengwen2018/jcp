// Package eventbus 提供跨运行时的消息事件抽象。
//
// 桌面版通过 Wails 的 runtime.EventsEmit 向前端 WebView 推送事件；
// Web 版通过 SSE 通道向浏览器推送事件。两者对上层暴露同一套接口，
// 使业务代码（app.go、market_pusher.go）无需感知运行形态。
package eventbus

import (
	"context"
	"sync"
)

// Handler 是事件处理函数。data 的具体类型由 Emit 时的入参决定。
type Handler func(data any)

// SubscribeFunc 是订阅事件的回调，参数为事件负载。
//
// 桌面模式下用于接收前端发来的订阅请求；
// Web 模式下浏览器的订阅走 HTTP 接口，此类型仅用于保持接口一致。
type SubscribeFunc func(data ...any)

// EventBus 是事件总线的抽象。
type EventBus interface {
	// Emit 发布事件，name 为事件名，data 为负载。
	Emit(ctx context.Context, name string, data any)
	// On 注册事件处理器，返回可用于注销的函数。
	On(name string, handler Handler) (off func())
	// Listen 监听"前端发来"的事件，返回注销函数。
	Listen(ctx context.Context, name string, fn SubscribeFunc) (off func())
	// Off 注销某事件的所有处理器。
	Off(name string)
	// Close 释放总线持有的资源。
	Close()
}

// Subscription 是订阅关系句柄，用于客户端声明订阅范围（如订阅某只股票）。
type Subscription interface {
	// Subscribe 提交一组订阅键（如股票代码）。
	Subscribe(keys []string)
	// Unsubscribe 取消订阅。
	Unsubscribe()
}

// NopBus 是空实现，用于不需要事件推送的场景（测试、单命令模式）。
type NopBus struct{}

func (NopBus) Emit(context.Context, string, any)             {}
func (NopBus) On(string, Handler) func()                      { return func() {} }
func (NopBus) Listen(context.Context, string, SubscribeFunc) func() {
	return func() {}
}
func (NopBus) Off(string) {}
func (NopBus) Close()     {}

// Hub 是通用的进程内事件总线，支持按订阅键过滤。
//
// 桌面模式下 Wails 自己维护订阅关系，此处仅用于 Web 模式：
// 每个 SSE 连接对应一个 Client，Client 通过 Subscribe 声明关心的事件键，
// Hub 在派发时按 key 过滤，避免把所有行情事件广播给每个浏览器。
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
	closed  bool

	// globalKeys 是全局订阅键。Web 模式为单用户场景，
	// 浏览器通过 HTTP 订阅接口设置，所有连接共享该集合，
	// 避免每个连接各自维护一套订阅状态。
	globalKeys map[string]struct{}
}

// NewHub 创建事件总线。
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		globalKeys: make(map[string]struct{}),
	}
}

// SetGlobalSubscription 设置全局订阅键集合。
// 传空集合表示订阅全部事件。
func (h *Hub) SetGlobalSubscription(keys []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.globalKeys = make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k != "" {
			h.globalKeys[k] = struct{}{}
		}
	}
}

// GlobalSubscription 返回当前全局订阅键。
func (h *Hub) GlobalSubscription() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]string, 0, len(h.globalKeys))
	for k := range h.globalKeys {
		out = append(out, k)
	}
	return out
}

// matchesGlobal 判断事件键是否命中全局订阅。
// 全局订阅为空时表示不过滤，全部放行。
func (h *Hub) matchesGlobal(key string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.globalKeys) == 0 {
		return true
	}
	_, ok := h.globalKeys[key]
	return ok
}

// Emit 将事件派发给所有匹配的客户端。
//
// filter 用于判断某个客户端是否需要接收该事件；返回 false 表示跳过该客户端。
// data 会被原样传递，调用方需保证可安全并发读取。
func (h *Hub) Emit(ctx context.Context, name string, data any, filter func(*Client) bool) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return
	}
	targets := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if filter == nil || filter(c) {
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range targets {
		c.deliver(name, data)
	}
}

// EmitGlobal 按全局订阅派发事件。
//
// key 为空表示该事件不参与过滤，全部连接都能收到；
// key 非空时，仅当全局订阅命中该 key 才投递。
func (h *Hub) EmitGlobal(ctx context.Context, name string, data any, key string) {
	if key != "" && !h.matchesGlobal(key) {
		return
	}
	h.Emit(ctx, name, data, nil)
}

// AddClient 注册客户端并返回注销函数。
func (h *Hub) AddClient(c *Client) func() {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	return func() { h.RemoveClient(c) }
}

// RemoveClient 移除客户端并关闭其通道。
func (h *Hub) RemoveClient(c *Client) {
	h.mu.Lock()
	_, ok := h.clients[c]
	delete(h.clients, c)
	h.mu.Unlock()
	if ok {
		c.close()
	}
}

// Close 关闭总线及全部客户端。
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.clients = make(map[*Client]struct{})
	h.mu.Unlock()

	for _, c := range clients {
		c.close()
	}
}

// Client 表示一条事件订阅连接（Web 模式下对应一个 SSE 连接）。
type Client struct {
	hub *Hub

	mu       sync.RWMutex
	keys     map[string]struct{}
	handlers map[string][]Handler

	events chan Message
	closed chan struct{}
	once   sync.Once
}

// Message 是一条待推送的消息。
type Message struct {
	Name string `json:"event"`
	Data any    `json:"data"`
}

// NewClient 创建客户端并注册到总线。
func NewClient(hub *Hub) *Client {
	c := &Client{
		hub:      hub,
		keys:     make(map[string]struct{}),
		handlers: make(map[string][]Handler),
		events:   make(chan Message, 256),
		closed:   make(chan struct{}),
	}
	hub.AddClient(c)
	return c
}

// Events 返回消息通道，供 SSE 写出端读取。
func (c *Client) Events() <-chan Message { return c.events }

// Done 在客户端关闭时关闭。
func (c *Client) Done() <-chan struct{} { return c.closed }

// Subscribe 声明客户端关心的订阅键（如股票代码）。传空表示不过滤。
func (c *Client) Subscribe(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k != "" {
			c.keys[k] = struct{}{}
		}
	}
}

// Unsubscribe 清空订阅过滤。
func (c *Client) Unsubscribe() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = make(map[string]struct{})
}

// HasKey 判断客户端是否关心指定键。
// 未设置任何键时视为订阅全部。
func (c *Client) HasKey(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.keys) == 0 {
		return true
	}
	_, ok := c.keys[key]
	return ok
}

// On 为客户端注册本地处理器，用于进程内直接消费事件。
func (c *Client) On(name string, handler Handler) func() {
	c.mu.Lock()
	c.handlers[name] = append(c.handlers[name], handler)
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		list := c.handlers[name]
		for i, h := range list {
			if h == nil {
				continue
			}
			list = append(list[:i], list[i+1:]...)
			break
		}
		c.handlers[name] = list
	}
}

// deliver 将消息投递给客户端，通道满时丢弃最旧消息，保证推送不阻塞业务线程。
func (c *Client) deliver(name string, data any) {
	c.mu.RLock()
	handlers := append([]Handler(nil), c.handlers[name]...)
	c.mu.RUnlock()

	for _, h := range handlers {
		safeCall(h, data)
	}

	msg := Message{Name: name, Data: data}
	select {
	case c.events <- msg:
	default:
		// 通道满：丢弃一条最旧消息后重试，保证最新数据优先。
		select {
		case <-c.events:
		default:
		}
		select {
		case c.events <- msg:
		default:
		}
	}
}

// close 关闭客户端通道。
func (c *Client) close() {
	c.once.Do(func() {
		close(c.closed)
		close(c.events)
	})
}

// safeCall 捕获处理器 panic，避免单个订阅者拖垮推送循环。
func safeCall(h Handler, data any) {
	defer func() {
		_ = recover()
	}()
	h(data)
}
