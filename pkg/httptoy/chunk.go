package httptoy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// chunkReader.go 针对 Request.setupBody 对 r.Body 的 chunk编码的读取解码
// chunk size 由十六进制表示
// chunk data 对应 chunk size 长度的数据
/* chunk 编码示例：
 * HTTP/1.1 200 OK\r\n
 * Content-Type: text/plain\r\n
 * Transfer-Encoding: chunked\r\n
 * \r\n
 *
 * # 以下为body
 * 17\r\n							#chunk size
 * hello, this is chunked \r\n		#chunk data
 * D\r\n							#chunk size
 * data sent by \r\n				#chunk data
 * 7\r\n							#chunk size
 * client!\r\n						#chunk data
 * 0\r\n\r\n						#end
 */

var (
	crlf = []byte("\r\n")
)

type chunkReader struct {
	n    int           // 当前处理的块中还有多少字节未读
	bufr *bufio.Reader // 读取body 的缓冲字节流
	done bool          // 记录报文读取完毕
	crlf [2]byte       // 用来读取 \r\n
}

func (cr *chunkReader) discardCRLF() error {
	_, err := io.ReadFull(cr.bufr, cr.crlf[:])

	// 如果完整的读取后续并且 是 \r\n， 则chunk编码格式没问题
	if err == nil && cr.crlf[0] == crlf[0] && cr.crlf[1] == crlf[1] {
		return nil
	}

	return errors.New("unsupported encoding format of chunk.")
}

func (cw *chunkReader) getChunkSize() (int, error) {
	var chunkSize int
	// readLine 有bufr读取一整行， \r\n被清除
	line, err := readLine(cw.bufr)
	if err != nil {
		return chunkSize, err
	}

	//将16进制换算成10进制
	// a b c d e f 补位 10 11 12 13 14 15
	// 16进位
	for i := 0; i < len(line); i++ {
		// ascii | 0x20 之后 ‘0’之前字符会变大， 非 ‘a' 字符同样有区间
		b1 := int((line[i] | 0x20))
		if b1-'0' > -1 && b1-'0' < 10 {
			chunkSize = chunkSize*16 + b1 - '0'
		} else if b1-'a' > -1 && b1-'a' < 6 {
			chunkSize = chunkSize*16 + b1 - 'a' + 10
		} else {
			return 0, errors.New("illegal hex number")
		}
	}

	return chunkSize, err
}

func (cr *chunkReader) Read(p []byte) (int, error) {
	if cr.done {
		return 0, io.EOF
	}

	var (
		n   int
		err error
	)

	if cr.n == 0 {
		cr.n, err = cr.getChunkSize()
		if err != nil {
			return 0, err
		}

		// 如果获取的chunksize为0，说明读到chunk报文结尾
		if cr.n == 0 {
			cr.done = true

			// 清理掉最后的CRLF，防止影响下一个http报文的解析
			err = cr.discardCRLF()

			return 0, err
		}
	}

	// 正常读取
	// 如果当前块剩余的数据长度 大于 待读取的数组长度，则读取，并且更新未读取的chunk 长度
	if len(p) <= cr.n {
		n, err = cr.bufr.Read(p)

		cr.n -= n
		return n, err
	}

	// 如果读取数组长度过长
	// 如果当前块剩余的数据长度 小于 带读取的数组长度，读取剩余chunk data，并且清除掉后面的 \r\n
	n, _ = io.ReadFull(cr.bufr, p[:cr.n])
	cr.n = 0
	err = cr.discardCRLF()

	return n, err
}

type chunkWriter struct {
	wrote bool // 写header的flag
	resp  *Response
}

// Write ...
func (cw *chunkWriter) Write(p []byte) (n int, err error) {
	if !cw.wrote {
		cw.finalizeHeader(p)
		cw.writeHeader()
		cw.wrote = true
	}

	isChunked := cw.resp.chunking
	bufw := cw.resp.c.bufw

	if isChunked {
		// 写入chunk size [十六进制]
		_, err = fmt.Fprintf(bufw, "%x\r\n", len(p))
		if err != nil {
			return 0, err
		}
	}

	n, err = bufw.Write(p)
	if err == nil && isChunked {
		// 写入 chunk data 结束符
		_, err = bufw.Write(crlf)
	}

	return n, err
}

// finalizeHeader ...
func (cw *chunkWriter) finalizeHeader(p []byte) {
	header := cw.resp.header

	// 如果未设置响应报文类型，则检测设置
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", http.DetectContentType(p))
	}

	// 如果未设置响应传递方式
	if header.Get("Content-Length") == "" && header.Get("Transfer-Encoding") == "" {
		// case 1: conn连接已经结束，此时需要chunkWriter确定发送报文，由于缓存大小为4kb，如不连接结束之前没有发送报文，那么在结束之后还有缓存的数据没发送，其小于4kb，并且是第一次发送
		if cw.resp.handlerDone {
			buffered := cw.resp.bufw.Buffered()
			header.Set("Content-Length", strconv.Itoa(buffered))
		} else {
			// case 2： conn 没有结束时，需要写入，说明resp的缓存流的数据超过4kb需要发送，可能还有数据需要发送，将编码改成 chunked
			cw.resp.chunking = true
			header.Set("Transfer-Encoding", "chunked")
		}
		return
	}

	// 如果已经设置了响应传递方式
	if header.Get("Transfer-Encoding") == "chunked" {
		cw.resp.chunking = true
	}
}

// writeHeader 写响应头信息
func (cw *chunkWriter) writeHeader() {

	// 写入状态行
	bufw := cw.resp.c.bufw

	bufw.WriteString(cw.resp.req.Proto)
	bufw.WriteByte(' ')
	bufw.Write(strconv.AppendInt([]byte{}, int64(cw.resp.statusCode), 10))
	bufw.WriteByte(' ')
	bufw.WriteString(http.StatusText(cw.resp.statusCode))
	bufw.Write(crlf)

	// header 写入
	for k, v := range cw.resp.header {
		bufw.WriteString(k)
		bufw.WriteString(": ")
		bufw.WriteString(v[0])
		bufw.Write(crlf)
	}

	// 首部字段分隔符
	bufw.Write(crlf)
}
