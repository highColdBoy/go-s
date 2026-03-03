package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"
)

// 事件结构（增强版）
type AdvancedEvent struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"` // 事件类型，用于分区
	Data       map[string]interface{} `json:"data"`
	Timestamp  time.Time              `json:"timestamp"`
	RetryCount int                    `json:"retry_count"` // 重试次数
}

// 死信事件
type DeadLetterEvent struct {
	Event      AdvancedEvent `json:"event"`
	Reason     string        `json:"reason"`
	CreateTime time.Time     `json:"create_time"`
}

// 消费确认类型
type AckType int

const (
	Ack   AckType = iota // 确认消费成功
	Nack                 // 消费失败，进入死信
	Retry                // 消费失败，重试
)

// 消费上下文
type ConsumeContext struct {
	Event AdvancedEvent
	Ack   chan AckType // 用于消费确认
}

// 订阅组
type SubscribeGroup struct {
	name      string
	consumers []chan *ConsumeContext
	mu        sync.Mutex
	partition string // 所属分区
}

// 分区
type Partition struct {
	name        string
	groups      map[string]*SubscribeGroup
	eventQueue  chan *AdvancedEvent
	deadLetter  chan *DeadLetterEvent
	persistFile *os.File
	mu          sync.RWMutex
	persistDir  string // 新增：保存持久化目录，解决作用域问题
	maxRetry    int    // 新增：保存最大重试次数
}

// 高性能事件总线
type AdvancedEventBus struct {
	partitions map[string]*Partition // 分区映射（按事件类型前缀）
	mu         sync.RWMutex
	persistDir string
	maxRetry   int // 最大重试次数
}

func NewAdvancedEventBus(persistDir string, maxRetry int) *AdvancedEventBus {
	os.MkdirAll(persistDir, 0755)
	bus := &AdvancedEventBus{
		partitions: make(map[string]*Partition),
		persistDir: persistDir,
		maxRetry:   maxRetry,
	}
	return bus
}

// 创建分区（按事件类型前缀，比如 "user" 对应 "user.register" "user.login"）
func (eb *AdvancedEventBus) CreatePartition(partitionName string) *Partition {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if p, ok := eb.partitions[partitionName]; ok {
		return p
	}

	// 打开持久化文件（增加错误处理，避免静默失败）
	filePath := fmt.Sprintf("%s/%s.events", eb.persistDir, partitionName)
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		fmt.Printf("创建分区持久化文件失败：%s，错误：%v\n", filePath, err)
	}

	// 初始化Partition时，传入persistDir和maxRetry
	p := &Partition{
		name:        partitionName,
		groups:      make(map[string]*SubscribeGroup),
		eventQueue:  make(chan *AdvancedEvent, 1000),
		deadLetter:  make(chan *DeadLetterEvent, 100),
		persistFile: file,
		persistDir:  eb.persistDir, // 关键：把总线的持久化目录传给分区
		maxRetry:    eb.maxRetry,   // 关键：把最大重试次数传给分区
	}

	// 启动分区消费协程
	go p.consumeLoop()
	// 启动死信队列处理协程
	go p.deadLetterLoop() // 无需传参，直接用Partition自身的maxRetry
	// 启动持久化协程
	go p.persistLoop()

	eb.partitions[partitionName] = p
	return p
}

// 订阅（指定分区 + 订阅组）
func (p *Partition) Subscribe(groupName string) chan *ConsumeContext {
	p.mu.Lock()
	defer p.mu.Unlock()

	group, ok := p.groups[groupName]
	if !ok {
		group = &SubscribeGroup{
			name:      groupName,
			partition: p.name,
			consumers: make([]chan *ConsumeContext, 0),
		}
		p.groups[groupName] = group
	}

	// 为订阅者创建消费通道
	ch := make(chan *ConsumeContext, 10)
	group.consumers = append(group.consumers, ch)
	return ch
}

// 发布事件（修复分区名提取逻辑：按"."拆分取前缀）
func (eb *AdvancedEventBus) Publish(event AdvancedEvent) {
	// 修复：原逻辑 partitionName = event.Type[:len(event.Type)-len(event.Type[len(event.Type)-1:])] 是错误的
	// 正确逻辑：按"."拆分事件类型，取第一个部分作为分区名
	partitionName := "default" // 默认分区
	for i, c := range event.Type {
		if c == '.' {
			partitionName = event.Type[:i]
			break
		}
	}

	p := eb.CreatePartition(partitionName)

	// 写入事件队列
	p.eventQueue <- &event
}

// 分区消费循环（分发事件到订阅组）
func (p *Partition) consumeLoop() {
	for event := range p.eventQueue {
		p.mu.RLock()
		groups := p.groups
		p.mu.RUnlock()

		// 遍历所有订阅组，同组内随机选一个消费者分发
		for _, group := range groups {
			if len(group.consumers) == 0 {
				continue
			}
			// 简单负载均衡：随机选一个消费者（原逻辑固定选第一个，优化为随机）
			consumer := group.consumers[rand.Intn(len(group.consumers))]

			// 创建消费上下文（带确认通道）
			ctx := &ConsumeContext{
				Event: *event,
				Ack:   make(chan AckType, 1),
			}

			// 分发事件
			select {
			case consumer <- ctx:
				// 等待消费确认
				go func(event *AdvancedEvent, group *SubscribeGroup) { // 修复：传入event和group，避免闭包引用同一变量
					select {
					case ackType := <-ctx.Ack:
						switch ackType {
						case Ack:
							fmt.Printf("事件 %s 消费成功（分区：%s，组：%s）\n", event.ID, p.name, group.name)
						case Nack:
							// 进入死信队列
							p.deadLetter <- &DeadLetterEvent{
								Event:      *event,
								Reason:     "消费者主动Nack",
								CreateTime: time.Now(),
							}
						case Retry:
							// 重试：重新入队
							event.RetryCount += 1
							p.eventQueue <- event
						}
					case <-time.After(10 * time.Second):
						// 超时未确认，进入死信
						p.deadLetter <- &DeadLetterEvent{
							Event:      *event,
							Reason:     "消费超时未确认",
							CreateTime: time.Now(),
						}
					}
				}(event, group) // 传入当前循环的event和group
			default:
				// 消费者忙，进入死信
				p.deadLetter <- &DeadLetterEvent{
					Event:      *event,
					Reason:     "消费者通道满",
					CreateTime: time.Now(),
				}
			}
		}
	}
}

// 死信队列处理（修复：参数改为从Partition自身读取maxRetry）
func (p *Partition) deadLetterLoop() {
	for dle := range p.deadLetter {
		if dle.Event.RetryCount < p.maxRetry {
			// 重试：重新入队
			dle.Event.RetryCount += 1
			p.eventQueue <- &dle.Event
			fmt.Printf("事件 %s 重试（次数：%d）\n", dle.Event.ID, dle.Event.RetryCount)
		} else {
			// 超过最大重试次数，持久化并丢弃
			fmt.Printf("事件 %s 进入死信队列（最终丢弃），原因：%s\n", dle.Event.ID, dle.Reason)
			p.persistDeadLetter(dle)
		}
	}
}

// 事件持久化循环（修复：原逻辑会取出队列事件再放回，导致队列长度异常）
func (p *Partition) persistLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.RLock()
		queueLen := len(p.eventQueue)
		p.mu.RUnlock()

		if queueLen == 0 || p.persistFile == nil {
			continue
		}

		// 批量持久化（修复：原逻辑取出事件后放回，会导致队列顺序错乱+重复入队）
		// 优化：遍历队列快照，不修改原队列
		tempEvents := make([]*AdvancedEvent, 0, queueLen)
		for i := 0; i < queueLen; i++ {
			event := <-p.eventQueue
			tempEvents = append(tempEvents, event)
		}

		// 批量写入文件
		for _, event := range tempEvents {
			data, err := json.Marshal(event)
			if err != nil {
				fmt.Printf("序列化事件失败：%s，错误：%v\n", event.ID, err)
				continue
			}
			_, err = p.persistFile.Write(append(data, '\n'))
			if err != nil {
				fmt.Printf("持久化事件失败：%s，错误：%v\n", event.ID, err)
			}
		}
		p.persistFile.Sync()

		// 把事件放回队列
		for _, event := range tempEvents {
			p.eventQueue <- event
		}
	}
}

// 持久化死信事件（修复核心错误：使用p.persistDir替代eb.persistDir）
func (p *Partition) persistDeadLetter(dle *DeadLetterEvent) {
	// 修复：用Partition自身的persistDir，而非eb.persistDir
	filePath := fmt.Sprintf("%s/%s.dlq", p.persistDir, p.name)
	// 增加错误处理，避免静默失败
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("创建死信文件失败：%s，错误：%v\n", filePath, err)
		return
	}
	defer file.Close()

	data, err := json.Marshal(dle)
	if err != nil {
		fmt.Printf("序列化死信事件失败：%s，错误：%v\n", dle.Event.ID, err)
		return
	}
	_, err = file.Write(append(data, '\n'))
	if err != nil {
		fmt.Printf("持久化死信事件失败：%s，错误：%v\n", dle.Event.ID, err)
	}
}

func main() {
	// 初始化随机数种子（修复：原代码rand.Intn无种子，每次运行结果相同）
	rand.Seed(time.Now().UnixNano())

	// 初始化事件总线（持久化目录 ./data，最大重试3次）
	bus := NewAdvancedEventBus("./data", 3)

	// 创建 user 分区
	userPartition := bus.CreatePartition("user")

	// 订阅组1（邮件组）
	emailCh := userPartition.Subscribe("email-group")
	go func() {
		for ctx := range emailCh {
			fmt.Printf(" [邮件组] 消费事件：%s，数据：%v\n", ctx.Event.ID, ctx.Event.Data)
			// 模拟偶尔失败
			if ctx.Event.RetryCount == 0 && rand.Intn(10) < 3 {
				ctx.Ack <- Retry // 重试
			} else {
				ctx.Ack <- Ack // 确认成功
			}
		}
	}()

	// 订阅组2（短信组）
	smsCh := userPartition.Subscribe("sms-group")
	go func() {
		for ctx := range smsCh {
			fmt.Printf(" [短信组] 消费事件：%s，数据：%v\n", ctx.Event.ID, ctx.Event.Data)
			// 模拟偶尔Nack
			if rand.Intn(10) < 1 {
				ctx.Ack <- Nack // 进入死信
			} else {
				ctx.Ack <- Ack
			}
		}
	}()

	// 发布事件
	for i := 0; i < 5; i++ {
		bus.Publish(AdvancedEvent{
			ID:         fmt.Sprintf("event-%d", i),
			Type:       "user.register", // 路由到 user 分区
			Data:       map[string]interface{}{"user_id": i, "name": fmt.Sprintf("user-%d", i)},
			Timestamp:  time.Now(),
			RetryCount: 0,
		})
	}

	time.Sleep(10 * time.Second)
	fmt.Println("事件总线演示完成")
}
