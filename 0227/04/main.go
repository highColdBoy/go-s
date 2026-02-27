package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	urls := []string{
		"https://www.google.com",
		"https://www.github.com",
		"https://baidu.com",
		"https://www.php.net",
		"https://golang.org",
	}

	taskChan := make(chan string, len(urls))
	resultChan := make(chan string)

	// 限制并发数为3
	for w := 1; w <= 3; w++ {
		go checker(ctx, w, taskChan, resultChan)
	}

	for _, url := range urls {
		taskChan <- url
	}

	firstResult := <-resultChan
	fmt.Printf(" [主程序] 已经抢到了第一个结果：%s，现在通知所有人下班！\n", firstResult)

	cancel()

	// 留点时间观察 Worker 的退出日志
	time.Sleep(1 * time.Second)
	fmt.Println(" [主程序] 退出")

}

func checker(ctx context.Context, id int, tasks <-chan string, results chan<- string) {
	for {
		select {
		case url := <-tasks:
			fmt.Printf(" Worker [%d] 开始请求：%s\n", id, url)

			req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Printf(" Worker [%d] 任务被取消或出错：%v\n", id, err)
				return
			}
			resp.Body.Close()
			//	模拟处理时间
			time.Sleep(500 * time.Millisecond)
			results <- url

		case <-ctx.Done():
			fmt.Printf(" Worker [%d] 收到退出指令，正在清理资源并下班...", id)
			return
		}
	}
}
