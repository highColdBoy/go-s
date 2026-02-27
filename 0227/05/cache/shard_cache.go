package cache

import "sync"

const ShardCount = 32 // 分成32个片段

type Shard struct {
	sync.RWMutex
	data map[string]any
}

type ShardedCache []*Shard

// NewShardedCache 初始化
func NewShardedCache() ShardedCache {
	cache := make(ShardedCache, ShardCount)
	for i := 0; i < ShardCount; i++ {
		cache[i] = &Shard{data: make(map[string]any)}
	}
	return cache
}

// getShard 根据key计算它属于哪个片段
func (sc ShardedCache) getShard(key string) *Shard {
	var sum uint32
	for _, char := range key {
		sum += uint32(char)
	}

	// 用任意数对32取模，得到的结果是一个0到31之间的确定编号
	return sc[sum%ShardCount]
}

func (sc ShardedCache) Get(key string) any {
	shard := sc.getShard(key)
	shard.RLock()
	defer shard.RUnlock()
	return shard.data[key]
}

func (sc ShardedCache) Set(key string, value any) {
	shard := sc.getShard(key)
	shard.Lock()
	defer shard.Unlock()
	shard.data[key] = value
}
