package httptoy_test

import (
	"build-HTTP-from-scracth/pkg/httptoy"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
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

	line := `!d1`
	var chunkSize int

	for i := 0; i < len(line); i++ {

		b1 := int((line[i] | 0x20))
		if b1-'0' > -1 && b1-'0' < 10 {
			chunkSize = chunkSize*16 + b1 - '0'
			continue
		} else if b1-'a' > -1 && b1-'a' < 6 {
			chunkSize = chunkSize*16 + b1 - 'a' + 10
			continue
		}

		t.Log("false -> ", string(line[i]))
	}

	t.Log(chunkSize)

	fmt.Println(('!'|0x20)-'0', ('!' | 0x20))
	fmt.Printf("%b %b \n %b %b \n", ' ', ' '|0x20, '!', '!'|0x20)

}

// test http server
type testHandler struct {
	F func(rw httptoy.ResponseWriter, req *httptoy.Request)
}

func (th *testHandler) ServeHTTP(rw httptoy.ResponseWriter, req *httptoy.Request) {
	th.F(rw, req)
}

// TestParseHeaderInfo 测试 request 解析 请求行，Header 信息
func TestParseHeaderInfo(t *testing.T) {
	fmt.Println("localhost:8080")
	th := new(testHandler)
	th.F = func(rw httptoy.ResponseWriter, req *httptoy.Request) {
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
		io.WriteString(rw, "HTTP/1.1 200 OK\r\n")
		io.WriteString(rw, fmt.Sprintf("Content-Length: %d\r\n", buff.Len()))
		io.WriteString(rw, "\r\n")
		io.Copy(rw, buff) //将buff缓存数据发送给客户端

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
	th.F = func(rw httptoy.ResponseWriter, req *httptoy.Request) {

		buf, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return
		}

		io.WriteString(rw, "HTTP/1.1 200 OK\r\n")
		io.WriteString(rw, fmt.Sprintf("Content-Length: %d\r\n", len(buf)))
		io.WriteString(rw, "\r\n")
		rw.Write(buf)

		// 查看 header
		io.WriteString(rw, "\r\n")
		buff := &bytes.Buffer{}
		_, err = fmt.Fprint(buff, "\nHeader:", req.Header)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println(req.Header)

		_, err = io.Copy(rw, buff)
		if err != nil {
			fmt.Println(err)
		}
	}

	svr := &httptoy.Server{
		Addr:    "127.0.0.1:8080",
		Handler: th,
	}
	panic(svr.ListenAndServe())
}

// TestMultipartReader 用于测试 MultipartReader
// 任意目录下传输 1.txt, 2.txt.
// cmd：curl -F "username=gu" -F "password=123" -F "file1=@1.txt" -F "file2=@2.txt" http://127.0.0.1:8080
func TestMultipartReader(t *testing.T) {

	th := new(testHandler)
	th.F = func(rw httptoy.ResponseWriter, req *httptoy.Request) {
		mr, err := req.MultipartReader()
		if err != nil {
			log.Println(err)
			return
		}

		var part *httptoy.Part
	label:
		for {
			part, err = mr.NextPart()
			if err != nil {
				break
			}
			// 判断是文本part还是文件part
			switch part.FileName() {
			case "": //文本
				fmt.Printf("FormName=%s, FormData:\n", part.FormName())
				// 输出到终端
				if _, err = io.Copy(os.Stdout, part); err != nil {
					break label
				}
				fmt.Println()
			default: //文件
				// 打印文件信息
				fmt.Printf("FormName=%s, FileName=%s\n", part.FormName(), part.FileName())

				// 创建文件
				var file *os.File
				if file, err = os.Create(part.FileName()); err != nil {
					break label
				}
				if _, err = io.Copy(file, part); err != nil {
					file.Close()
					break label
				}
				file.Close()
			}
		}
		if err != io.EOF {
			fmt.Println(err)
		}

		// 发送响应报文
		io.WriteString(rw, "HTTP/1.1 200 OK\r\n")
		io.WriteString(rw, fmt.Sprintf("Content-Length: %d\r\n", 0))
		io.WriteString(rw, "\r\n")
	}

	svr := &httptoy.Server{
		Addr:    "127.0.0.1:8080",
		Handler: th,
	}
	panic(svr.ListenAndServe())
}

// 测试FormFile。 将文件文本输出到终端
// cmd : curl -F "file1=@1.txt" http://127.0.0.1:8080/test1
func handleTest1(rw httptoy.ResponseWriter, req *httptoy.Request) (err error) {
	fh, err := req.FormFile("file1")
	if err != nil {
		return
	}
	rc, err := fh.Open()
	if err != nil {
		return
	}
	defer rc.Close()
	buf, err := ioutil.ReadAll(rc)
	if err != nil {
		return
	}
	fmt.Printf("%s\n", buf)
	return
}

// 测试Save。 将文件保存到硬盘
// cmd :  curl -F "file1=@1.txt" -F "file2=@2.txt" http://127.0.0.1/test2
func handleTest2(rw httptoy.ResponseWriter, req *httptoy.Request) (err error) {
	if err = req.ParseForm(); err != nil {
		return
	}

	mr := req.MultipartForm
	for _, fh := range mr.File {
		err = fh.Save(fh.Filename)
		if err == nil {
			fmt.Printf("file %v saved.\n", fh.Filename)
		}
	}

	return err
}

// 测试PostForm
// cmd : curl -d "foo1=bar1&foo2=bar2" http://127.0.0.1/test3
func handleTest3(rw httptoy.ResponseWriter, req *httptoy.Request) (err error) {

	value1 := req.PostFormValue("foo1")
	value2 := req.PostFormValue("foo2")
	fmt.Printf("post form :%v\n", req.PostForm)
	fmt.Printf("foo1=%s,foo2=%s\n", value1, value2)

	return nil
}

func TestParseForm(t *testing.T) {
	th := new(testHandler)
	th.F = func(rw httptoy.ResponseWriter, req *httptoy.Request) {
		var err error

		switch req.URL.Path {
		case "/test1":
			err = handleTest1(rw, req)
		case "/test2":
			err = handleTest2(rw, req)
		case "/test3":
			err = handleTest3(rw, req)
		}
		if err != nil {
			fmt.Println(err)
		}

		// 手动构建响应报文
		io.WriteString(rw, "HTTP/1.1 200 OK\r\n")
		io.WriteString(rw, fmt.Sprintf("Content-Length: %d\r\n", 0))
		io.WriteString(rw, "\r\n")
	}

	svr := &httptoy.Server{
		Addr:    "127.0.0.1:80",
		Handler: th,
	}
	panic(svr.ListenAndServe())
}

// TestResponse 测试 response 块写入
// test.html [小于4kb] 以及 test.webp [大于4kb] 存在于 assets 目录中，测试需要放置在该测试文件同目录下
// cmd : curl http://127.0.0.1
func TestResponse(t *testing.T) {
	th := new(testHandler)
	th.F = func(rw httptoy.ResponseWriter, req *httptoy.Request) {

		fmt.Println(req.URL.Path)
		// 照片
		if req.URL.Path == "/photo" {
			file, err := os.Open("test.webp")
			if err != nil {
				fmt.Println("open file error:", err)
				return
			}
			io.Copy(rw, file)
			file.Close()
			return
		}

		// html文件
		data, err := ioutil.ReadFile("test.html")
		if err != nil {
			fmt.Println("readFile test.html error: ", err)
			return
		}
		rw.Write(data)

	}

	svr := &httptoy.Server{
		Addr:    "127.0.0.1:80",
		Handler: th,
	}
	panic(svr.ListenAndServe())
}

type foo2Handler struct{}

func (*foo2Handler) ServeHTTP(rw httptoy.ResponseWriter, req *httptoy.Request) {
	io.WriteString(rw, "/foo2 check in.")
}

// TestServeMux 测试 ServeMux 包装的路由器
// cmd 1: curl http://127.0.0.1/foo1
// cmd 2: curl http://127.0.0.1/foo2
// cmd 3: curl http://127.0.0.1/foo1/bar1
func TestServeMux(t *testing.T) {

	// test HandleFunc
	httptoy.HandleFunc("/foo1", func(rw httptoy.ResponseWriter, req *httptoy.Request) {
		io.WriteString(rw, "/foo1 check in.")
	})

	// test Handle
	httptoy.Handle("/foo2", &foo2Handler{})

	// test pattern match
	httptoy.HandleFunc("/foo1/bar1", func(rw httptoy.ResponseWriter, req *httptoy.Request) {
		io.WriteString(rw, "/foo1/bar1")
	})

	httptoy.ListenAndServe("127.0.0.1:80", nil)
}
