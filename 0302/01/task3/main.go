package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type Task interface {
	Execute(ctx context.Context) error
	Name() string
}

// Scheduler 常驻调度器
type Scheduler struct {
	taskQueue   chan Task      // 任务队列
	workerCount int            // 协程池大小
	wg          sync.WaitGroup // 等待所有 worker 退出
	ctx         context.Context
	stopFunc    context.CancelFunc
}

func NewScheduler(workerCount int, queueSize int) *Scheduler {
	// 创建一个可以手动取消的 Context 用于控制整个调度器的生命周期
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		taskQueue:   make(chan Task, queueSize),
		workerCount: workerCount,
		ctx:         ctx,
		stopFunc:    cancel,
	}
}

// Start 启动 Worker 池
func (s *Scheduler) Start() {
	fmt.Printf("[SYSTEM] 调度器启动，工作协程数量: %d\n", s.workerCount)
	for i := 0; i < s.workerCount; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}
}

// worker 内部工作逻辑
func (s *Scheduler) worker(workerID int) {
	defer s.wg.Done()
	fmt.Printf("[WORKER-%d] 已就绪\n", workerID)

	for {
		select {
		case task, ok := <-s.taskQueue:
			if !ok {
				// 通道关闭，Worker 退出
				fmt.Printf("[WORKER-%d] 任务队列关闭，正在退出...\n", workerID)
				return
			}
			s.handleTask(workerID, task)
		case <-s.ctx.Done():
			// 收到系统停止信号
			fmt.Printf("[WORKER-%d] 收到停止信号，清理中...\n", workerID)
			return
		}
	}
}

func (s *Scheduler) handleTask(workerID int, t Task) {
	// 这里可以集成中间件逻辑
	fmt.Printf("[WORKER-%d] 正在处理: %s\n", workerID, t.Name())

	// 为单个任务设置超时，防止单次任务阻塞整个 Worker
	taskCtx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()

	if err := t.Execute(taskCtx); err != nil {
		fmt.Printf("[ERROR] %s 执行失败: %v\n", t.Name(), err)
	} else {
		fmt.Printf("[SUCCESS] %s 完成\n", t.Name())
	}
}

func (s *Scheduler) Submit(t Task) error {
	select {
	case s.taskQueue <- t:
		return nil
	case <-s.ctx.Done():
		return errors.New("调度器已关闭，无法提交任务")
	default:
		// 如果队列满了，可以根据业务决定是阻塞还是报错
		return errors.New("任务队列已满")
	}
}

// Stop 优雅停机
// force: 如果为 true，则立刻中断正在执行的任务；如果为 false，则处理完队列中的存量任务。
func (s *Scheduler) Stop(force bool) {
	fmt.Printf("\n[SYSTEM] 正在关闭调度器 (Force: %v)...\n", force)

	if force {
		s.stopFunc()
	} else {
		close(s.taskQueue)
	}
	s.wg.Wait()

	// 扫尾工作：即便不是强制退出，最后也要 cancel 释放 context 资源
	if !force {
		s.stopFunc()
	}
	fmt.Println("[SYSTEM] 调度器安全退出")
}

type SimpleTask struct {
	ID int
}

func (s *SimpleTask) Name() string { return fmt.Sprintf("Task-%d", s.ID) }
func (s *SimpleTask) Execute(ctx context.Context) error {
	// 模拟随机耗时
	sleep := time.Duration(rand.Intn(500)) * time.Millisecond
	select {
	case <-time.After(sleep):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func main() {
	// 初始化：3个 Worker，队列容量 10
	scheduler := NewScheduler(3, 10)
	scheduler.Start()

	// 模拟持续产生任务的生产者
	go func() {
		for i := 1; i <= 10; i++ {
			task := &SimpleTask{ID: i}
			if err := scheduler.Submit(task); err != nil {
				fmt.Printf("[SEND ERROR] %v\n", err)
			}
			time.Sleep(200 * time.Millisecond) // 每200ms产生一个任务
		}
	}()

	// 运行 3 秒后关闭系统，模拟服务器重启或停机
	time.Sleep(3 * time.Second)
	scheduler.Stop(false)
}
