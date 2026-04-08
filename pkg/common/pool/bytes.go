package pool

import "sync"

// 🌊 工业级字节缓冲池：有效降低高并发下的 GC 压力，防止内存碎片化。
// 预设 32KB 大小，能够满足绝大多数 TCP 数据包的吞吐需求。
var BytesPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 32*1024)
	},
}

// Get 从池中获取一个缓冲区
func Get() []byte {
	return BytesPool.Get().([]byte)
}

// Put 将缓冲区归还到池中
func Put(b []byte) {
	// 🛡️ 防御性编程：只回收固定大小且能重复利用的切片，防止异种容量破坏池结构
	if cap(b) >= 32*1024 {
		BytesPool.Put(b[:32*1024])
	}
}
