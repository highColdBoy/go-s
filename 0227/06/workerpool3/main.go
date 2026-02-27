package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type TaskWithCtx func(ctx context.Context) error

type Pool struct {
	taskQueue chan TaskWithCtx
	wg        sync.WaitGroup
}

func NewPool(workerCount int) *Pool {
	p := &Pool{
		taskQueue: make(chan TaskWithCtx, 100),
	}

	for i := 0; i < workerCount; i++ {
		go p.worker(i)
	}

	return p
}

func (p *Pool) worker(id int) {
	for task := range p.taskQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

		fmt.Printf("工人 [%d] 开始执行任务...\n", id)

		err := task(ctx)
		if err != nil {
			fmt.Printf("工人 [%d] 任务执行失败：%v\n", id, err)
		} else {
			fmt.Printf("工人 [%d] 任务顺利完成\n", id)
		}

		cancel() // 必须调用，否则会造成 Context 泄漏
		p.wg.Done()
	}
}

func (p *Pool) Submit(t TaskWithCtx) {
	p.wg.Add(1)
	p.taskQueue <- t
}

func main() {
	pool := NewPool(3)

	// task1 正常任务耗时1s
	pool.Submit(func(ctx context.Context) error {
		time.Sleep(1 * time.Second)
		return nil
	})

	// task2 耗时5s 预期会超时
	pool.Submit(func(ctx context.Context) error {
		select {
		case <-time.After(5 * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	pool.wg.Wait()
}
