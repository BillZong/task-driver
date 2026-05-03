package db

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/BillZong/task-driver/internal/model"
)

func freshDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "task-driver-ut-*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	os.Remove(f.Name())
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := InitDB(f.Name())
	if err != nil {
		t.Fatalf("InitDB() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInitDB(t *testing.T) {
	db := freshDB(t)
	var tables []string
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	expected := []string{"agent_config", "task_checkpoints", "task_state", "task_steps"}
	for _, e := range expected {
		found := false
		for _, tbl := range tables {
			if tbl == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected table %q not found in %v", e, tables)
		}
	}
}

func TestCreateAndGetTask(t *testing.T) {
	db := freshDB(t)
	id, err := CreateTask(db, "test goal", `["step1","step2"]`, "hermes", "session-123")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if id == "" {
		t.Fatal("CreateTask() returned empty id")
	}
	task, err := GetTask(db, id)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.Goal != "test goal" {
		t.Errorf("Goal = %q, want %q", task.Goal, "test goal")
	}
	if task.Status != model.TaskPending {
		t.Errorf("Status = %q, want %q", task.Status, model.TaskPending)
	}
	if task.AgentID != "hermes" {
		t.Errorf("AgentID = %q, want %q", task.AgentID, "hermes")
	}
}

func TestGetTask_notFound(t *testing.T) {
	db := freshDB(t)
	_, err := GetTask(db, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestStartAndFinishTask(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "task-lifecycle", `["s1"]`, "hermes", "sess-1")
	if err := StartTask(db, id); err != nil {
		t.Fatalf("StartTask() error = %v", err)
	}
	task, _ := GetTask(db, id)
	if task.Status != model.TaskInProgress {
		t.Errorf("after start: Status = %q, want %q", task.Status, model.TaskInProgress)
	}
	if err := FinishTask(db, id, "completed", ""); err != nil {
		t.Fatalf("FinishTask() error = %v", err)
	}
	task, _ = GetTask(db, id)
	if task.Status != model.TaskCompleted {
		t.Errorf("after finish: Status = %q, want %q", task.Status, model.TaskCompleted)
	}
}

func TestListUnfinishedTasks(t *testing.T) {
	db := freshDB(t)
	id1, _ := CreateTask(db, "unfinished1", `["s1"]`, "h", "s1")
	StartTask(db, id1)
	id2, _ := CreateTask(db, "unfinished2", `["s1"]`, "h", "s2")
	StartTask(db, id2)
	FinishTask(db, id2, "completed", "")
	items, err := ListUnfinishedTasks(db)
	if err != nil {
		t.Fatalf("ListUnfinishedTasks() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 unfinished task, got %d", len(items))
	}
	if items[0].TaskID != id1 {
		t.Errorf("unfinished task id = %q, want %q", items[0].TaskID, id1)
	}
}

func TestUpdateCurrentStep(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "stepper", `["s1","s2"]`, "h", "s1")
	if err := UpdateCurrentStep(db, id, 1); err != nil {
		t.Fatalf("UpdateCurrentStep() error = %v", err)
	}
	task, _ := GetTask(db, id)
	if task.CurrentStep != 1 {
		t.Errorf("CurrentStep = %d, want 1", task.CurrentStep)
	}
}

func TestUpdateTaskSession(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "sessionable", `["s1"]`, "h", "old-session")
	if err := UpdateTaskSession(db, id, "new-session"); err != nil {
		t.Fatalf("UpdateTaskSession() error = %v", err)
	}
	task, _ := GetTask(db, id)
	if task.AgentSessionID != "new-session" {
		t.Errorf("AgentSessionID = %q, want %q", task.AgentSessionID, "new-session")
	}
}

func TestHeartbeat(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "hb", `["s1"]`, "h", "s1")
	time.Sleep(10 * time.Millisecond) // ensure different timestamp
	if err := Heartbeat(db, id); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	task, _ := GetTask(db, id)
	if task.UpdatedAt == "" {
		t.Error("UpdatedAt is empty")
	}
	parsed, err := time.Parse(time.RFC3339, task.UpdatedAt)
	if err != nil {
		t.Errorf("UpdatedAt %q is not valid RFC3339: %v", task.UpdatedAt, err)
	}
	if time.Since(parsed) > 10*time.Second {
		t.Errorf("UpdatedAt is too old: %v ago", time.Since(parsed))
	}
}

func TestStepLifecycle(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "steps", `["s1","s2","s3"]`, "h", "s1")
	for i := 0; i < 3; i++ {
		if err := CreateStep(db, id, i, "step desc", "expected outcome"); err != nil {
			t.Fatalf("CreateStep(%d) error = %v", i, err)
		}
	}
	steps, err := GetSteps(db, id)
	if err != nil {
		t.Fatalf("GetSteps() error = %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	for i, s := range steps {
		if s.StepIndex != i {
			t.Errorf("steps[%d].StepIndex = %d, want %d", i, s.StepIndex, i)
		}
		if s.Status != model.StepPending {
			t.Errorf("steps[%d].Status = %q, want pending", i, s.Status)
		}
	}
	if err := UpdateStep(db, id, 0, model.StepCompleted, "done", ""); err != nil {
		t.Fatalf("UpdateStep() error = %v", err)
	}
	steps, _ = GetSteps(db, id)
	if steps[0].Status != model.StepCompleted {
		t.Errorf("after update: Status = %q, want completed", steps[0].Status)
	}
	if steps[0].ActualOutcome != "done" {
		t.Errorf("ActualOutcome = %q, want %q", steps[0].ActualOutcome, "done")
	}
	if err := SkipStep(db, id, 1, "not needed"); err != nil {
		t.Fatalf("SkipStep() error = %v", err)
	}
	steps, _ = GetSteps(db, id)
	if steps[1].Status != model.StepSkipped {
		t.Errorf("after skip: Status = %q, want skipped", steps[1].Status)
	}
}

func TestReorderSteps(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "reorder", `["s1","s2","s3"]`, "h", "s1")
	for i := 0; i < 3; i++ {
		CreateStep(db, id, i, "", "")
	}
	if err := ReorderSteps(db, id, []int{2, 0, 1}); err != nil {
		t.Fatalf("ReorderSteps() error = %v", err)
	}
	steps, _ := GetSteps(db, id)
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps after reorder, got %d", len(steps))
	}
	for i := 0; i < 3; i++ {
		if steps[i].StepIndex != i {
			t.Errorf("steps[%d].StepIndex = %d, want %d", i, steps[i].StepIndex, i)
		}
	}
}

func TestCheckpointLifecycle(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "cp-test", `["s1"]`, "h", "s1")
	cpID, err := CreateCheckpoint(db, id, model.CheckpointNormal, 0, "first", `{}`, "hermes")
	if err != nil {
		t.Fatalf("CreateCheckpoint() error = %v", err)
	}
	if cpID <= 0 {
		t.Errorf("checkpoint id = %d, want > 0", cpID)
	}
	time.Sleep(2 * time.Second) // ensure different timestamp
	CreateCheckpoint(db, id, model.CheckpointCompressionBoundary, 1, "second", `{}`, "h")

	cps, err := GetCheckpoints(db, id)
	if err != nil {
		t.Fatalf("GetCheckpoints() error = %v", err)
	}
	if len(cps) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(cps))
	}
	if cps[0].Monologue != "second" {
		t.Errorf("cps[0].Monologue = %q, want %q", cps[0].Monologue, "second")
	}
	if cps[1].Monologue != "first" {
		t.Errorf("cps[1].Monologue = %q, want %q", cps[1].Monologue, "first")
	}
	latest, err := GetLatestCheckpoint(db, id)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint() error = %v", err)
	}
	if latest.Monologue != "second" {
		t.Errorf("latest Monologue = %q, want %q", latest.Monologue, "second")
	}
}

func TestUpdatePlan(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "plan-update", `["old1","old2"]`, "h", "s1")
	CreateStep(db, id, 0, "old step 1", "")
	CreateStep(db, id, 1, "old step 2", "")
	newPlan := `["new1","new2","new3"]`
	newSteps := []model.Step{
		{StepIndex: 0, Description: "new step 1", Status: model.StepPending},
		{StepIndex: 1, Description: "new step 2", Status: model.StepPending},
		{StepIndex: 2, Description: "new step 3", Status: model.StepPending},
	}
	if err := UpdatePlan(db, id, newPlan, newSteps); err != nil {
		t.Fatalf("UpdatePlan() error = %v", err)
	}
	task, _ := GetTask(db, id)
	if task.Plan != newPlan {
		t.Errorf("Plan = %q, want %q", task.Plan, newPlan)
	}
	steps, _ := GetSteps(db, id)
	if len(steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(steps))
	}
}
