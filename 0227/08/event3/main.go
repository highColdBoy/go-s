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
	isClosed    bool // 标志位：防止关闭后继续发布
}

// Subscribe 订阅事件，返回一个用于接收事件的通道
func (eb *EventBus) Subscribe() chan *Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	ch := make(chan *Event, 10)

	eb.subscribers = append(eb.subscribers, ch)
	return ch
}

// Unsubscribe 取消订阅
func (eb *EventBus) Unsubscribe(ch chan *Event) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	for i, sub := range eb.subscribers {
		if sub == ch {
			eb.subscribers = append(eb.subscribers[:i], eb.subscribers[i+1:]...)
			close(ch)
			fmt.Println(" [系统]某个订阅者已安全退出")
			return
		}
	}
}

// Publish 发布事件，所有订阅者都能收到
func (eb *EventBus) Publish(data string) {
	eb.mu.RLock()
	// 这里在读锁内判断 是为了防止被别的进程篡改
	if eb.isClosed {
		eb.mu.RUnlock()
		return
	}

	subs := make([]chan *Event, len(eb.subscribers))
	copy(subs, eb.subscribers)
	eb.mu.RUnlock()

	e := eventPool.Get().(*Event)
	e.Data = data

	for _, ch := range subs {
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

func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.isClosed = true
	for _, ch := range eb.subscribers {
		close(ch)
	}
	eb.subscribers = nil
	fmt.Println(" [系统] EventBus 已彻底关闭")
}

func main() {
	bus := &EventBus{}

	// 模拟邮件服务
	subEmail := bus.Subscribe()
	go func() {
		for e := range subEmail {
			fmt.Printf(" [邮件服务] 处理: %s\n", e.Data)
			e.wg.Done()
		}
		fmt.Println(" [邮件服务] 协程已停止")
	}()

	// 1. 测试发布
	bus.Publish("第一次广播")

	// 2. 测试取消订阅
	time.Sleep(100 * time.Millisecond)
	bus.Unsubscribe(subEmail)

	// 3. 再次发布，邮件服务不应收到
	bus.Publish("第二次广播")

	// 4. 优雅关闭
	time.Sleep(100 * time.Millisecond)
	bus.Close()

	fmt.Println("主程序退出")
}
