package httptoy

import (
	"net"
	"net/http"
	"strings"
)

// Handler ...
type Handler interface {
	ServeHTTP(rw ResponseWriter, req *Request)
}

type HandlerFunc func(rw ResponseWriter, req *Request)

// Server ...
type Server struct {
	Addr    string  // 监听地址
	Handler Handler // 处理http请求的回调函数
}

// ListenAndServe ...
func (s *Server) ListenAndServe() error {
	// 开启tcp，监听 s.Addr 地址
	l, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}

	for {
		// 获取tcp连接的上下文
		rwc, err := l.Accept()
		if err != nil {
			continue
		}

		// 创建连接
		conn := newConn(rwc, s)

		// 开启协程，运行conn的服务
		go conn.serve()
	}
}

// NewServeMux ...
func NewServeMux() *ServeMux {
	return &ServeMux{m: make(map[string]HandlerFunc)}
}

// ServeMux 公共路由
// 实现 Handler 接口，通过哈希表映射包装成 路径匹配的路由器
type ServeMux struct {
	m map[string]HandlerFunc // 用map匹配路径
}

// HandlerFunc ...
func (sm *ServeMux) HandleFunc(pattern string, hf HandlerFunc) {
	if sm.m == nil {
		sm.m = make(map[string]HandlerFunc)
	}

	sm.m[pattern] = hf
}

func (sm *ServeMux) Handle(pattern string, hanlder Handler) {
	if sm.m == nil {
		sm.m = make(map[string]HandlerFunc)
	}

	sm.m[pattern] = hanlder.ServeHTTP
}

func (sm *ServeMux) ServeHTTP(rw ResponseWriter, req *Request) {
	hf, ok := sm.m[req.URL.Path]
	if !ok && len(req.URL.Path) > 1 {
		p := strings.LastIndex(req.URL.Path, `\`)
		hf, ok = sm.m[req.URL.Path[p:]]
	}

	if !ok {
		rw.WriteHeader(http.StatusNotFound)
		return
	}

	hf(rw, req)
}

var defaultServeMux ServeMux
var DefaultServeMux *ServeMux = &defaultServeMux

// Handler 包函数调用
func Handle(pattern string, hanlder Handler) {
	DefaultServeMux.Handle(pattern, hanlder)
}

// HandleFunc 包函数调用
func HandleFunc(pattern string, hanlder HandlerFunc) {
	DefaultServeMux.HandleFunc(pattern, hanlder)
}

// ListenAndServe 包函数调用
func ListenAndServe(addr string, handler Handler) error {
	if handler == nil {
		handler = DefaultServeMux
	}

	svr := &Server{
		Addr:    addr,
		Handler: handler,
	}

	return svr.ListenAndServe()
}
