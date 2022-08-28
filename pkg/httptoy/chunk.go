package httptoy

import (
	"bufio"
	"errors"
	"io"
)

// chunkReader.go 针对 Request.setupBody 对 r.Body 的 chunk编码的读取解码
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
// chunk size 由十六进制表示
// chunk data 对应 chunk size 长度的数据

type chunkReader struct {
	n    int           // 当前处理的块中还有多少字节未读
	bufr *bufio.Reader // 读取body 的缓冲字节流
	done bool          // 记录报文读取完毕
	crlf [2]byte       // 用来读取 \r\n
}

func (cr *chunkReader) discardCRLF() error {
	_, err := io.ReadFull(cr.bufr, cr.crlf[:])

	// 如果完整的读取后续并且 是 \r\n， 则chunk编码格式没问题
	if err == nil && cr.crlf[0] == '\r' && cr.crlf[1] == '\n' {
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
		switch {
		case 'a' <= line[i] && line[i] <= 'f':
			chunkSize = chunkSize*16 + int(line[i]-'a') + 10
		case 'A' <= line[i] && line[i] <= 'F':
			chunkSize = chunkSize*16 + int(line[i]-'A') + 10
		case '0' <= line[i] && line[i] <= '9':
			chunkSize = chunkSize*16 + int(line[i]-'0')
		default:
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
