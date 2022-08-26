package httptoy_test

import (
	"build-HTTP-from-scracth/pkg/httptoy"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
	"strings"
	"testing"
)

// other test functions:
func TestHex(t *testing.T) {
	b := "D\r\n"
	p := strings.IndexByte(b, '\r')
	t.Log(p, b[:p])
	t.Log(strconv.ParseInt(b[:p], 16, 64))
}

// test http server
type testHandler struct {
	F func(req *httptoy.Request, res httptoy.ResponseWriter)
}

func (th *testHandler) ServeHTTP(req *httptoy.Request, res httptoy.ResponseWriter) {
	th.F(req, res)
}

// TestParseHeaderInfo 测试 request 解析 请求行，Header 信息
func TestParseHeaderInfo(t *testing.T) {
	fmt.Println("localhost:8080")
	th := new(testHandler)
	th.F = func(req *httptoy.Request, res httptoy.ResponseWriter) {
		// 用户的头部信息保存到buff中
		buff := &bytes.Buffer{}
		// 测试Request的解析
		fmt.Fprintf(buff, "[query]name=%s\n", req.Query("name"))
		fmt.Fprintf(buff, "[query]token=%s\n", req.Query("token"))
		fmt.Fprintf(buff, "[cookie]foo1=%s\n", req.Cookie("foo1"))
		fmt.Fprintf(buff, "[cookie]foo2=%s\n", req.Cookie("foo2"))
		fmt.Fprintf(buff, "[Header]User-Agent=%s\n", req.Header.Get("User-Agent"))
		fmt.Fprintf(buff, "[Header]Proto=%s\n", req.Proto)
		fmt.Fprintf(buff, "[Header]Method=%s\n", req.Method)
		fmt.Fprintf(buff, "[Addr]Addr=%s\n", req.RemoteAddr)

		fmt.Fprintf(buff, "[Request] Header:%v\n", req.Header)

		//手动发送响应报文
		io.WriteString(res, "HTTP/1.1 200 OK\r\n")
		io.WriteString(res, fmt.Sprintf("Content-Length: %d\r\n", buff.Len()))
		io.WriteString(res, "\r\n")
		io.Copy(res, buff) //将buff缓存数据发送给客户端

	}

	svr := &httptoy.Server{
		Addr:    "127.0.0.1:8080",
		Handler: th,
	}

	panic(svr.ListenAndServe())
}

// TestRequestBody 测试 request body信息 读写流
// 测试1： limitReader
// curl -H "Content-Length: 43" -d "hello, this is chunked message from client!" http://127.0.0.1:8080 -i
// 测试2： chunkReader
// curl -H "Transfer-Encoding: chunked" -H "Content-Length: 13" -d "hello, this is chunked message from client!" http://127.0.0.1:8080 -i
func TestRequestBody(t *testing.T) {
	th := new(testHandler)
	th.F = func(req *httptoy.Request, res httptoy.ResponseWriter) {

		buf, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return
		}

		const prefix = "your message:"
		io.WriteString(res, "HTTP/1.1 200 OK\r\n")
		io.WriteString(res, fmt.Sprintf("Content-Length: %d\r\n", len(buf)+len(prefix)))
		io.WriteString(res, "\r\n")
		io.WriteString(res, prefix)
		res.Write(buf)

		// 查看 header
		// io.WriteString(res, "\r\n")
		// buff := &bytes.Buffer{}
		// fmt.Fprintln(buff, "Header:", req.Header)
		// io.Copy(res, buff)

	}

	svr := &httptoy.Server{
		Addr:    "127.0.0.1:8080",
		Handler: th,
	}
	panic(svr.ListenAndServe())
}
