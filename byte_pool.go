package remotedialer

import (
	"errors"
	"io"
	"sync"
)

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

// CopyBuffer is the actual implementation of Copy and CopyBuffer.
// if buf is nil, one is allocated.
func CopyBuffer(dst io.Writer, src io.Reader, buf []byte) (written int64, err error) {
	// If the reader has a WriteTo method, use it to do the copy.
	// Avoids an allocation and a copy.
	if wt, ok := src.(io.WriterTo); ok {
		return wt.WriteTo(dst)
	}
	// Similarly, if the writer has a ReadFrom method, use it to do the copy.
	if rt, ok := dst.(io.ReaderFrom); ok {
		return rt.ReadFrom(src)
	}
	if buf == nil {
		size := 32 * 1024
		if l, ok := src.(*io.LimitedReader); ok && int64(size) > l.N {
			if l.N < 1 {
				size = 1
			} else {
				size = int(l.N)
			}
		}
		buf = make([]byte, size)
	}
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = errors.New("invalid write result")
				}
			}
			written += int64(nw)
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}
