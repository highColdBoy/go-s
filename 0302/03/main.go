package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
)

type Middlewares func(http.Handler) http.Handler

// 全局 tracer
var tracer trace.Tracer

// 初始化链路追踪
func initTracer() {
	exporter, _ := stdouttrace.New(stdouttrace.WithPrettyPrint())
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			"go-s-demo",
			attribute.String("service.name", "advanced-http"),
		)),
	)
	otel.SetTracerProvider(tp)
	tracer = tp.Tracer("advanced-http-tracer")
}

// Tracing 链路追踪中间件
func Tracing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), r.URL.Path)
		defer span.End()

		// 将 TraceID/SpanID 写入响应头
		traceID := span.SpanContext().TraceID().String()
		spanID := span.SpanContext().SpanID().String()
		w.Header().Set("X-Trace-ID", traceID)
		w.Header().Set("X-Span-ID", spanID)

		// 将 ctx 传递给下一个中间件
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RateLimit 限流中间件（漏桶算法）
func RateLimit(limiter *rate.Limiter) Middlewares {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte("429 - Too Many Requests"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CircuitBreaker 熔断器中间件
type CircuitBreaker struct {
	failCount    atomic.Int32
	successCount atomic.Int32
	open         atomic.Bool
	openUntil    atomic.Value // time.Time
}

func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{}
}

func (cb *CircuitBreaker) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 熔断器打开中，且未到恢复时间 -> 直接返回 503
		if cb.open.Load() {
			openUntil, _ := cb.openUntil.Load().(time.Time)
			if time.Now().Before(openUntil) {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("503 - Service Unavailable (Circuit Open)"))
				return
			}
			// 恢复时间到，关闭熔断器
			cb.open.Store(false)
			fmt.Println("熔断器已关闭")
		}

		// 执行下一个中间件，并捕获错误
		rec := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		// 统计失败 成功数
		if rec.statusCode >= 500 {
			cb.failCount.Add(1)
		} else {
			cb.successCount.Add(1)
		}

		// 失败率超50% -> 打开熔断器（5秒）
		total := cb.failCount.Load() + cb.successCount.Load()
		if total > 10 && float64(cb.failCount.Load())/float64(total) > 0.5 {
			cb.open.Store(true)
			cb.openUntil.Store(time.Now().Add(5 * time.Second))
			fmt.Println("熔断器已打开（5秒）")
			// 重置统计
			cb.failCount.Store(0)
			cb.successCount.Store(0)
		}
	})
}

// responseRecorder 响应记录器（用于捕获状态码）
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// Metrics 指标采集中间件
type Metrics struct {
	reqCount    atomic.Int64
	reqDuration atomic.Int64
	errCount    atomic.Int64
}

func NewMetrics() *Metrics {
	m := &Metrics{}
	// 定时输出指标
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			fmt.Printf("\n=== HTTP 指标 ===\n")
			fmt.Printf("总请求数: %d | 错误数: %d | 平均耗时: %dms\n",
				m.reqCount.Load(), m.errCount.Load(),
				m.reqDuration.Load()/max(m.reqCount.Load(), 1))
		}
	}()

	return m
}

func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		// 采集指标
		m.reqCount.Add(1)
		m.reqDuration.Add(int64(time.Since(start).Milliseconds()))
		if rec.statusCode >= 500 {
			m.errCount.Add(1)
		}
	})
}

func ApplyAdvanced(handler http.HandlerFunc, middlewares ...Middlewares) http.Handler {
	var h http.Handler = handler
	// 逆序应用（保证第一个中间件最先执行）
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// 业务 Handler（模拟偶尔报错）
func HelloHandler(w http.ResponseWriter, r *http.Request) {
	if rand.Intn(10) < 3 { // 30% 概率返回500
		panic("模拟业务panic")
		// w.WriteHeader(http.StatusInternalServerError)
		// w.Write([]byte("500 - Internal Server Error"))
		// return
	}
	w.Write([]byte("Hello, Advanced Middleware!"))
}

func main() {
	initTracer()

	// 初始化组件
	limiter := rate.NewLimiter(rate.Limit(5), 10) // 每秒5个请求，桶容量10
	circuitBreaker := NewCircuitBreaker()
	metrics := NewMetrics()

	// 构建中间件链：PanicRecovery → Tracing → RateLimit → CircuitBreaker → Metrics
	handler := ApplyAdvanced(HelloHandler,
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					if e := recover(); e != nil {
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte("500 - Internal Server Error"))
					}
				}()
				next.ServeHTTP(w, r)
			})
		},
		Tracing,
		RateLimit(limiter),
		circuitBreaker.Middleware,
		metrics.Middleware,
	)

	fmt.Println("服务启动在：8888...")
	http.ListenAndServe(":8888", handler)
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
