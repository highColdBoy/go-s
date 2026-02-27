package main

import (
	"fmt"
	"sync"
	"time"
)

// Event 事件结构
type Event struct {
	Data string
	wg   sync.WaitGroup
}

var eventPool = sync.Pool{
	New: func() any {
		fmt.Println(" [池子] 创建了全新的 Event 对象")
		return &Event{}
	},
}

// EventBus 事件总线
type EventBus struct {
	mu          sync.RWMutex
	subscribers []chan *Event
}

// Subscribe 订阅事件，返回一个用于接收事件的通道
func (eb *EventBus) Subscribe() chan *Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan *Event, 10)

	eb.subscribers = append(eb.subscribers, ch)
	return ch
}

// Publish 发布事件，所有订阅者都能收到
func (eb *EventBus) Publish(data string) {
	e := eventPool.Get().(*Event)
	e.Data = data

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	for _, ch := range eb.subscribers {
		e.wg.Add(1)
		select {
		case ch <- e:

		/*
			底层原理：time.After 每次调用都会创建一个新的 Timer 对象，并启动一个临时的内部协程。
			潜在风险：如果你有 1000 个订阅者，每发布一条消息，就会瞬间创建 1000 个计时器。如果消息频率很高，内存和 CPU 调度开销会激增
		*/
		//case <-time.After(1 * time.Second):
		//	continue
		//}
		default:
			e.wg.Done()
			fmt.Println("订阅者忙，已跳过")
		}
	}

	go func(ev *Event) {
		ev.wg.Wait()      // 等待所有订阅者 Done
		ev.Data = ""      // 清理数据
		eventPool.Put(ev) // 放回池子
		fmt.Println(" [池子] 资源已安全回收")
	}(e)
}

func main() {
	bus := &EventBus{}

	// 订阅者： 发邮件
	sub1 := bus.Subscribe()
	go func() {
		for e := range sub1 {
			e.wg.Done()
			fmt.Printf(" [邮件服务] 收到事件: %s\n", e.Data)
		}
	}()

	// 订阅者： 发短信
	sub2 := bus.Subscribe()
	go func() {
		for e := range sub2 {
			e.wg.Done()
			fmt.Printf(" [短信服务] 收到事件: %s\n", e.Data)
		}
	}()

	bus.Publish("用户 007 注册成功")

	time.Sleep(1 * time.Second)
}
