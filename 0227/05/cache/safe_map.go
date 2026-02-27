package cache

import "sync"

type SimpleCache struct {
	data map[string]any
	mu   sync.RWMutex // 读写锁
}

func (c *SimpleCache) Get(key string) any {
	c.mu.RLock() // 开启读锁：允许多个人同时读，但禁止写
	defer c.mu.RUnlock()
	return c.data[key]
}

func (c *SimpleCache) Set(key string, value any) {
	c.mu.Lock() // 开启写锁：完全互斥，其他人不能读也不能写
	defer c.mu.Unlock()
	c.data[key] = value
}

/**
SimpleCache 有个致命缺点：锁竞争太剧烈。
想象一下，如果你有 1000 个协程在并发运行，即使它们修改的是不同的 Key，
也必须去抢同一把锁。这在高性能场景下是不能接受的。
*/
