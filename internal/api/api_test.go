package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/BillZong/task-driver/internal/config"
	"github.com/BillZong/task-driver/internal/db"
)

func setupTest(t *testing.T) (*config.Config, *sql.DB) {
	t.Helper()
	f, err := os.CreateTemp("", "task-driver-api-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	os.Remove(f.Name())
	t.Cleanup(func() { os.Remove(f.Name()) })

	database, err := db.InitDB(f.Name())
	if err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		Listen: ":9876",
		DB:     f.Name(),
		Agents: map[string]config.AgentConfig{
			"hermes": {
				ResumeCmd:   "hermes run --resume {session_id}",
				Description: "Hermes Agent",
			},
		},
	}

	return cfg, database
}

func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func req(t *testing.T, handler http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, body)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestHealthHandler(t *testing.T) {
	cfg, database := setupTest(t)
	h := HealthHandler(database, cfg)
	w := req(t, h, "GET", "/health", nil)

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["alive"] != true {
		t.Errorf("alive = %v, want true", resp["alive"])
	}
	// Health with compression config
	cfg.Compression = &config.CompressionConfig{Threshold: 0.50, ContextWindow: 1048576}
	w = req(t, h, "GET", "/health", nil)
	json.NewDecoder(w.Body).Decode(&resp)
	comp, ok := resp["compression"].(map[string]any)
	if !ok {
		t.Fatal("compression field missing")
	}
	if comp["threshold_tokens"] != 524288.0 {
		t.Errorf("threshold_tokens = %v, want 524288", comp["threshold_tokens"])
	}
}

func TestCreateAndGetTask(t *testing.T) {
	cfg, database := setupTest(t)
	router := NewRouter(database, cfg)

	w := req(t, router, "POST", "/task/create", jsonBody(t, map[string]any{
		"goal": "test task", "plan": []string{"step1", "step2"},
		"agent_id": "hermes", "agent_session_id": "session-1",
	}))
	var createResp map[string]any
	json.NewDecoder(w.Body).Decode(&createResp)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201. resp=%v", w.Code, createResp)
	}
	taskID := createResp["task_id"].(string)
	if taskID == "" {
		t.Fatal("task_id is empty")
	}

	w = req(t, router, "GET", "/task/"+taskID, nil)
	var getResp map[string]any
	json.NewDecoder(w.Body).Decode(&getResp)
	if getResp["task"] == nil {
		t.Error("task field missing in get response")
	}
}

func TestCreateTask_validation(t *testing.T) {
	cfg, database := setupTest(t)
	router := NewRouter(database, cfg)

	tests := []struct {
		name string
		body any
		code int
	}{
		{"missing goal", map[string]any{"plan": []string{"s1"}, "agent_id": "h"}, http.StatusBadRequest},
		{"missing agent_id", map[string]any{"goal": "g", "plan": []string{"s1"}}, http.StatusBadRequest},
		{"valid", map[string]any{"goal": "g", "plan": []string{"s1"}, "agent_id": "h", "agent_session_id": "s1"}, http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := req(t, router, "POST", "/task/create", jsonBody(t, tt.body))
			if w.Code != tt.code {
				t.Errorf("status = %d, want %d. body=%s", w.Code, tt.code, w.Body.String())
			}
		})
	}
}

func TestTaskLifecycleHandlers(t *testing.T) {
	cfg, database := setupTest(t)
	router := NewRouter(database, cfg)

	w := req(t, router, "POST", "/task/create", jsonBody(t, map[string]any{
		"goal": "lifecycle", "plan": []string{"s1"},
		"agent_id": "hermes", "agent_session_id": "sess-1",
	}))
	var createResp map[string]any
	json.NewDecoder(w.Body).Decode(&createResp)
	taskID := createResp["task_id"].(string)

	// Start
	w = req(t, router, "POST", "/task/"+taskID+"/start", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("start status = %d", w.Code)
	}

	// Update step
	w = req(t, router, "POST", "/task/"+taskID+"/step", jsonBody(t, map[string]any{
		"step_index": 0, "status": "completed", "actual_outcome": "done",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("step update status = %d. body=%s", w.Code, w.Body.String())
	}

	// Checkpoint
	w = req(t, router, "POST", "/task/"+taskID+"/checkpoint", jsonBody(t, map[string]any{
		"snapshot_type": "normal", "step_index": 0, "monologue": "all good", "agent_id": "hermes",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("checkpoint status = %d", w.Code)
	}

	// Finish
	w = req(t, router, "POST", "/task/"+taskID+"/finish", jsonBody(t, map[string]any{
		"end_reason": "completed",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("finish status = %d", w.Code)
	}

	// Resume
	w = req(t, router, "POST", "/task/"+taskID+"/resume", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("resume status = %d. body=%s", w.Code, w.Body.String())
	}
	var resumeResp map[string]any
	json.NewDecoder(w.Body).Decode(&resumeResp)
	if resumeResp["resume_cmd"] == "" {
		t.Error("resume_cmd is empty")
	}
}

func TestPlanUpdateAndSkipAndReorder(t *testing.T) {
	cfg, database := setupTest(t)
	router := NewRouter(database, cfg)

	w := req(t, router, "POST", "/task/create", jsonBody(t, map[string]any{
		"goal": "plan-test", "plan": []string{"s1", "s2"},
		"agent_id": "h", "agent_session_id": "s1",
	}))
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	taskID := resp["task_id"].(string)

	// Plan update
	w = req(t, router, "POST", "/task/"+taskID+"/plan-update", jsonBody(t, map[string]any{
		"plan": []string{"new1", "new2", "new3"},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("plan-update status = %d. body=%s", w.Code, w.Body.String())
	}

	// Skip step
	w = req(t, router, "POST", "/task/"+taskID+"/step/skip", jsonBody(t, map[string]any{
		"step_index": 0, "reason": "skip it",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("skip status = %d", w.Code)
	}

	// Reorder
	w = req(t, router, "POST", "/task/"+taskID+"/step/reorder", jsonBody(t, map[string]any{
		"step_order": []int{2, 0, 1},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("reorder status = %d. body=%s", w.Code, w.Body.String())
	}
}

func TestSessionUpdateAndHeartbeat(t *testing.T) {
	cfg, database := setupTest(t)
	router := NewRouter(database, cfg)

	w := req(t, router, "POST", "/task/create", jsonBody(t, map[string]any{
		"goal": "session-test", "plan": []string{"s1"},
		"agent_id": "h", "agent_session_id": "old-session",
	}))
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	taskID := resp["task_id"].(string)

	// Session update
	w = req(t, router, "POST", "/task/"+taskID+"/session", jsonBody(t, map[string]any{
		"agent_session_id": "new-session",
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("session update status = %d", w.Code)
	}

	// Heartbeat
	w = req(t, router, "POST", "/task/"+taskID+"/heartbeat", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d", w.Code)
	}
}

func TestGetTask_notFound(t *testing.T) {
	cfg, database := setupTest(t)
	router := NewRouter(database, cfg)
	w := req(t, router, "GET", "/task/nonexistent-id", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
