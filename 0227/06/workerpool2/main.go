package main

import (
	"fmt"
	"sync"
	"time"
)

type Task func()

type Pool struct {
	taskQueue chan Task
	wg        sync.WaitGroup
	once      sync.Once
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
		fmt.Printf("工人 [%d] 开始干活...\n", id)
		task()
		p.wg.Done()
	}
}

func (p *Pool) Submit(t Task) {
	p.wg.Add(1)
	p.taskQueue <- t
}

func (p *Pool) Shutdown() {
	// 防止多次调用 Shutdown 重复关闭
	p.once.Do(func() {
		close(p.taskQueue)
	})

	p.wg.Wait()
	fmt.Println("所有任务已完成，池子优雅关闭")
}

func main() {
	pool := NewPool(3)

	for i := 0; i < 10; i++ {
		taskID := i
		pool.Submit(func() {
			time.Sleep(500 * time.Millisecond)
			fmt.Printf("任务  %d 处理完成\n", taskID)
		})
	}

	pool.Shutdown()
}
