package httptoy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
)

// multipart.go 负责 multipart-form 的解析
// 对各part的boudary进行是否最终边界的判断，否则不断进行part解析读取
// Body 对 multipart-form的读取形式如下：
/* \r\n
 * --------------------------b139b4e3da21cb36\r\n
 * Content-Disposition: form-data; name="username"\r\n
 * \r\n
 * gu\r\n
 * --------------------------b139b4e3da21cb36\r\n
 * Content-Disposition: form-data; name="password"\r\n
 * \r\n
 * 123\r\n
 * --------------------------b139b4e3da21cb36\r\n
 * Content-Disposition: form-data; name="file1"; filename="1.txt"\r\n
 * Content-Type: text/plain\r\n
 * \r\n
 * this is 1.txt!\r\n
 * --------------------------b139b4e3da21cb36\r\n
 * Content-Disposition: form-data; name="file2"; filename="2.txt"\r\n
 * Content-Type: text/plain\r\n
 * \r\n
 * this is 2.txt!\r\n
 * --------------------------b139b4e3da21cb36--\r\n
 */

type Part struct {
	Header Header // 存储 当前 part 的首部
	mr     *MultipartReader

	formName         string
	fileName         string
	closed           bool      // part 是否关闭
	substituteReader io.Reader // 替补Reader
	parsed           bool      // 是否解析过formName 以及 fileName
}

func (p *Part) Close() error {
	if p.closed {
		return nil
	}

	_, err := io.Copy(io.Discard, p)
	p.closed = true

	return err
}

// Read 需要处理 Body 存在数据读取时如何寻找boundary以及 Body 出现 eof 情况
// 1.Body 出现 eof 时，如果以及找到 终止边界，则需要关闭 Part
// 另一种情况是 客户端异常提前关闭了表单连接，导致服务端没读取完，则同样进行关闭
// 2.正常的解析Boundary，通过 bufr的 peek方法寻找 boundary 出现的位置，读取之前的数据进行下一步的字符串解析
// 可能出现 bufSize长度的缓存数据中没找到，需要保留 最后 boundary 长度的数据，需要跟下一次缓存数据拼接成 boundary标志
func (p *Part) Read(buf []byte) (n int, err error) {
	// case 1: p 已经关闭
	if p.closed {
		return 0, io.EOF
	}

	// case 2: p 已经寻找到boundary，直接读取属于p的边界之前的数据
	if p.substituteReader != nil {
		return p.substituteReader.Read(buf)
	}

	// case 3: 寻找 p 的边界, 并创建 limitReader
	var peek []byte
	bufr := p.mr.bufr

	// 预览指定长度的缓存数据
	peek, err = bufr.Peek(bufSize)
	// 如果预览读取发现 Body 没有数据可读，则对已缓存数据进行预览
	if err == io.EOF {
		p.mr.occurEofErr = true
		peek, _ = bufr.Peek(bufr.Buffered())
	} else if err != nil {
		return 0, err
	}

	// 在 peek 中寻找 boundary
	index := bytes.Index(peek, p.mr.crlfDashBoundary)
	// sub case 1: 如果index != -1，说明找到boundary，读取index之前的数据
	// sub case 2: 如果index == -1，并且 Body 没有数据可读，http连接提前关闭，则读取 -1之前的数据，eof提前关闭
	if index != -1 || p.mr.occurEofErr {
		//读取 index 之前的数据
		p.substituteReader = io.LimitReader(bufr, int64(index))
		return p.substituteReader.Read(buf)
	}

	// sub case 3: 如果 index == -1 ，但 Body 还有数据可读，则留空最后boundary长度不读，作为下次peek寻找boundary的候选缓存数据
	maxRead := bufSize - len(p.mr.crlfDashBoundary) + 1
	// 如果 最大读取长度 大于 buf长度, 则直接读满buf
	if maxRead > len(buf) {
		maxRead = len(buf)
	}

	return bufr.Read(buf[:maxRead])
}

// parseFormData 必须在 readHeader 之后调用，否则可能为空串，或是旧字符串
// parseFormData 用于解析 multipart中每个part第一行的信息
// E.g. Content-Disposition: form-data; name="file1"; filename="1.txt"\r\n
func (p *Part) parseFormData() {
	p.parsed = true // 先设置，防止卡壳

	// cd 因为被 readHeader 做处理，所以如下形式
	// form-data; name="password"
	cd := p.Header.Get("Content-Disposition")
	splitCd := strings.Split(cd, ";")
	// 解析错误处理
	if len(splitCd) < 2 || strings.ToLower(splitCd[0]) != "form-data" {
		return
	}

	// 提取信息
	for _, str := range splitCd[1:] {
		kvStr := strings.Split(str, "=")
		if len(kvStr) != 2 { // 格式错误
			return
		}

		k, v := strings.TrimSpace(kvStr[0]), strings.Trim(kvStr[1], `"`)
		switch k { // 用 switch 进行正确的解析
		case "name":
			p.formName = v
		case "filename":
			p.fileName = v
		}
	}
}

// FormName ...
func (p *Part) FormName() string {
	// lazyload
	if !p.parsed {
		p.parseFormData()
	}

	return p.formName
}

// FileName ...
func (p *Part) FileName() string {
	if !p.parsed {
		p.parseFormData()
	}

	return p.fileName
}

const bufSize = 4 << 10 // 滑动窗口的大小

type MultipartReader struct {
	// bufr 是对 Body 的封装，方便使用peek预查Body上的数据，从而确定part之间边界
	// 每个part共享这个bufr，但只有Body的读取指针指向对应part的报文
	// 对应的part能从指针中读取数据，此时其他part是无效的
	bufr *bufio.Reader

	// occurEofErr 记录bufr的读取过程中是否出现io.EOF错误，
	// 如果发送了这个错误，说明Body数据消费完毕，表单报文消费完毕，不需要产生part
	occurEofErr          bool
	crlfDashBoundaryDash []byte  // \r\n--boundary--
	crlfDashBoundary     []byte  // \r\n--boundary 分隔符
	dashBoundary         []byte  // --boundary
	dashBoundaryDash     []byte  // --boundary--
	curPart              *Part   // 当前解析到了哪个part
	crlf                 [2]byte // 用于消费 \r\n
}

func NewMultipartReader(r io.Reader, boundary string) *MultipartReader {
	b := []byte(fmt.Sprintf("\r\n--%v--", boundary))
	bLen := len(b)

	return &MultipartReader{
		bufr:                 bufio.NewReaderSize(r, bufSize),
		crlfDashBoundaryDash: b,
		crlfDashBoundary:     b[:bLen-2],
		dashBoundary:         b[2 : bLen-2],
		dashBoundaryDash:     b[2:],
	}
}

func (mr *MultipartReader) discardCRLF() error {
	_, err := io.ReadFull(mr.bufr, mr.crlf[:])

	// 如果完整的读取后续并且 是 \r\n， 则chunk编码格式没问题
	if err == nil && mr.crlf[0] == '\r' && mr.crlf[1] == '\n' {
		return nil
	}

	return fmt.Errorf("Expect crlf, but got %s", mr.crlf)
}

func (mr *MultipartReader) NextPart() (p *Part, err error) {
	// 如果curPart存在，将其关闭，消费掉当前part数据，让下一次part做准备
	if mr.curPart != nil {
		if err = mr.curPart.Close(); err != nil {
			return
		}

		if err = mr.discardCRLF(); err != nil {
			return
		}
	}

	// 前提: 当前是创建新part的时候，因此下一行必定是boundary, 对其判断是否为终止边界，否则继续创建下一个part
	line, err := readLine(mr.bufr)
	if err != nil {
		return
	}

	// 到达 multipart 表单末尾，终止读取
	if bytes.Equal(line, mr.dashBoundaryDash) {
		return nil, io.EOF
	}
	// 如果当前行不是boudary，则出现错误
	if !bytes.Equal(line, mr.dashBoundary) {
		err = fmt.Errorf("Want delimiter %s, but got %s", mr.dashBoundary, line)
		return
	}

	p = new(Part)
	p.mr = mr
	// 为 part的类似首部字段解析处理
	p.Header, err = readHeader(mr.bufr)
	if err != nil {
		return
	}

	mr.curPart = p
	return
}

type FileHeader struct {
	Filename string
	Header   Header
	Size     int
	content  []byte
	tmpFile  string
}

// Open ...
func (fh *FileHeader) Open() (io.ReadCloser, error) {
	// case 1:如果 fh 有临时文件保存，则返回 文件流
	if fh.tmpFile != "" {
		return os.Open(fh.tmpFile)
	}

	// case 2: 如果没有临时文件保存，则是保存在内存中，返回读取数组的 Reader
	b := bytes.NewReader(fh.content)
	return ioutil.NopCloser(b), nil
}

// Save 将 fh 保存到 指定dest路径文件中
func (fh *FileHeader) Save(dest string) error {
	rc, err := fh.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	file, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, rc)
	if err != nil {
		return err
	}

	return nil
}

type MultipartForm struct {
	Value map[string]string
	File  map[string]*FileHeader
}

// RemoveAll 用于 handler 结束后删除临时文件
func (mf *MultipartForm) RemoveAll() {
	for _, fh := range mf.File {
		if fh == nil || fh.tmpFile == "" {
			continue
		}

		os.Remove(fh.tmpFile)
	}
}

// ReadForm ...
func (mr *MultipartReader) ReadForm() (*MultipartForm, error) {
	var (
		part             *Part
		err              error
		n                int64
		nonFileMaxMemory int64 = 10 << 20 // 保存非文件数据最大内存为 10 mb
		fileMaxMemory    int64 = 30 << 20 // 保存文件数据最大内存为 30 mb
	)

	mf := MultipartForm{
		Value: make(map[string]string),
		File:  make(map[string]*FileHeader),
	}

	for {
		part, err = mr.NextPart()
		if err == io.EOF || err != nil {
			break
		}

		name := part.FormName()
		if name == "" {
			continue
		}

		var buff bytes.Buffer
		filename := part.FileName()

		// case 1: non-file part
		if filename == "" {
			n, err = io.CopyN(&buff, part, nonFileMaxMemory+1)
			if err != nil && err != io.EOF {
				break
			}

			// 统计内存
			nonFileMaxMemory -= n
			if nonFileMaxMemory < 0 { // sub-case 1: 超过保存非文件最大内存则报错
				return nil, errors.New("Multipart: message too large.")
			}

			// sub-case 2: 保存成字符串
			mf.Value[name] = buff.String()
			continue
		}

		// case 2: file part
		n, err = io.CopyN(&buff, part, fileMaxMemory+1)
		if err != nil && err != io.EOF {
			break
		}

		fh := FileHeader{
			Filename: filename,
			Header:   part.Header,
		}

		// sub-case 1:超过文件最大内存限制，保存到临时文件
		if n > fileMaxMemory {
			var tmpFile *os.File
			tmpFile, err = os.CreateTemp("", "multipart-")
			if err != nil { // 创建临时文件失败
				break
			}

			// 将拷贝到 buff的数据以及 part剩余的部分写入硬盘中
			n, err = io.Copy(tmpFile, io.MultiReader(&buff, part))
			if cerr := tmpFile.Close(); err == nil {
				err = cerr
			}
			if err != nil { // 如果写入 或者 关闭文件出错，则都删除 tmpFile
				os.Remove(tmpFile.Name())
				break
			}

			// 设置超过内存的文件大小，以及临时文件名
			fh.Size = int(n)
			fh.tmpFile = tmpFile.Name()
		} else {
			// sub-case 2:未超过文件最大内存限制
			fh.content = buff.Bytes()
			fh.Size = int(n)
			fileMaxMemory -= n
		}

		mf.File[name] = &fh
	}

	return &mf, err
}
