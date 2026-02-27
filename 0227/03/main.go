package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Task 定义抓取任务
type Task struct {
	URL string
}

// Result 定义抓取结果
type Result struct {
	URL     string
	Status  int
	Latency time.Duration
	Err     error
}

func main() {
	urls := []string{
		"https://www.google.com",
		"https://www.github.com",
		"https://baidu.com",
		"https://www.php.net",
		"https://golang.org",
	}

	taskChan := make(chan Task, len(urls))
	resultChan := make(chan Result, len(urls))

	// 限制并发数为3
	for w := 1; w <= 3; w++ {
		go checker(w, taskChan, resultChan)
	}

	for _, url := range urls {
		taskChan <- Task{URL: url}
	}
	close(taskChan)

	for i := 0; i < len(urls); i++ {
		res := <-resultChan
		// %-20s 左对齐 不足20填充
		if res.Err != nil {
			fmt.Printf("[失败] 站点：%-20s | 错误：%v\n", res.URL, res.Err)
		} else {
			fmt.Printf("[结果] 站点：%-20s | 状态码：%d | 耗时：%v\n", res.URL, res.Status, res.Latency)
		}
	}

}

func checker(id int, task <-chan Task, results chan<- Result) {
	for t := range task {
		start := time.Now()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

		// 创建一个带超时的请求
		req, _ := http.NewRequestWithContext(ctx, "GET", t.URL, nil)

		fmt.Printf("Worker [%d] 正在检查：%s \n", id, t.URL)

		resp, err := http.DefaultClient.Do(req)

		status := 0
		var finalErr error
		if err != nil {
			finalErr = err
		} else {
			status = resp.StatusCode
			resp.Body.Close()
		}

		cancel()

		results <- Result{
			URL:     t.URL,
			Status:  status,
			Latency: time.Since(start),
			Err:     finalErr,
		}
	}
}
