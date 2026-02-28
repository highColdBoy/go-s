package main

import (
	"fmt"
	"net/http"
	"time"
)

type Middlewares func(http.Handler) http.Handler

// Logger 日志记录器
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		fmt.Printf(" [>>>] 收到请求：%s %s\n", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
		fmt.Printf(" [<<<] 请求处理完成，耗时：%v\n", time.Since(start))
	})
}

func PanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if e := recover(); e != nil {
				fmt.Printf("[>>>] Panic recover %v\n", e)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("500 - Internal Server Error"))
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func Apply(handler http.HandlerFunc, middlewares ...Middlewares) http.Handler {
	f := middlewares[0]
	middlewares = middlewares[1:]

	res := f(handler)

	for _, m := range middlewares {
		res = m(res)
	}

	return res
}

func HelloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println(" [业务]正在生成 Hello 响应...")
	w.Write([]byte("Hello, Go Middleware!"))
}

func main() {
	handler := http.HandlerFunc(HelloHandler)

	finalHandler := Apply(handler, PanicRecovery, Logger)

	fmt.Println("服务启动在：8888...")
	http.ListenAndServe(":8888", finalHandler)
}
