package pool

import "sync"

// 🌊 工业级字节缓冲池：有效降低高并发下的 GC 压力
// 预设 64KB 大小。注意：这只是基础块，如果数据超过此大小，我们将使用动态分配。
var BytesPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 64*1024)
	},
}

// Get 从池中获取一个缓冲区
func Get() []byte {
	return BytesPool.Get().([]byte)
}

// Put 将缓冲区归还到池中
func Put(b []byte) {
	// 🛡️ 防御性编程：只回收 64KB 的切片。
	// 大于 64KB 的由于是动态申请的，交由系统 GC 自动回收，防止内存爆掉。
	if cap(b) == 64*1024 {
		BytesPool.Put(b[:64*1024])
	}
}
