package cache

import (
	"fmt"
	"testing"
)

// 测试分段锁性能
func BenchmarkShardedCache_Set(b *testing.B) {
	cache := NewShardedCache()
	// 使用并行测试（多goroutine并发执行）
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		// 循环执行 b.N 次（由Go测试框架自动决定）
		for pb.Next() {
			cache.Set(fmt.Sprintf("key-%d", i), i)
			i++
		}
	})
}

func BenchmarkSimpleCache_Set(b *testing.B) {
	cache := &SimpleCache{data: make(map[string]any)}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cache.Set(fmt.Sprintf("key-%d", i), i)
			i++
		}
	})
}
