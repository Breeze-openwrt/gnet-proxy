package pool

import "sync"

// 🌊 [工业级多级分片池]：有效降低高吞吐带来的 GC 压力。
// 预设三种常用规格，复盖 95% 的网络包场景。
var (
	pool64K = sync.Pool{New: func() interface{} { return make([]byte, 64*1024) }}
	pool256K = sync.Pool{New: func() interface{} { return make([]byte, 256*1024) }}
	pool1M = sync.Pool{New: func() interface{} { return make([]byte, 1024*1024) }}
)

// Get 根据所需大小，从最匹配的池中获取缓冲区
func Get(size int) []byte {
	if size <= 64*1024 {
		return pool64K.Get().([]byte)
	} else if size <= 256*1024 {
		return pool256K.Get().([]byte)
	} else if size <= 1024*1024 {
		return pool1M.Get().([]byte)
	}
	// 超大包由于极其罕见，直接内存分配，交给 GC 处理
	return make([]byte, size)
}

// Put 将缓冲区归还到对应的池中
func Put(b []byte) {
	c := cap(b)
	switch c {
	case 64 * 1024:
		pool64K.Put(b[:64*1024])
	case 256 * 1024:
		pool256K.Put(b[:256*1024])
	case 1024 * 1024:
		pool1M.Put(b[:1024*1024])
	}
}
