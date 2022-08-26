package httptoy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"strconv"
	"strings"
)

/* 下面是http协议的post请求
 * POST /index?name=gu HTTP/1.1\r\n			#请求行
 * Content-Type: text/plain\r\n				#此处至报文主体为首部字段
 * User-Agent: PostmanRuntime/7.28.0\r\n
 * Host: 127.0.0.1:8080\r\n
 * Accept-Encoding: gzip, deflate, br\r\n
 * Connection: keep-alive\r\n
 * Cookie: uuid=12314753; tid=1BDB9E9; HOME=1\r\n
 * Content-Length: 18\r\n
 * \r\n
 * hello,I am client!							#报文主体k
 */

/*
 * Request 针对 http 连接的请求载体的实现
 * Request 针对请求报文的三大部分进行解析， 请求行，首部字段，报文主体
 * 1.请求行 -> Method, RemoteURI, Proto
 * 2.首部字段 除了特殊的Cookies需要特殊解析，大部分不用特殊处理
 * 3.报文主体
 */
type Request struct {
	// 请求行
	Method    string // 请求方法，如 GET, POST, PUT等
	RemoteURI string // 客户端字符串形式 url
	Proto     string // 协议以及版本

	// 首部字段
	Header Header // 首部字段

	// 报文主体
	URL         *url.URL          // url
	conn        *conn             // 请求连接对象
	RemoteAddr  string            // 客户端地址
	cookies     map[string]string // 客户端cookies
	queryString map[string]string // 请求的url 询问键值对
	Body        io.Reader         // 用于读取报文的io
}

// readLine 包装 bufr.ReadLine(), 保证请求行完整，直到 \r\n
func readLine(bufr *bufio.Reader) ([]byte, error) {
	p, isPrefix, err := bufr.ReadLine()
	if err != nil {
		return p, err
	}

	var l []byte
	for isPrefix {
		l, isPrefix, err = bufr.ReadLine()

		if err != nil {
			break
		}

		p = append(p, l...)
	}

	return p, err
}

// 解析 请求报文的 function:
// parseQuery 包装
func parseQuery(RawQuery string) map[string]string {
	// name=jack&age=12
	parts := strings.Split(RawQuery, "&")
	queries := make(map[string]string, len(parts))

	for _, part := range parts {
		p := strings.IndexByte(part, '=')

		// 防止 query 格式错误， 以及 空值传入
		if p < 0 || p == len(part)-1 {
			continue
		}

		// 去除 k-v 对的空格
		queries[strings.TrimSpace(part[:p])] = strings.TrimSpace(part[p+1:])
	}

	return queries
}

func (r *Request) parseQuery() {
	r.queryString = parseQuery(r.URL.RawQuery)
}

// readHeader 用来解析 header 首部字段
func readHeader(bufr *bufio.Reader) (Header, error) {
	header := make(Header)

	// ex: Content-Type: text/plain\r\n
	for {
		// 利用 readLine 读完整的一行 \r\n
		line, err := readLine(bufr)
		if err != nil {
			return nil, err
		}

		// 如果读取行为空， 则说明以及读取到 \r\n\r\n
		if len(line) == 0 {
			break
		}

		p := bytes.IndexByte(line, ':')
		// 如果没找打':', 首部字段读取失败
		if p < 0 {
			return nil, errors.New("Unsupport protocol")
		}
		// 如果 ':' 为最后一位, 则为空值, 跳过
		if p == len(line)-1 {
			continue
		}

		// 添加首部键值对
		header.Add(string(line[:p]), strings.TrimSpace(string(line[p+1:])))
	}

	return header, nil
}

// parseCookies 用于解析header中的Cookie
// 格式 -> Cookie: uuid=12314753; tid=1BDB9E9; HOME=1\r\n
func (r *Request) parseCookies() {
	if r.cookies != nil {
		// ? 多一次判断
		return
	}

	r.cookies = make(map[string]string)

	rawCookies, ok := r.Header["Cookie"]
	if !ok {
		return
	}

	// 解析 cookies
	// line : "uuid=222333 ; tid=1BDB919 ; HOME=1"
	for _, line := range rawCookies {
		kvParis := strings.Split(strings.TrimSpace(line), ";")

		if len(kvParis) < 2 && kvParis[0] == "" {
			continue
		}

		for i := 0; i < len(kvParis); i++ {
			// uuid=222333
			p := strings.IndexByte(kvParis[i], '=')

			if p < 0 { // 没找到就跳过
				continue
			}

			r.cookies[strings.TrimSpace(kvParis[i][:p])] = strings.TrimSpace(kvParis[i][p+1:])
		}
	}

}

// eofReader 用来读取报文主体的
type eofReader struct{}

// 实现了io.Reader接口
func (er *eofReader) Read([]byte) (n int, err error) {
	return 0, io.EOF
}

type expectContinueReader struct {
	wroteContinue bool          // 用作记录发送 “100 continue” 的flag
	r             io.Reader     // 保存 r.Body
	w             *bufio.Writer // 缓冲输出流
}

func (ecr *expectContinueReader) Read(p []byte) (int, error) {
	// 如果没有发送过 100 continue 则先进行发送
	if !ecr.wroteContinue {
		ecr.w.WriteString("HTTP/1.1 100 Continue\r\n\r\n")
		ecr.w.Flush()

		// 设置flag
		ecr.wroteContinue = true
	}

	// 继承 setupBody 的 chunked 编码读取流 或 限制流，作为中介
	return ecr.r.Read(p)
}

// chunked 检查 conn 连接是否为chunk 编码读取
func (r *Request) chunked() bool {
	return r.Header.Get("Transfer-Encoding") == "chunked"
}

// fixExpectContinueReader 包装 r.Body，包装成发送 100 continue的特殊流
func (r *Request) fixExpectContinueReader() {
	if r.Header.Get("Expect") != "100-continue" {
		return
	}

	// 利用 expectContinueReader 对象提前发送 100 continue
	r.Body = &expectContinueReader{
		r: r.Body,
		w: r.conn.bufw,
	}
}

func (r *Request) setupBody() {
	// 按照http协议，除了POST和PUT以外的方法不允许设置报文主体
	switch r.Method {
	case "POST":
		fallthrough
	case "PUT":
		if cl := r.Header.Get("Content-Length"); cl != "" {
			contentLength, err := strconv.ParseInt(cl, 10, 64)
			if err != nil {
				break
			}

			// 根据客户端查询方式，进行包装读取流，提前进行发送 100 continue
			defer r.fixExpectContinueReader()

			// chunk 编码读取
			if r.chunked() {
				r.Body = &chunkReader{bufr: r.conn.bufr}
				return
			}

			// 普通限制
			// 限制Body 读取至多长度contentLength的数据
			r.Body = io.LimitReader(r.conn.bufr, contentLength)
			return
		}
	}

	// 其余情况，直接创建eof终止对象
	r.Body = new(eofReader)
}

// readRequest 创建并返回request，解析基本的 request 的信息
func readRequest(c *conn) (*Request, error) {
	r := Request{conn: c, RemoteAddr: c.rwc.RemoteAddr().String()}

	// 1.读取请求行
	line, err := readLine(c.bufr)
	if err != nil {
		return nil, err
	}

	// 解析请求行
	_, err = fmt.Sscanf(string(line), "%s%s%s", &r.Method, &r.RemoteURI, &r.Proto)
	if err != nil {
		return nil, err
	}

	// 2.URL转变形式
	r.URL, err = url.ParseRequestURI(r.RemoteURI)
	if err != nil {
		return nil, err
	}

	// 3.解析queryString
	r.parseQuery()

	// 4.解析首部字段
	r.Header, err = readHeader(c.bufr)
	if err != nil {
		return nil, err
	}

	// 5.读取body
	r.conn.lr.N = (1<<63 - 1) // 设置body读取无需限制
	r.setupBody()

	return &r, nil
}

// finishRequest 处理 Request 两个缓冲流的资源过剩, 写完与读完, 将由 server 调用
func (r *Request) finishRequest() error {
	// 将缓冲输出流中的剩余数据发送
	err := r.conn.bufw.Flush()

	if err == nil {
		// 同样的，r.Body 可能存在未读完的资源导致 conn 不能关闭
		// 因此使用 io.Copy 将 r.Body 全部读取出来， ioutil.Discard 只会读取不做其他事
		// 待将剩余的资源读完，则可以释放 r.Body
		_, err = io.Copy(ioutil.Discard, r.Body)
	}

	return err
}

// 查询 Request function:
// Query 用来查询 请求的 queryString
func (r *Request) Query(key string) string {
	return r.queryString[key]
}

// Cookie 用于查询 请求的 Cookies
func (r *Request) Cookie(key string) string {
	// lazyload
	if r.cookies == nil {
		r.parseCookies()
	}

	return r.cookies[key]
}
