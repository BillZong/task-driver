package metrics

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func cleanup(t *testing.T) {
	// Reset the package-level registry for clean state between tests
	reg = nil
	regOnce = sync.Once{}
}

func TestRegistry_initialization(t *testing.T) {
	cleanup(t)
	r := Registry()
	if r == nil {
		t.Fatal("Registry() returned nil")
	}
	// Should be idempotent
	r2 := Registry()
	if r2 != r {
		t.Error("Registry() not idempotent")
	}
}

func TestReportToken(t *testing.T) {
	cleanup(t)
	_ = Registry() // ensure reg exists

	report := &TokenReport{
		TaskID:          "test-task-1",
		StepIndex:       0,
		AgentID:         "hermes",
		InputTokens:     50000,
		OutputTokens:    8000,
		CacheReadTokens: 10000,
		ReasoningTokens: 2000,
	}
	ReportToken(report)

	// Use a Gatherer to read back the metrics
	gatherer := Registry()
	mfs, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	var foundInput, foundOutput bool
	for _, mf := range mfs {
		name := mf.GetName()
		if name == "task_token_input_total" {
			for _, m := range mf.GetMetric() {
				labels := m.GetLabel()
				for _, l := range labels {
					if l.GetName() == "task_id" && l.GetValue() == "test-task-1" {
						if m.GetCounter().GetValue() != 50000 {
							t.Errorf("input_total = %v, want 50000", m.GetCounter().GetValue())
						}
						foundInput = true
					}
				}
			}
		}
		if name == "task_token_output_total" {
			for _, m := range mf.GetMetric() {
				labels := m.GetLabel()
				for _, l := range labels {
					if l.GetName() == "task_id" && l.GetValue() == "test-task-1" {
						if m.GetCounter().GetValue() != 8000 {
							t.Errorf("output_total = %v, want 8000", m.GetCounter().GetValue())
						}
						foundOutput = true
					}
				}
			}
		}
	}
	if !foundInput {
		t.Error("task_token_input_total not found in gathered metrics")
	}
	if !foundOutput {
		t.Error("task_token_output_total not found in gathered metrics")
	}
}

func TestReportToken_zeroFields(t *testing.T) {
	cleanup(t)
	_ = Registry()

	// Only input/output, no cache/reasoning (should not cause errors)
	report := &TokenReport{
		TaskID:      "test-task-2",
		StepIndex:   1,
		AgentID:     "claude",
		InputTokens: 1000,
		OutputTokens: 200,
	}
	ReportToken(report)
	// No panic = pass
}

func TestReportLatency(t *testing.T) {
	cleanup(t)
	_ = Registry()

	report := &LatencyReport{
		TaskID:    "test-latency-task",
		StepIndex: 0,
		Status:    "completed",
		Duration:  30.5,
	}
	ReportLatency(report)
	// No panic = pass
}

func TestTokenReportHandler(t *testing.T) {
	cleanup(t)
	_ = Registry()

	body := `{"task_id":"task-1","step_index":0,"agent_id":"hermes","input_tokens":15000,"output_tokens":3000}`
	req := httptest.NewRequest(http.MethodPost, "/metrics/tokens", io.NopCloser(bytes.NewBufferString(body)))
	w := httptest.NewRecorder()
	TokenReportHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
}

func TestTokenReportHandler_missingFields(t *testing.T) {
	cleanup(t)
	_ = Registry()

	tests := []struct {
		name string
		body string
		want int
	}{
		{"missing task_id", `{"step_index":0,"agent_id":"hermes"}`, http.StatusBadRequest},
		{"missing agent_id", `{"task_id":"t1","step_index":0}`, http.StatusBadRequest},
		{"empty object", `{}`, http.StatusBadRequest},
		{"valid", `{"task_id":"t1","step_index":0,"agent_id":"h"}`, http.StatusAccepted},
		{"invalid json", `not-json`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/metrics/tokens", io.NopCloser(bytes.NewBufferString(tt.body)))
			w := httptest.NewRecorder()
			TokenReportHandler(w, req)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d. body=%s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestLatencyReportHandler(t *testing.T) {
	cleanup(t)
	_ = Registry()

	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", `{"task_id":"t1","step_index":0,"status":"completed","duration_seconds":10.5}`, http.StatusAccepted},
		{"missing task_id", `{"step_index":0,"duration_seconds":5}`, http.StatusBadRequest},
		{"zero duration", `{"task_id":"t1","step_index":0,"duration_seconds":0}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/metrics/latency", io.NopCloser(bytes.NewBufferString(tt.body)))
			w := httptest.NewRecorder()
			LatencyReportHandler(w, req)
			if w.Code != tt.want {
				t.Errorf("status = %d, want %d", w.Code, tt.want)
			}
		})
	}
}

func TestHandler_returnsPrometheus(t *testing.T) {
	cleanup(t)
	_ = Registry()

	// Report a token so there's at least some custom metric
	ReportToken(&TokenReport{
		TaskID: "prom-test", StepIndex: 0, AgentID: "h",
		InputTokens: 1, OutputTokens: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(string(body), "task_token_input_total") {
		t.Error("/metrics response missing task_token_input_total")
	}
	if !strings.Contains(string(body), "prom-test") {
		t.Error("/metrics response missing task_id label")
	}
}

func TestFormatStep(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "step_0"},
		{1, "step_1"},
		{42, "step_42"},
	}
	for _, tt := range tests {
		if got := formatStep(tt.input); got != tt.want {
			t.Errorf("formatStep(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{1234567890, "1234567890"},
	}
	for _, tt := range tests {
		if got := itoa(tt.input); got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
