package cache2

import (
	"fmt"
	"sync"
	"time"
)

const ShardCount = 32 // 分成32个片段

type Shard struct {
	sync.RWMutex
	data map[string]Item
}

type Item struct {
	Value      any
	Expiration int64 // 过期时间戳（纳秒或秒）
}

func (item *Item) IsExpired() bool {
	if item.Expiration == 0 { // 0 代表永久有效
		return false
	}
	return time.Now().UnixNano() > item.Expiration
}

type ShardedCache []*Shard

// NewShardedCache 初始化
func NewShardedCache() ShardedCache {
	cache := make(ShardedCache, ShardCount)
	for i := 0; i < ShardCount; i++ {
		cache[i] = &Shard{data: make(map[string]Item)}
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
	item, sExists := shard.data[key]
	shard.RUnlock()

	if sExists {
		if item.IsExpired() {
			// 发现过期了，尝试删除（这里要换成写锁）
			shard.Lock()
			delete(shard.data, key)
			shard.Unlock()
			return nil
		}
		return item.Value
	}
	return nil
}

func (sc ShardedCache) Set(key string, value any, duration time.Duration) {
	shard := sc.getShard(key)
	shard.Lock()
	defer shard.Unlock()
	var expiration int64 = 0
	if duration > 0 {
		expiration = time.Now().Add(duration).UnixNano()
	}
	shard.data[key] = Item{Value: value, Expiration: expiration}
}

func (sc ShardedCache) StartCleaner() {
	// 创建一个每5s触发一次的定时器
	ticker := time.NewTicker(5 * time.Second)

	go func() {
		for range ticker.C {
			fmt.Println(" [清洁工] 开始扫描过期 Key...")
			for _, shard := range sc {
				sc.cleanShard(shard)
			}
		}
	}()
}

func (sc ShardedCache) cleanShard(shard *Shard) {
	shard.Lock()
	defer shard.Unlock()

	for key, item := range shard.data {
		if item.IsExpired() {
			delete(shard.data, key)
		}
	}
}
