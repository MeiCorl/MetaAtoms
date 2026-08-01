// Package web 提供 MetaAtoms 的 Web 交互层实现。
// 通过 HTTP 协议提供嵌入式静态资源，通过 WebSocket 实现浏览器与 Agent
// 的实时双向通信。云端多租户模式下 HTTP 服务监听固定端口，由部署层决定
// 对外暴露的服务器 IP / 域名。
package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/metaatoms/metaatoms/src/logger"
)

// DefaultAddr 默认监听地址。端口可由全局 setting.json 的 server_port 覆盖。
const DefaultAddr = "0.0.0.0:8969"

// WSPath WebSocket 端点路径。
const WSPath = "/ws"

//go:embed static
var staticFS embed.FS

// Server 承载 HTTP 静态资源服务与 WebSocket 升级入口。
// 业务消息由 ConnectionManager 内部 Router 接收与分发；
// 业务层通过 Server.Router() 注册 handler。
//
// 字段说明：
//   - addr   监听地址；构造时保存期望地址，Start 中 listen 成功后刷新为
//     listener 的实际地址，供上层打印访问提示。
//   - ready  服务"已就绪"信号通道；listen 完成且 addr 已刷新后关闭。
//     调用方可通过 Ready() 阻塞等待端口可用，避免 time.Sleep 这种竞态写法。
type Server struct {
	mu         sync.RWMutex // 保护 addr 在 listen 前后被并发读写
	addr       string
	ready      chan struct{}
	wsMgr      *ConnectionManager
	router     *Router
	httpSrv    *http.Server
	authFunc   func(*http.Request) (string, bool)
	tenantFunc func(string) (*Router, func(*websocket.Conn), func(*websocket.Conn), error)
	httpHooks  []func(*http.ServeMux)
}

// NewServer 构造 Server 实例；addr 为空时使用 DefaultAddr。
func NewServer(addr string) *Server {
	if addr == "" {
		addr = DefaultAddr
	}
	router := NewRouter()
	return &Server{
		addr:   addr,
		ready:  make(chan struct{}),
		wsMgr:  NewConnectionManager(router),
		router: router,
	}
}

// Addr 返回监听地址。
// Start 完成 net.Listen 之前返回构造时传入的期望地址；listen 成功之后返回
// listener 的实际地址。推荐先 <-Ready() 等待就绪，再调用 Addr()。
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.addr
}

// Ready 返回一个只读通道，listen 成功并完成 addr 刷新后关闭。
// 调用方可通过 <-server.Ready() 同步等待 server 进入可服务状态。
func (s *Server) Ready() <-chan struct{} {
	return s.ready
}

// ConnectionManager 暴露连接管理器，供业务层做广播/单播。
func (s *Server) ConnectionManager() *ConnectionManager {
	return s.wsMgr
}

// Router 暴露消息路由，业务层在此注册各消息类型的 handler。
func (s *Server) Router() *Router {
	return s.router
}

// Start 启动 HTTP 服务并阻塞，直到 ctx 取消或服务异常退出。
// 端口被占用时返回明确错误信息。
//
// 上层若需在 server 真正可服务后再执行，应先 <-server.Ready()，避免轮询或
// time.Sleep 带来的竞态。
func (s *Server) SetTenantRouter(auth func(*http.Request) (string, bool), tenant func(string) (*Router, func(*websocket.Conn), func(*websocket.Conn), error)) {
	s.authFunc = auth
	s.tenantFunc = tenant
}

func (s *Server) AddHTTPHandlers(fn func(*http.ServeMux)) {
	if fn == nil {
		return
	}
	s.httpHooks = append(s.httpHooks, fn)
}

func (s *Server) Start(ctx context.Context) error {
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("提取嵌入的 static 目录失败: %w", err)
	}

	mux := http.NewServeMux()
	for _, hook := range s.httpHooks {
		hook(mux)
	}
	staticHandler := http.FileServer(http.FS(staticSub))
	mux.HandleFunc(WSPath, func(w http.ResponseWriter, r *http.Request) {
		if s.authFunc == nil || s.tenantFunc == nil {
			s.wsMgr.HandleWS(w, r)
			return
		}
		userID, ok := s.authFunc(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		router, onOpen, onClose, err := s.tenantFunc(userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.wsMgr.HandleWSWithRouter(w, r, router, onOpen, onClose)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metaatoms-icon.png" || r.URL.Path == "/favicon.ico" {
			w.Header().Set("Cache-Control", "no-store")
			rr := new(http.Request)
			*rr = *r
			u := *r.URL
			u.Path = "/metaatoms-icon.png"
			rr.URL = &u
			staticHandler.ServeHTTP(w, rr)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" || r.URL.Path == "/app.js" || r.URL.Path == "/style.css" {
			w.Header().Set("Cache-Control", "no-store")
		}
		if s.authFunc != nil {
			if _, ok := s.authFunc(r); !ok {
				if r.URL.Path == "/" || r.URL.Path == "/index.html" {
					serveLoginPage(w)
					return
				}
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		}
		staticHandler.ServeHTTP(w, r)
	})

	// 先取构造时传入的"期望地址"用于 listen，listen 成功后再回写真实地址。
	wantAddr := s.Addr()
	listener, err := net.Listen("tcp", wantAddr)
	if err != nil {
		if isAddrInUse(err) {
			return fmt.Errorf("端口 %s 已被占用，请检查后重试", wantAddr)
		}
		return fmt.Errorf("启动 Web 服务失败: %w", err)
	}

	// 把 addr 刷新为操作系统实际分配的地址（含真实端口）。
	realAddr := listener.Addr().String()
	s.mu.Lock()
	s.addr = realAddr
	s.mu.Unlock()

	s.httpSrv = &http.Server{
		Addr:              realAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("Web 服务启动",
		zap.String("addr", realAddr),
		zap.String("static_root", "/"),
		zap.String("ws_path", WSPath),
	)

	// 通知上层 server 已可服务（地址已经刷新到 s.addr）。
	close(s.ready)

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("Web 服务开始关闭（ctx 取消）")
		return s.Shutdown(context.Background())
	case err, ok := <-errCh:
		if !ok {
			return nil
		}
		if err != nil {
			return fmt.Errorf("Web 服务运行出错: %w", err)
		}
		return nil
	}
}

// Shutdown 优雅关闭 HTTP 服务与所有 WebSocket 连接。
func serveLoginPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>MetaAtoms</title>
  <link rel="icon" href="/metaatoms-icon.png?v=20260731-metaatoms-icon" type="image/png">
  <style>
    body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f6f7f9;color:#111827;font-family:Inter,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    .panel{width:min(420px,calc(100vw - 32px));background:white;border:1px solid #e5e7eb;border-radius:8px;padding:28px;box-shadow:0 18px 45px rgba(15,23,42,.08)}
    .brand{display:flex;align-items:center;gap:10px;margin-bottom:4px}.brand-mark{display:block;width:34px;height:34px;border-radius:8px;object-fit:cover}
    h1{margin:0 0 4px;font-size:24px;line-height:1.2}p{margin:0 0 22px;color:#6b7280}
    .tabs{display:flex;gap:8px;margin-bottom:18px}.tabs button{flex:1;border:1px solid #d1d5db;background:#fff;border-radius:6px;padding:10px;cursor:pointer}
    .tabs button.active{background:#111827;color:white;border-color:#111827}
    label{display:block;margin:12px 0 6px;font-size:13px;color:#374151}input{width:100%;box-sizing:border-box;border:1px solid #d1d5db;border-radius:6px;padding:11px;font-size:14px}
    .submit{width:100%;margin-top:18px;border:0;border-radius:6px;background:#2563eb;color:white;padding:12px;font-weight:600;cursor:pointer}
    .error{min-height:20px;margin-top:12px;color:#dc2626;font-size:13px}
  </style>
</head>
<body>
  <main class="panel">
    <div class="brand"><img class="brand-mark" src="/metaatoms-icon.png?v=20260731-metaatoms-icon" alt="" aria-hidden="true"><h1>MetaAtoms</h1></div>
    <p>登录后进入你的专属 Agent 工作区。</p>
    <div class="tabs"><button id="loginTab" class="active">登录</button><button id="registerTab">注册</button></div>
    <form id="form">
      <div id="nicknameWrap" style="display:none"><label>昵称</label><input id="nickname" autocomplete="nickname" /></div>
      <label>邮箱</label><input id="email" type="email" autocomplete="email" required />
      <label>密码</label><input id="password" type="password" autocomplete="current-password" required />
      <button class="submit" id="submit">登录</button>
      <div class="error" id="error"></div>
    </form>
  </main>
  <script>
    let mode = 'login';
    const $ = (id) => document.getElementById(id);
    function setMode(next){ mode=next; $('loginTab').classList.toggle('active',mode==='login'); $('registerTab').classList.toggle('active',mode==='register'); $('nicknameWrap').style.display=mode==='register'?'block':'none'; $('submit').textContent=mode==='register'?'注册':'登录'; $('error').textContent=''; }
    $('loginTab').onclick=()=>setMode('login'); $('registerTab').onclick=()=>setMode('register');
    $('form').onsubmit=async(e)=>{ e.preventDefault(); $('error').textContent=''; const payload={email:$('email').value,password:$('password').value,nickname:$('nickname').value}; const res=await fetch('/api/'+mode,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(payload)}); if(res.ok){ location.reload(); return; } const data=await res.json().catch(()=>({error:'请求失败'})); $('error').textContent=data.error||'请求失败'; };
  </script>
</body>
</html>`))
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.wsMgr != nil {
		s.wsMgr.CloseAll()
	}
	if s.httpSrv != nil {
		if err := s.httpSrv.Shutdown(ctx); err != nil {
			return fmt.Errorf("关闭 Web 服务失败: %w", err)
		}
	}
	logger.Info("Web 服务已关闭")
	return nil
}

// isAddrInUse 跨平台判断 net.Listen 错误是否为"地址已被占用"。
// 不依赖平台特定的 syscall 常量，通过错误字符串兜底：
//   - Linux/macOS："address already in use" / "bind: address already in use"
//   - Windows："Only one usage of each socket address ... is normally permitted"
func isAddrInUse(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "Only one usage of each socket address")
}
