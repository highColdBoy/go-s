package main

import (
	"fmt"
	"sync"
	"time"
)

type Config struct {
	Addr string
	Port int
}

type ConfigManager struct {
	mu     sync.RWMutex
	config *Config
}

func (cm *ConfigManager) Get() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	// 这里返回的是指针 存在被篡改的可能
	return cm.config
}

func (cm *ConfigManager) Update(config *Config) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.config = config
}

func main() {
	cm := &ConfigManager{
		config: &Config{Addr: "localhost", Port: 8080},
	}

	// 模拟多个业务协程一直在读取配置
	for i := 0; i < 3; i++ {
		go func(id int) {
			for {
				cfg := cm.Get()
				fmt.Printf("协程 %d 读取配置: %v\n", id, cfg)
				time.Sleep(1 * time.Second)
			}
		}(i)
	}

	// 模拟后台动态更新配置
	time.Sleep(2 * time.Second)
	fmt.Println("--- 管理员修改了配置 ---")
	cm.Update(&Config{Addr: "127.0.0.1", Port: 9999})

	select {} // 阻塞
}
