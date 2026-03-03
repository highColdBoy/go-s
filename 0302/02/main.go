package main

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// Priority 任务优先级
type Priority int

const (
	Low Priority = iota
	Medium
	High
)

// PriorityTask 带优先级的任务
type PriorityTask struct {
	fn       func(ctx context.Context) error
	timeout  time.Duration
	priority Priority
	index    int
}

// PriorityQueue 优先级队列
type PriorityQueue []*PriorityTask

// 实现 container/heap 接口

func (pq *PriorityQueue) Len() int           { return len(*pq) }
func (pq *PriorityQueue) Less(i, j int) bool { return (*pq)[i].priority > (*pq)[j].priority }
func (pq *PriorityQueue) Swap(i, j int) {
	(*pq)[i], (*pq)[j] = (*pq)[j], (*pq)[i]
	(*pq)[i].index = i
	(*pq)[j].index = j
}

func (pq *PriorityQueue) Push(x any) {
	task := x.(*PriorityTask)
	task.index = len(*pq)
	*pq = append(*pq, task)
}

func (pq *PriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	task := old[n-1]
	task.index = -1
	*pq = old[:n-1]
	return task
}

type AdvancedPool struct {
	pq           PriorityQueue
	mu           sync.Mutex
	wg           sync.WaitGroup
	once         sync.Once
	tokenBucket  chan struct{} // 令牌桶限流
	workerCount  atomic.Int32  // 当前 worker 数
	maxWorker    int32         // 最大 worker 数
	minWorker    int32         // 最小 worker 数
	queueLen     atomic.Int32  // 队列长度
	successCount atomic.Int32  // 成功任务数
	failCount    atomic.Int32  // 失败任务数
}

func NewAdvancedPool(minWorker, maxWorker int32, qps int) *AdvancedPool {
	p := &AdvancedPool{
		minWorker:   minWorker,
		maxWorker:   maxWorker,
		tokenBucket: make(chan struct{}, qps),
	}

	heap.Init(&p.pq)

	// 初始化令牌桶（每秒填充 qbs 个令牌）
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			for i := 0; i < qps; i++ {
				select {
				case p.tokenBucket <- struct{}{}:
				default:
				}
			}
		}
	}()

	// 初始化最小 worker
	for i := 0; i < int(minWorker); i++ {
		p.workerCount.Add(1)
		go p.worker(i)
	}

	// 指标监控
	go p.metricMonitor()

	return p
}

func (p *AdvancedPool) worker(id int) {
	for {
		p.mu.Lock()
		if p.pq.Len() == 0 {
			p.mu.Unlock()
			// 队列为空时，判断是否缩容
			if p.workerCount.Load() > p.minWorker {
				p.workerCount.Add(-1)
				fmt.Printf("Worker [%d] 退出，当前 worker 数: %d\n", id, p.workerCount.Load())
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		// 取出最高优先级任务
		task := heap.Pop(&p.pq).(*PriorityTask)
		p.queueLen.Add(-1)
		p.mu.Unlock()

		// 执行任务
		func() {
			defer func() {
				if r := recover(); r != nil {
					//fmt.Printf("Worker [%d] Panic: %v, Stack: %s\n", id, r, debug.Stack())
					fmt.Printf("Worker [%d] Panic: %v\n", id, r)
					p.failCount.Add(1)
				}
				p.wg.Done()
			}()

			maxRetries := 2
			var err error
			for i := 0; i <= maxRetries; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), task.timeout)
				if i > 0 {
					fmt.Printf("Worker [%d] 重试任务（优先级：%d），第 %d 次\n", id, task.priority, i)
				}
				err = task.fn(ctx)
				cancel()
				if err == nil || !errors.Is(err, context.DeadlineExceeded) {
					break
				}
			}

			if err != nil {
				fmt.Printf("Worker [%d] 任务失败（优先级：%d）: %v\n", id, task.priority, err)
				p.failCount.Add(1)
			} else {
				fmt.Printf("Worker [%d] 任务成功（优先级：%d）\n", id, task.priority)
				p.successCount.Add(1)
			}
		}()
	}
}

func (p *AdvancedPool) Submit(fn func(ctx context.Context) error, timeout time.Duration, priority Priority) bool {
	// 令牌通限流
	select {
	case <-p.tokenBucket:
	case <-time.After(2 * time.Second):
		fmt.Println("提交任务超时（限流")
		return false
	}

	p.wg.Add(1)
	p.mu.Lock()
	heap.Push(&p.pq, &PriorityTask{fn: fn, timeout: timeout, priority: priority})
	p.queueLen.Add(1)
	p.mu.Unlock()

	// 触发扩容（队列长度 > 当前 worker 数，且未到最大 worker）
	if p.queueLen.Load() > p.workerCount.Load() && p.workerCount.Load() < p.maxWorker {
		p.workerCount.Add(1)
		go p.worker(int(p.workerCount.Load()))
		fmt.Printf("扩容 worker，当前数: %d\n", p.workerCount.Load())
	}

	return true
}

// 指标监控
func (p *AdvancedPool) metricMonitor() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		total := p.successCount.Load() + p.failCount.Load()
		successRate := 0.0
		if total > 0 {
			successRate = float64(p.successCount.Load()) / float64(total) * 100
		}
		fmt.Printf("\n=== 监控指标 ===\n")
		fmt.Printf("队列长度: %d | 活跃 worker: %d | 总任务数: %d | 成功率: %.2f%%\n",
			p.queueLen.Load(), p.workerCount.Load(), total, successRate)
	}
}

// Shutdown 优雅关闭
func (p *AdvancedPool) Shutdown() {
	p.once.Do(func() {
		p.wg.Wait()
		fmt.Println("所有任务执行完成，工作池关闭")
	})
}

func main() {
	// 初始化：最小2个worker，最大5个，QPS限流10
	pool := NewAdvancedPool(2, 5, 10)

	// 提交不同优先级任务
	for i := 0; i < 20; i++ {
		taskID := i
		var priority Priority
		switch {
		case i%3 == 0:
			priority = High
		case i%3 == 1:
			priority = Medium
		default:
			priority = Low
		}

		pool.Submit(func(ctx context.Context) error {
			fmt.Printf("[任务] %d 任务被执行\n", taskID)
			// 模拟任务耗时
			sleep := time.Duration(rand.Intn(1500)) * time.Millisecond
			select {
			case <-time.After(sleep):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, 2*time.Second, priority)
	}

	// 模拟 panic 任务
	pool.Submit(func(ctx context.Context) error {
		var nilSlice []int
		fmt.Println(nilSlice[10]) // 触发 panic
		return nil
	}, 2*time.Second, High)

	time.Sleep(10 * time.Second)
	pool.Shutdown()
}
