package remotedialer

import "sync"

type BytePool struct {
	p sync.Pool
}

func NewBytePool(size int) *BytePool {
	p := &BytePool{}
	p.p.New = func() any {
		return make([]byte, size)
	}
	return p
}

// Get 获取字节数组
func (p *BytePool) Get() []byte {
	return p.p.Get().([]byte)
}

// Put 归还字节数组
func (p *BytePool) Put(b []byte) {
	// 重置已用大小
	b = b[:0]
	p.p.Put(b)
}
