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

type EmailTask struct{ ID int }

func (e *EmailTask) Name() string { return fmt.Sprintf("EmailTask-%d", e.ID) }

func (e *EmailTask) Execute(ctx context.Context) error {
	traceID, ok := ctx.Value("trace_id").(string)
	if !ok {
		traceID = "unknown"
	}

	//	模拟耗时操作
	select {
	case <-time.After(time.Duration(rand.Intn(500)) * time.Millisecond):
		fmt.Printf("[LOG] [%s] 正在连接 SMTP 服务器...\n", traceID)
		if rand.Float32() < 0.2 { // 20% 概率失败
			return errors.New("smtp connection timeout")
		}
		return nil
	case <-ctx.Done(): // 响应超时或取消
		return fmt.Errorf("[%s] 任务超时取消: %v", traceID, ctx.Err())
	}
}

type Scheduler struct {
	maxConcurrency int
	successCount   int64
	failCount      int64
}

func NewScheduler(limit int) *Scheduler {
	return &Scheduler{maxConcurrency: limit}
}

func (s *Scheduler) Run(task []Task) {
	sem := make(chan struct{}, s.maxConcurrency)
	var wg sync.WaitGroup

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	for _, t := range task {
		wg.Add(1)
		sem <- struct{}{}

		go func(task Task) {
			defer wg.Done()
			defer func() { <-sem }()

			traceID := fmt.Sprintf("trace-%d", rand.Intn(1000000))
			taskCtx := context.WithValue(ctx, "trace_id", traceID)

			fmt.Printf("[START] %s | Trace: %s\n", task.Name(), traceID)

			err := task.Execute(taskCtx)

			if err != nil {
				atomic.AddInt64(&s.failCount, 1)
				fmt.Printf("[ERROR] %s | %v\n", task.Name(), err)
			} else {
				atomic.AddInt64(&s.successCount, 1)
				fmt.Printf("[DONE]  %s\n", task.Name())
			}
		}(t)
	}

	wg.Wait()
	fmt.Printf("\n--- 任务汇总: 成功 %d, 失败 %d ---\n", s.successCount, s.failCount)
}

// 添加带重试机制的任务

// RetriableTask 重试装饰器
type RetriableTask struct {
	Task
	maxRetries int
	backoff    time.Duration
	retries    atomic.Int32
}

func (r *RetriableTask) Execute(ctx context.Context) error {
	var lastErr error
	for i := 0; i < r.maxRetries; i++ {
		if i > 0 {
			fmt.Printf("[RETRY] 正在进行第 %d 次重试: %s\n", i, r.Name())
			r.retries.Add(1)
			// 策略：等待一段时间再重试，或者监听 ctx 取消
			select {
			case <-time.After(r.backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := r.Task.Execute(ctx)
		if err == nil {
			return nil
		}

		lastErr = err
		if errors.Is(err, ctx.Err()) {
			break
		}
	}

	return fmt.Errorf("超过最大重试次数，最后错误: %w", lastErr)
}

// WithRetry 包装器函数
func WithRetry(t Task, max int, delay time.Duration) Task {
	return &RetriableTask{
		Task:       t,
		maxRetries: max,
		backoff:    delay,
	}
}

func main() {
	scheduler := NewScheduler(3)
	var tasks []Task
	for i := 1; i <= 5; i++ {
		rawTask := &EmailTask{ID: i}
		tasks = append(tasks, WithRetry(rawTask, 3, 100*time.Millisecond))
	}

	scheduler.Run(tasks)
}
