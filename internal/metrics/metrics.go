package metrics

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ── 包级注册表 ──

var (
	reg     *prometheus.Registry
	regOnce sync.Once

	// tokenInputTotal 记录每个 task+step 的输入 token 累积量。
	tokenInputTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "task_token_input_total",
		Help: "Total input tokens consumed, by task and step.",
	}, []string{"task_id", "step_index", "agent_id"})

	// tokenOutputTotal 记录每个 task+step 的输出 token 累积量。
	tokenOutputTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "task_token_output_total",
		Help: "Total output tokens produced, by task and step.",
	}, []string{"task_id", "step_index", "agent_id"})

	// tokenCacheReadTotal 记录缓存命中的 token 读取量。
	tokenCacheReadTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "task_token_cache_read_total",
		Help: "Total cache read tokens, by task and step.",
	}, []string{"task_id", "step_index", "agent_id"})

	// tokenReasoningTotal 记录推理 token 量（如 DeepSeek R1）。
	tokenReasoningTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "task_token_reasoning_total",
		Help: "Total reasoning tokens, by task and step.",
	}, []string{"task_id", "step_index", "agent_id"})

	// tokenInputBucket 是输入 token 的直方图，用于观察分布。
	tokenInputBucket = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "task_token_input_bucket",
		Help:    "Input token distribution per reporting event.",
		Buckets: prometheus.ExponentialBuckets(100, 4, 8), // 100 → 409600
	}, []string{"task_id", "step_index"})

	// tokenOutputBucket 是输出 token 的直方图。
	tokenOutputBucket = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "task_token_output_bucket",
		Help:    "Output token distribution per reporting event.",
		Buckets: prometheus.ExponentialBuckets(50, 4, 8), // 50 → 204800
	}, []string{"task_id", "step_index"})

	// taskStepLatencySeconds 记录步骤执行耗时（从 start 到完成）。
	taskStepLatencySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "task_step_latency_seconds",
		Help:    "Step execution duration in seconds.",
		Buckets: prometheus.ExponentialBuckets(1, 4, 10), // 1s → 262144s
	}, []string{"task_id", "step_index", "status"})
)

// TokenReport 是 POST /metrics/tokens 接收的请求体。
type TokenReport struct {
	TaskID          string `json:"task_id"`
	StepIndex       int    `json:"step_index"`
	AgentID         string `json:"agent_id"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	CacheReadTokens int64  `json:"cache_read_tokens,omitempty"`
	ReasoningTokens int64  `json:"reasoning_tokens,omitempty"`
}

// LatencyReport 是 POST /metrics/latency 接收的请求体。
type LatencyReport struct {
	TaskID    string  `json:"task_id"`
	StepIndex int     `json:"step_index"`
	Status    string  `json:"status"` // completed / failed / skipped
	Duration  float64 `json:"duration_seconds"`
}

// ── 注册表 ──

// Registry 返回 Prometheus 注册表实例（懒初始化）。
func Registry() *prometheus.Registry {
	regOnce.Do(func() {
		reg = prometheus.NewRegistry()
		// 注册默认的 Go/process 收集器
		reg.MustRegister(prometheus.NewGoCollector())
		reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

		// 注册自定义指标
		reg.MustRegister(tokenInputTotal)
		reg.MustRegister(tokenOutputTotal)
		reg.MustRegister(tokenCacheReadTotal)
		reg.MustRegister(tokenReasoningTotal)
		reg.MustRegister(tokenInputBucket)
		reg.MustRegister(tokenOutputBucket)
		reg.MustRegister(taskStepLatencySeconds)
	})
	return reg
}

// Handler 返回 /metrics HTTP handler。
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry(), promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// ── 上报入口 ──

// ReportToken 解析并记录一条 token 报告。
// 返回错误时由调用方决定是否忽略（指标写入不应影响主流程）。
func ReportToken(r *TokenReport) {
	stepKey := formatStep(r.StepIndex)

	tokenInputTotal.WithLabelValues(r.TaskID, stepKey, r.AgentID).Add(float64(r.InputTokens))
	tokenOutputTotal.WithLabelValues(r.TaskID, stepKey, r.AgentID).Add(float64(r.OutputTokens))

	if r.CacheReadTokens > 0 {
		tokenCacheReadTotal.WithLabelValues(r.TaskID, stepKey, r.AgentID).Add(float64(r.CacheReadTokens))
	}
	if r.ReasoningTokens > 0 {
		tokenReasoningTotal.WithLabelValues(r.TaskID, stepKey, r.AgentID).Add(float64(r.ReasoningTokens))
	}

	// Histogram 记录单次上报的 token 量分布
	tokenInputBucket.WithLabelValues(r.TaskID, stepKey).Observe(float64(r.InputTokens))
	tokenOutputBucket.WithLabelValues(r.TaskID, stepKey).Observe(float64(r.OutputTokens))
}

// ReportLatency 解析并记录一条步骤耗时报告。
func ReportLatency(r *LatencyReport) {
	stepKey := formatStep(r.StepIndex)
	taskStepLatencySeconds.WithLabelValues(r.TaskID, stepKey, r.Status).Observe(r.Duration)
}

// ── HTTP handler ──

// TokenReportHandler 处理 POST /metrics/tokens。
func TokenReportHandler(w http.ResponseWriter, r *http.Request) {
	var report TokenReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if report.TaskID == "" || report.AgentID == "" {
		http.Error(w, "task_id and agent_id are required", http.StatusBadRequest)
		return
	}

	ReportToken(&report)
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"accepted"}`))
}

// LatencyReportHandler 处理 POST /metrics/latency。
func LatencyReportHandler(w http.ResponseWriter, r *http.Request) {
	var report LatencyReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if report.TaskID == "" || report.Duration <= 0 {
		http.Error(w, "task_id and positive duration_seconds are required", http.StatusBadRequest)
		return
	}

	ReportLatency(&report)
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"accepted"}`))
}

// ── helpers ──

func formatStep(idx int) string {
	return "step_" + itoa(idx)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
