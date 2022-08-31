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
	Header      Header // 首部字段
	contentType string // 解析报文内容的类型
	boundary    string // from-data的边界

	// 报文主体
	URL         *url.URL          // url
	conn        *conn             // 请求连接对象
	RemoteAddr  string            // 客户端地址
	cookies     map[string]string // 客户端cookies
	queryString map[string]string // 请求的url 询问键值对
	Body        io.Reader         // 用于读取报文的io

	// 特殊表单处理
	// 需要 ParseForm 调用之后才能直接调用 PostForm 以及 MultipartForm
	PostForm      map[string]string
	MultipartForm *MultipartForm
	hadParsedForm bool
	parseFromErr  error
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

// readHeader 用来解析 header 首部字段
// E.g. Content-Length: 13
func readHeader(bufr *bufio.Reader) (Header, error) {
	header := make(Header)

	// E.g. Content-Type: text/plain\r\n
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

// 特殊表单的解析处理 parsePostForm, parseMultipartForm
// parseContentType 主要解析 content-type, 根据情况 解析 boudanry
// Content-Type: multipart/form-data; boundary=------974767299852498929531610575
// Content-Type: multipart/form-data; boundary=""------974767299852498929531610575"
// Content-Type: application/x-www-form-urlencoded
func (r *Request) parseContentType() {
	// 1.解析 content-type
	ct := r.Header.Get("Content-Type")
	pivot := strings.IndexByte(ct, ';')
	// 如果没找到 ; 说明content-type 不是 from-data, 直接保存
	if pivot < 0 {
		r.contentType = ct
		return
	}
	// pivot 在最后一位，可能解析失败，直接退出
	if pivot == len(ct)-1 {
		return
	}
	r.contentType = ct[:pivot]

	// 2.解析 boudnary
	sStr := strings.Split(ct[pivot+1:], "=")
	// exit cond: sStr 长度小于2 或者 第一个子串 不是 “boundary”
	if len(sStr) < 2 || strings.TrimSpace(sStr[0]) != "boundary" {
		return
	}
	r.boundary = strings.Trim(sStr[1], `"`)
}

// MultipartReader 用于读取 multipart表单
func (r *Request) MultipartReader() (*MultipartReader, error) {
	if len(r.boundary) < 1 {
		return nil, errors.New("no boundary detected")
	}

	return NewMultipartReader(r.Body, r.boundary), nil
}

// parse-form 1:parsePostForm Post表单的数据类似 queryString ，直接用 parseQuery 解析
// E.g. name=jack&age=22
func (r *Request) parsePostForm() error {
	bb, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return err
	}

	r.PostForm = parseQuery(string(bb))
	return nil
}

// parse-form 2:parseMultipartForm multipart表单 创建文件流对象保存数据，让handler调用
func (r *Request) parseMultipartForm() error {
	mr, err := r.MultipartReader()
	if err != nil {
		return err
	}

	r.MultipartForm, err = mr.ReadForm()
	r.PostForm = r.MultipartForm.Value // postForm也可以通过multipart表单文本数据解析
	return nil
}

// ParseForm ...
func (r *Request) ParseForm() error {
	if r.Method != "POST" && r.Method != "PUT" { // 排除掉没有body的表单解析
		return errors.New("Missing form body.")
	}

	r.hadParsedForm = true

	// 根据 contenType 进行解析表单
	switch r.contentType {
	case "application/x-www-form-urlencoded":
		return r.parsePostForm()
	case "multipart/form-data":
		return r.parseMultipartForm()
	}

	return errors.New("unsupport form type")
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

// setupBody 为连接提供读取流对象，只有put跟post请求能够创建相应的读取流
// chunkReader 以及 LimitReader 根据客户端情况进行创建，以及请求报文的预处理 100 continue
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
			if r.Header.Get("Transfer-Encoding") == "chunked" {
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
	r.queryString = parseQuery(r.URL.RawQuery)

	// 4.解析首部字段
	r.Header, err = readHeader(c.bufr)
	if err != nil {
		return nil, err
	}

	// 5.根据 Content-Type字段进行报文解析
	r.parseContentType()

	// 6.设置 body 读取流
	r.conn.lr.N = (1<<63 - 1) // 设置body读取无需限制
	r.setupBody()

	return &r, nil
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

// PostFormValue 用来做单次查询
func (r *Request) PostFormValue(key string) string {
	if !r.hadParsedForm { // lazy-parse
		r.parseFromErr = r.ParseForm()
	}

	// 如果出现 error 或者 没有 postForm 则返回空字符串
	if r.parseFromErr != nil || r.PostForm == nil {
		return ""
	}

	return r.PostForm[key]
}

// FormFile 用来查询某个名字的文件
func (r *Request) FormFile(filename string) (*FileHeader, error) {
	if !r.hadParsedForm { // lazy-parse
		r.parseFromErr = r.ParseForm()
	}

	if r.parseFromErr != nil || r.MultipartForm == nil {
		return nil, r.parseFromErr
	}

	fh, ok := r.MultipartForm.File[filename]
	if !ok { // 如果文件保存失败
		return nil, errors.New("http: missing multipart file")
	}

	return fh, nil
}
