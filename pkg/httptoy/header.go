package httptoy

// header 针对请求报文的首部字段的解析

// Header 用来储存一次请求报文的键值对
type Header map[string][]string

func (h Header) Add(key, val string) {
	h[key] = []string{val}
}

func (h Header) Set(key, val string) {
	h[key] = []string{val}
}

func (h Header) Get(key string) string {
	if value, ok := h[key]; ok && len(value) > 0 {
		return value[0]
	} else {
		return ""
	}
}

func (h Header) Del(key string) {
	delete(h, key)
}
