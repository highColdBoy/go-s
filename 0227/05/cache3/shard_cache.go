package cache3

import (
	"fmt"
	"sync"
	"time"
)

const ShardCount = 32 // 分成32个片段

var itemPool = sync.Pool{
	// 当池子中没东西时，定义如何创建一个新对象
	New: func() any {
		return new(Item)
	},
}

type Shard struct {
	sync.RWMutex
	data map[string]*Item
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
		cache[i] = &Shard{data: make(map[string]*Item)}
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
			/*
				这里不清洗返回池子 item 是指针类型
				1. 协程A 通过 Get("key1") 拿到了 item 的指针 正准备读取 Value
				2. 协程B或清洁工发现 key1 过期，进行了 item 清洗并放回了池子
				3. 协程C 通过 Set("key2") 又得到了这个 item的指针地址 并修改了 item 的 Value

				解决方案：只利用池子获取 而不去清洗放回
			*/
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

	// 从池子中取出一个干净的对象
	item := itemPool.Get().(*Item)
	item.Value = value
	if duration > 0 {
		item.Expiration = time.Now().Add(duration).UnixNano()
	} else {
		item.Expiration = 0
	}

	shard.data[key] = item
}

func (sc ShardedCache) Del(key string) {
	shard := sc.getShard(key)
	shard.Lock()
	defer shard.Unlock()

	delete(shard.data, key)
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
