package httptoy

import (
	"net"
)

// Handler ...
type Handler interface {
	ServeHTTP(req *Request, res ResponseWriter)
}

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
