package main

import (
	"build-HTTP-from-scracth/pkg/httptoy"
	"bytes"
	"fmt"
	"io"
)

type myHandler struct{}

func (*myHandler) ServeHTTP(req *httptoy.Request, res httptoy.ResponseWriter) {

	// 用户的头部信息保存到buff中
	buff := &bytes.Buffer{}
	// 测试Request的解析
	// fmt.Fprintf(buff, "[query]name=%s\n", req.Query("name"))
	// fmt.Fprintf(buff, "[query]token=%s\n", req.Query("token"))
	// fmt.Fprintf(buff, "[cookie]foo1=%s\n", req.Cookie("foo1"))
	// fmt.Fprintf(buff, "[cookie]foo2=%s\n", req.Cookie("foo2"))
	// fmt.Fprintf(buff, "[Header]User-Agent=%s\n", req.Header.Get("User-Agent"))
	// fmt.Fprintf(buff, "[Header]Proto=%s\n", req.Proto)
	// fmt.Fprintf(buff, "[Header]Method=%s\n", req.Method)
	// fmt.Fprintf(buff, "[Addr]Addr=%s\n", req.RemoteAddr)

	fmt.Fprintf(buff, "[Request] Header:%v\n", req.Header)

	//手动发送响应报文
	io.WriteString(res, "HTTP/1.1 200 OK\r\n")
	io.WriteString(res, fmt.Sprintf("Content-Length: %d\r\n", buff.Len()))
	io.WriteString(res, "\r\n")
	io.Copy(res, buff) //将buff缓存数据发送给客户端

}

func main() {
	fmt.Println("localhost:8080")

	svr := &httptoy.Server{
		Addr:    "127.0.0.1:8080",
		Handler: new(myHandler),
	}
	panic(svr.ListenAndServe())
}
