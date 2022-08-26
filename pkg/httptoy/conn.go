package httptoy

import (
	"bufio"
	"io"
	"log"
	"net"
)

// newConn 创建 http.conn
func newConn(rwc net.Conn, svr *Server) *conn {
	// 由于一个正常报文请求不会超过1mb，因此防止恶意连接，限制每次连接至多读取1mb请求报文
	lr := &io.LimitedReader{R: rwc, N: 1 << 20} // 限制每次conn至多读取 1mb

	return &conn{
		svr:  svr,
		rwc:  rwc,
		lr:   lr,
		bufr: bufio.NewReaderSize(lr, 4<<10),  // 4kb 的读取缓冲
		bufw: bufio.NewWriterSize(rwc, 4<<10), // 4kb 的写入缓冲
	}
}

// handleError 处理 http 连接出现错误
func handleError(err error, c *conn) {
	log.Fatalf("http conn encounter err:%v", err)
}

// conn 是关于 http 创建服务的连接
type conn struct {
	svr *Server
	rwc net.Conn

	lr   *io.LimitedReader // 限制读取
	bufr *bufio.Reader     // 缓冲读取
	bufw *bufio.Writer     //优化连接，能进行缓冲写入
}

// readRequest 读取请求
func (c *conn) readRequest() (*Request, error) {
	// 调用 request.go 的 readRequest 方法解析 conn
	return readRequest(c)
}

// setupResponse 创建响应报文
func (c *conn) setupResponse() *Response {
	return setupResponse(c)
}

// close 关闭 tcp 连接
func (c *conn) close() {
	c.rwc.Close()
}

// serve 模仿 http1.1 支持的 kepp-alive 长连接，该连接能读多个请求
func (c *conn) serve() {
	// 防止 goroutine 宕机，使其恢复，并处理 http 连接错误
	defer func() {
		if err := recover(); err != nil {
			log.Fatalf("panic recover, err :%v\n", err)
		}

		c.close()
	}()

	// for循环不退出，实现 keep-alive 长连接
	for {
		// 读取请求
		req, err := c.readRequest()
		if err != nil {
			break
		}

		// 创建响应
		res := c.setupResponse()

		// 传入请求跟响应，执行后端服务
		c.svr.Handler.ServeHTTP(req, res)

		// 将 tcp 连接 写完以及读完全部剩余数据, 防止资源释放失败
		err = req.finishRequest()
		if err != nil {
			return
		}
	}

}
