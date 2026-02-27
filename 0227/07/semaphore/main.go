package main

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type DBPool struct {
	sem         chan struct{}
	activeConns atomic.Int32
}

func NewDBPool(maxConns int) *DBPool {
	p := &DBPool{
		sem: make(chan struct{}, maxConns),
	}

	go p.watch()

	return p
}

func (p *DBPool) watch() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Printf("pool work size: %d\n", p.activeConns.Load())
		}
	}
}

func (db *DBPool) Execute(queryID int) error {
	select {
	case db.sem <- struct{}{}:
		defer func() { <-db.sem }()
	case <-time.After(3 * time.Second):
		return errors.New("timeout")
	}

	db.activeConns.Add(1)
	defer db.activeConns.Add(-1)

	fmt.Printf("[DB] 任务 %d 正在查询...\n", queryID)
	time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)
	fmt.Printf("[DB] 任务 %d 查询结束\n", queryID)

	return nil
}

func main() {
	pool := NewDBPool(5)
	var wg sync.WaitGroup

	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = pool.Execute(id)
		}(i)
	}

	wg.Wait()
	fmt.Println("所有数据库操作完成")
}
