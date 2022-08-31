package httptoy

import (
	"bufio"
	"io"
	"io/ioutil"
	"log"
	"net"
)

// handleError 处理 http 连接出现错误
func handleError(err error, c *conn) {
	log.Fatalf("http conn encounter err:%v", err)
}

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
func (c *conn) setupResponse(req *Request) *Response {
	return setupResponse(c, req)
}

// close 关闭 tcp 连接
func (c *conn) close() {
	c.rwc.Close()
}

// finishRequest 处理 Request 缓冲Reader 跟 Writer的资源过剩, 写完与读完, 将在 serve 最后调用
func (c *conn) finishRequest(req *Request, resp *Response) (err error) {
	// 将可能保存的临时文件删除
	if req.MultipartForm != nil {
		req.MultipartForm.RemoveAll()
	}

	resp.handlerDone = true // 记录 handler 结束flag
	// 将缓冲输出流中的剩余数据发送, resp的输出根据情况设置 chunk写入还是一次性
	if err = resp.bufw.Flush(); err != nil {
		return err
	}

	if resp.chunking {
		_, err = c.bufw.WriteString("0\r\n\r\n")
		if err != nil {
			return err
		}
	}

	if err = c.bufw.Flush(); err != nil {
		return err
	}

	// 消费完Body剩余的数据
	// 同样的，r.Body 可能存在未读完的资源导致 conn 不能关闭
	// 因此使用 io.Copy 将 r.Body 全部读取出来， ioutil.Discard 只会读取不做其他事
	// 待将剩余的资源读完，则可以释放 r.Body
	_, err = io.Copy(ioutil.Discard, req.Body)

	return err
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
			handleError(err, c)
			break
		}

		// 创建响应
		resp := c.setupResponse(req)

		// 传入请求跟响应，执行后端服务
		c.svr.Handler.ServeHTTP(resp, req)

		// 将 tcp 连接 写完以及读完全部剩余数据, 防止资源释放失败
		err = c.finishRequest(req, resp)
		// 如果出现错误，或者 响应回复完毕则退出
		if err != nil || resp.closeAfterReply {
			break
		}
	}
}
