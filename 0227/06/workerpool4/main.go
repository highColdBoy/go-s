package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type TaskWithCtx func(ctx context.Context) error

type Task struct {
	t TaskWithCtx
	d time.Duration
}

type Pool struct {
	taskQueue chan Task
	wg        sync.WaitGroup
}

func NewPool(workerCount int) *Pool {
	p := &Pool{
		taskQueue: make(chan Task, 100),
	}

	for i := 0; i < workerCount; i++ {
		go p.worker(i)
	}

	return p
}

func (p *Pool) worker(id int) {
	for task := range p.taskQueue {
		maxRetries := 2
		var err error

		for i := 0; i <= maxRetries; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), task.d)

			if i > 0 {
				fmt.Printf("工人 [%d] 正在进行第 %d 次重试...\n", id, i)
			}

			err = task.t(ctx)
			cancel()

			if err == nil || !errors.Is(err, context.DeadlineExceeded) {
				break
			}
		}

		if err != nil {
			fmt.Printf("工人 [%d] 任务最终失败：%v\n", id, err)
		} else {
			fmt.Printf("工人 [%d] 任务顺利完成\n", id)
		}

		p.wg.Done()
	}
}

func (p *Pool) Submit(t TaskWithCtx, d time.Duration) {
	p.wg.Add(1)
	p.taskQueue <- Task{t, d}
}

func main() {
	pool := NewPool(3)

	// task1 正常任务耗时1s
	pool.Submit(func(ctx context.Context) error {
		time.Sleep(1 * time.Second)
		return nil
	}, 2*time.Second)

	// task2 耗时5s 预期会超时
	pool.Submit(func(ctx context.Context) error {
		select {
		case <-time.After(5 * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, 1*time.Second)

	pool.wg.Wait()
}
