package ioutil

import (
	"io"
	"sync"
)

type onCloseReader struct {
	io.ReadCloser
	once sync.Once
}

// WrapOnClose 返回一个 ReadCloser，保证 underlying rc 只被 Close 一次：
// - 正常读完（EOF）时自动关闭
// - Hertz 调用 Close() 时关闭
func WrapOnClose(rc io.ReadCloser) io.ReadCloser {
	return &onCloseReader{ReadCloser: rc}
}

func (r *onCloseReader) Read(p []byte) (n int, err error) {
	n, err = r.ReadCloser.Read(p)
	if err == io.EOF {
		r.once.Do(func() { r.ReadCloser.Close() })
	}
	return
}

func (r *onCloseReader) Close() error {
	var closeErr error
	r.once.Do(func() {
		closeErr = r.ReadCloser.Close()
	})
	return closeErr
}
