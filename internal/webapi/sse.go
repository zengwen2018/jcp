//go:build web

package webapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/run-bigpig/jcp/internal/pkg/eventbus"
)

// Subscriber 行情订阅处理器。
//
// 由 App 实现，HTTP 层通过它把浏览器的订阅请求
// 转发给市场数据推送服务——Web 模式下没有 Wails 事件通道，
// 只能走这条直连路径。
type Subscriber interface {
	// SetStockSubscriptions 设置股票订阅列表。
	SetStockSubscriptions(codes []string)
	// SetOrderBookSubscription 设置盘口订阅的股票代码。
	SetOrderBookSubscription(code string)
	// SetKLineSubscription 设置 K 线订阅。
	SetKLineSubscription(code, period string)
}

// SSEHandler 处理事件推送的 SSE 长连接。
type SSEHandler struct {
	bus *eventbus.WebBus
	// subscriber 行情订阅处理器，为 nil 时忽略订阅请求。
	subscriber Subscriber
	// heartbeatInterval 心跳间隔，用于穿透 Nginx 等中间层的空闲超时。
	heartbeatInterval time.Duration
}

// NewSSEHandler 创建 SSE 处理器。
func NewSSEHandler(bus *eventbus.WebBus, subscriber Subscriber) *SSEHandler {
	return &SSEHandler{
		bus:               bus,
		subscriber:        subscriber,
		heartbeatInterval: 20 * time.Second,
	}
}

// Register 注册 SSE 路由与订阅接口。
func (s *SSEHandler) Register(r *Router) {
	r.HandleFunc("/api/events", s.handleEvents)
	r.HandleFunc("/api/market/subscribe", s.handleSubscribe)
	r.HandleFunc("/api/market/orderbook/subscribe", s.handleOrderBookSubscribe)
	r.HandleFunc("/api/market/kline/subscribe", s.handleKLineSubscribe)
}

// handleEvents 建立 SSE 连接。
//
// 浏览器通过 EventSource 建立连接，服务器持续推送行情与会议事件。
// 连接关闭时自动从总线注销。
func (s *SSEHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "当前服务器不支持流式响应")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	// 禁止 Nginx 缓冲，否则事件会被攒住批量下发。
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	client := eventbus.NewClient(s.bus.Hub())
	defer s.bus.Hub().RemoveClient(client)

	// 立即推送一条握手事件，让前端确认连接已建立。
	if _, err := w.Write([]byte("data: {\"event\":\"connected\",\"data\":{\"ok\":true}}\n\n")); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(s.heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-client.Done():
			return
		case <-heartbeat.C:
			// 心跳注释行，保持连接活跃。
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case msg, ok := <-client.Events():
			if !ok {
				return
			}
			// 使用统一的信封格式且不写 event: 行，
			// 这样浏览器端可以用 onmessage 接收任意事件名
			//（会议事件名带股票代码后缀，无法预注册监听）。
			payload, err := json.Marshal(msg.Data)
			if err != nil {
				continue
			}
			envelope, err := json.Marshal(map[string]any{
				"event": msg.Name,
				"data":  json.RawMessage(payload),
			})
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: " + string(envelope) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleSubscribe 处理股票列表订阅。
//
// 请求体：{ "codes": ["sh600000", "sz000001"] }
func (s *SSEHandler) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Codes []string `json:"codes"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.bus.Hub().SetGlobalSubscription(req.Codes)
	if s.subscriber != nil {
		s.subscriber.SetStockSubscriptions(req.Codes)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"codes": req.Codes}})
}

// handleOrderBookSubscribe 处理盘口订阅。
func (s *SSEHandler) handleOrderBookSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.bus.Hub().SetGlobalSubscription([]string{req.Code})
	if s.subscriber != nil {
		s.subscriber.SetOrderBookSubscription(req.Code)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"code": req.Code}})
}

// handleKLineSubscribe 处理 K 线订阅。
func (s *SSEHandler) handleKLineSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code   string `json:"code"`
		Period string `json:"period"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.bus.Hub().SetGlobalSubscription([]string{req.Code})
	if s.subscriber != nil {
		s.subscriber.SetKLineSubscription(req.Code, req.Period)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{"code": req.Code, "period": req.Period},
	})
}

// decodeJSON 解析 JSON 请求体。
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}
