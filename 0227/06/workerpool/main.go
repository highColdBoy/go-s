package main

import (
	"fmt"
	"time"
)

type Task func()

type Pool struct {
	taskQueue chan Task
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
	}
}

func (p *Pool) Submit(t Task) {
	p.taskQueue <- t
}

func main() {
	pool := NewPool(3)

	for i := 0; i < 10; i++ {
		taskID := i
		pool.Submit(func() {
			time.Sleep(1 * time.Second)
			fmt.Printf("任务  %d 处理完成\n", taskID)
		})
	}

	time.Sleep(5 * time.Second)
}
