package httptoy

type ResponseWriter interface {
	Write(p []byte) (n int, err error)
}

func setupResponse(c *conn) *Response {
	return &Response{c: c}
}

// Response 是针对 http 连接处理的响应报文载体
type Response struct {
	c *conn
}

func (w *Response) Write(p []byte) (int, error) {
	return w.c.bufw.Write(p)
}
