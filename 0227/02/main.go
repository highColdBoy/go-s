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

	// 开启一个限时 3s 的 “总闹钟”
	totalTimeout := time.After(3 * time.Second)
	var finalResults []Result

CollectLoop: // 标签，用于直接跳出 for 循环
	for i := 0; i < len(urls); i++ {
		select {
		case res := <-resultChan:
			finalResults = append(finalResults, res)
			fmt.Printf("收到任务结果:%s\n", res.URL)
		case <-totalTimeout:
			fmt.Println("\n[警告] 总时间已到，部分请求由于过慢被放弃...")
			break CollectLoop
		}
	}

	fmt.Printf("\n--- 最终汇总（共收到 %d 个结果）---\n", len(finalResults))
	for _, r := range finalResults {
		statusStr := "成功"
		if r.Err != nil {
			statusStr = "失败"
		}
		fmt.Printf("[%s] %s (耗时 %v)\n", r.URL, statusStr, r.Latency)
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
