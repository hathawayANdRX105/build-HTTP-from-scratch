package httptoy

import (
	"bufio"
	"fmt"
)

/* 一般的响应报文
 * HTTP/1.1 200 OK\r\n					#状态行
 * Content-Length: 20\r\n
 * Content-Type: text/html; charset=utf-8\r\n
 * \r\n
 * <h1>hello world</h1>
 */

type ResponseWriter interface {
	Write(p []byte) (int, error)

	// Header 在ServeHTTP设置写入响应报文的首部信息，交由WriteHeader调用
	Header() Header
	// WriteHeader 能够写入设置好的首部信息，以及状态码
	WriteHeader(statusCode int)
}

func setupResponse(c *conn, req *Request) *Response {
	var (
		protoMinor, protoMajor int
		closeAfterReply        bool
	)

	fmt.Sscanf(req.Proto, "HTTP/%d.%d", &protoMinor, &protoMajor)
	if protoMajor < 1 || protoMinor == 1 && protoMajor == 0 || req.Header.Get("Connection") == "close" {
		closeAfterReply = true
	}

	cw := chunkWriter{}

	resp := Response{
		closeAfterReply: closeAfterReply,
		statusCode:      200,
		header:          make(Header),
		cw:              &cw,
		bufw:            bufio.NewWriterSize(&cw, 4<<10), // 4kb 缓存size
		c:               c,
		req:             req,
	}

	cw.resp = &resp

	return &resp
}

// Response 是针对 http 连接处理的响应报文载体
type Response struct {
	wroteHeader bool // 第一次写入resp header flag
	chunking    bool // chunk编码 flag
	handlerDone bool // handler结束 flag

	//是否在本次http请求结束后关闭tcp连接，以下情况需要关闭连接：
	//1、HTTP/1.1之前的版本协议
	//2、请求报文头部设置了Connection: close
	//3、在net.Conn进行Write的过程中发生错误
	closeAfterReply bool

	statusCode int    // 状态码
	header     Header // 响应报文的首部信息

	cw   *chunkWriter  // 块编码 writer
	bufw *bufio.Writer // 缓存 writer

	req *Request
	c   *conn
}

func (w *Response) Write(p []byte) (int, error) {
	n, err := w.bufw.Write(p)
	if err != nil {
		w.closeAfterReply = true
	}

	return n, err
}

// Header ...
func (w *Response) Header() Header {
	return w.header
}

// WriterHeader ...
func (w *Response) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}

	w.statusCode = statusCode
	w.wroteHeader = true

}
