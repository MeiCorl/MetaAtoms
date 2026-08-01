package web

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/metaatoms/metaatoms/src/logger"
)

// HandlerFunc 业务消息处理函数。
// conn：当前 WebSocket 连接；msg：解码后的消息信封。
// handler 自行负责 payload 解码、错误回传与业务逻辑。
// 返回错误时 Router 仅记录日志，handler 应已通过 stream_error 通知客户端。
type HandlerFunc func(conn *websocket.Conn, msg Message) error

// Router 消息路由分发器。
// 内部使用 map + RWMutex 保护 handlers 的并发访问：
// 写操作用 Lock（Register），读操作用 RLock（Route/Handler/Types）。
type Router struct {
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
	resolve  func(conn *websocket.Conn, msg Message) (*Router, error)
}

// NewRouter 构造空路由。
func NewRouter() *Router {
	return &Router{
		handlers: make(map[string]HandlerFunc),
	}
}

// NewDynamicRouter constructs a router that resolves the concrete router for
// each incoming message. Cloud tenant connections use this to keep an existing
// WebSocket bound to the latest rebuilt tenant runtime.
func NewDynamicRouter(resolve func(conn *websocket.Conn, msg Message) (*Router, error)) *Router {
	r := NewRouter()
	r.resolve = resolve
	return r
}

// Register 注册一个消息类型对应的 handler。同类型重复注册将覆盖前一个。
func (r *Router) Register(typ string, h HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[typ] = h
}

// Handler 返回指定类型的 handler（用于测试与调试）。
func (r *Router) Handler(typ string) (HandlerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[typ]
	return h, ok
}

// Types 返回所有已注册的消息类型（用于测试与调试）。
func (r *Router) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for t := range r.handlers {
		out = append(out, t)
	}
	return out
}

// Route 根据消息类型分发到对应 handler。
//   - 未知类型：记录警告，向客户端发送 stream_error(code=unknown_message_type)，返回 error。
//   - handler 返回错误：仅记录日志，不重复发送 stream_error（由 handler 自行决定）。
func (r *Router) Route(conn *websocket.Conn, msg Message) error {
	if r.resolve != nil {
		target, err := r.resolve(conn, msg)
		if err != nil {
			logger.Warn("动态路由解析失败",
				zap.String("type", msg.Type),
				zap.String("remote", conn.RemoteAddr().String()),
				zap.Error(err),
			)
			r.sendDynamicRouteError(conn, err)
			return err
		}
		if target == nil {
			err := fmt.Errorf("动态路由未返回目标 router")
			r.sendDynamicRouteError(conn, err)
			return err
		}
		if target == r {
			err := fmt.Errorf("动态路由返回自身")
			r.sendDynamicRouteError(conn, err)
			return err
		}
		return target.Route(conn, msg)
	}
	r.mu.RLock()
	h, ok := r.handlers[msg.Type]
	r.mu.RUnlock()
	if !ok {
		logger.Warn("收到未知类型消息",
			zap.String("type", msg.Type),
			zap.String("remote", conn.RemoteAddr().String()),
		)
		r.sendUnknownTypeError(conn, msg.Type)
		return fmt.Errorf("未知消息类型: %s", msg.Type)
	}
	if err := h(conn, msg); err != nil {
		logger.Warn("消息处理失败",
			zap.String("type", msg.Type),
			zap.String("remote", conn.RemoteAddr().String()),
			zap.Error(err),
		)
		return err
	}
	return nil
}

func (r *Router) sendDynamicRouteError(conn *websocket.Conn, err error) {
	data, encErr := EncodePayload(MsgTypeStreamError, StreamErrorPayload{
		Code:    "runtime_reload_failed",
		Message: err.Error(),
	})
	if encErr != nil {
		logger.Warn("编码动态路由错误消息失败", zap.Error(encErr))
		return
	}
	if writeErr := conn.WriteMessage(websocket.TextMessage, data); writeErr != nil {
		logger.Warn("发送动态路由错误消息失败", zap.Error(writeErr))
	}
}

// HandleLoop 启动 WebSocket 读循环并把消息分发给 router。该方法会阻塞直到连接断开。
//   - 客户端主动关闭（CloseNormal/GoingAway/Abnormal）→ 静默返回。
//   - 解码失败 → 向客户端发送 stream_error(code=invalid_message) 后继续读循环。
//   - 路由/handler 错误 → 仅记录日志，不中断读循环。
//   - 网络异常 → 记录日志后返回。
//
// 调用方负责在 HandleLoop 退出后清理连接。
func (r *Router) HandleLoop(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure) {
				logger.Warn("WebSocket 读消息失败", zap.Error(err))
			}
			return
		}
		msg, err := Decode(data)
		if err != nil {
			logger.Warn("WebSocket 消息解码失败",
				zap.Error(err),
				zap.ByteString("raw", data),
			)
			r.sendInvalidMessageError(conn, err.Error())
			continue
		}
		_ = r.Route(conn, msg)
	}
}

// sendUnknownTypeError 向客户端发送 stream_error 通知未知类型。
func (r *Router) sendUnknownTypeError(conn *websocket.Conn, typ string) {
	data, err := EncodePayload(MsgTypeStreamError, StreamErrorPayload{
		Code:    "unknown_message_type",
		Message: fmt.Sprintf("未注册的消息类型: %s", typ),
	})
	if err != nil {
		logger.Warn("编码未知类型错误消息失败", zap.Error(err))
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Warn("发送未知类型错误消息失败", zap.Error(err))
	}
}

// sendInvalidMessageError 向客户端发送 stream_error 通知消息格式错误。
func (r *Router) sendInvalidMessageError(conn *websocket.Conn, detail string) {
	data, err := EncodePayload(MsgTypeStreamError, StreamErrorPayload{
		Code:    "invalid_message",
		Message: detail,
	})
	if err != nil {
		logger.Warn("编码格式错误消息失败", zap.Error(err))
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Warn("发送格式错误消息失败", zap.Error(err))
	}
}
