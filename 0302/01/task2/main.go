package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type Task interface {
	Execute(ctx context.Context) error
	Name() string
}

// Handler 任务执行的函数原型，用于构建中间件链
type Handler func(ctx context.Context, t Task) error

// LoggingMiddleware 日志中间件
func LoggingMiddleware(next Handler) Handler {
	return func(ctx context.Context, t Task) error {
		start := time.Now()
		traceID, _ := ctx.Value("trace_id").(string)
		fmt.Printf("[LOG] >>> 开始执行: %s | Trace: %s\n", t.Name(), traceID)

		err := next(ctx, t)

		duration := time.Since(start)
		if err != nil {
			fmt.Printf("[LOG] <<< 执行失败: %s | 耗时: %v | 错误: %v\n", t.Name(), duration, err)
		} else {
			fmt.Printf("[LOG] <<< 执行成功: %s | 耗时: %v\n", t.Name(), duration)
		}
		return err
	}
}

// RecoveryMiddleware 恢复中间件
func RecoveryMiddleware(next Handler) Handler {
	return func(ctx context.Context, t Task) error {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[PANIC RECOVER] 捕获到异常: %v\n", r)
			}
		}()

		return next(ctx, t)
	}
}

type Scheduler struct {
	maxConcurrency int
	successCount   int64
	failCount      int64
	middlewares    []func(Handler) Handler
}

// NewScheduler 使用选项模式初始化
func NewScheduler(limit int, opts ...func(*Scheduler)) *Scheduler {
	s := &Scheduler{maxConcurrency: limit}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithMiddleware 允许用户动态注入中间件
func WithMiddleware(mw ...func(Handler) Handler) func(*Scheduler) {
	return func(s *Scheduler) {
		s.middlewares = append(s.middlewares, mw...)
	}
}

func (s *Scheduler) Run(tasks []Task) {
	sem := make(chan struct{}, s.maxConcurrency)
	var wg sync.WaitGroup

	// 构建执行链（洋葱模型）
	//	核心业务逻辑是最后一环
	var finalHandler Handler = func(ctx context.Context, t Task) error {
		return t.Execute(ctx)
	}

	// 从后往前包装中间件
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		finalHandler = s.middlewares[i](finalHandler)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for _, t := range tasks {
		wg.Add(1)
		sem <- struct{}{}

		go func(task Task) {
			defer wg.Done()
			defer func() { <-sem }()

			traceID := fmt.Sprintf("trace-%d", rand.Intn(1000000))
			taskCtx := context.WithValue(ctx, "trace_id", traceID)

			err := finalHandler(taskCtx, t)

			if err != nil {
				atomic.AddInt64(&s.failCount, 1)
			} else {
				atomic.AddInt64(&s.successCount, 1)
			}
		}(t)
	}

	wg.Wait()
	fmt.Printf("\n--- 任务汇总: 成功 %d, 失败 %d ---\n", s.successCount, s.failCount)
}

var ErrFatal = errors.New("fatal error: do not retry") // 定义不可重试错误

type EmailTask struct {
	ID      int
	Content string
}

func (e *EmailTask) Name() string { return fmt.Sprintf("Email-%d", e.ID) }

func (e *EmailTask) Execute(ctx context.Context) error {
	// 模拟随机状况
	r := rand.Float32()
	select {
	case <-time.After(time.Duration(rand.Intn(200)) * time.Millisecond):
		if r < 0.1 {
			panic("数据库连接突然断开!") // 测试 Recovery 中间件
		}
		if r < 0.3 {
			return fmt.Errorf("网络抖动: %w", errors.New("timeout"))
		}
		if r < 0.4 {
			return fmt.Errorf("账户欠费: %w", ErrFatal) // 测试错误识别
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type RetriableTask struct {
	Task
	maxRetries int
	backoff    time.Duration
}

func (r *RetriableTask) Execute(ctx context.Context) error {
	var lastErr error
	for i := 0; i < r.maxRetries; i++ {
		err := r.Task.Execute(ctx)
		if err == nil {
			return nil
		}

		lastErr = err
		if errors.Is(err, ErrFatal) {
			fmt.Printf("[RETRY] 遇到致命错误，停止重试: %s\n", r.Name())
			break
		}

		if i < r.maxRetries-1 {
			fmt.Printf("[RETRY] 正在重试 %s (第%d次)... 原因: %v\n", r.Name(), i+1, err)
			select {
			case <-time.After(r.backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return lastErr
}

func main() {
	scheduler := NewScheduler(3,
		WithMiddleware(
			RecoveryMiddleware, // 最外层捕获 Panic
			LoggingMiddleware,  // 记录执行日志
		),
	)

	var tasks []Task
	for i := 1; i <= 6; i++ {
		rawTask := &EmailTask{ID: i}

		tasks = append(tasks, &RetriableTask{
			Task:       rawTask,
			maxRetries: 3,
			backoff:    100 * time.Millisecond,
		})
	}

	scheduler.Run(tasks)
}
