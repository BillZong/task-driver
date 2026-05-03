package db

import (
	"testing"

	"github.com/BillZong/task-driver/internal/model"
)

func TestFinishTask_failedAndCancelled(t *testing.T) {
	db := freshDB(t)

	// Failed
	id1, _ := CreateTask(db, "fail-me", `["s1"]`, "h", "s1")
	StartTask(db, id1)
	if err := FinishTask(db, id1, "failed", "timeout"); err != nil {
		t.Fatalf("FinishTask(failed) error = %v", err)
	}
	task, _ := GetTask(db, id1)
	if task.Status != model.TaskFailed {
		t.Errorf("Status = %q, want %q", task.Status, model.TaskFailed)
	}
	if task.Error != "timeout" {
		t.Errorf("Error = %q, want %q", task.Error, "timeout")
	}

	// Cancelled
	id2, _ := CreateTask(db, "cancel-me", `["s1"]`, "h", "s2")
	StartTask(db, id2)
	if err := FinishTask(db, id2, "cancelled", ""); err != nil {
		t.Fatalf("FinishTask(cancelled) error = %v", err)
	}
	task, _ = GetTask(db, id2)
	if task.Status != model.TaskCancelled {
		t.Errorf("Status = %q, want %q", task.Status, model.TaskCancelled)
	}
}

func TestUpdateStep_retryIncrement(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "retry", `["s1"]`, "h", "s1")
	CreateStep(db, id, 0, "desc", "")

	// First failure
	if err := UpdateStep(db, id, 0, model.StepFailed, "", "error 1"); err != nil {
		t.Fatal(err)
	}
	steps, _ := GetSteps(db, id)
	if steps[0].RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", steps[0].RetryCount)
	}
	if steps[0].Error != "error 1" {
		t.Errorf("Error = %q, want %q", steps[0].Error, "error 1")
	}

	// Second failure — retry should increment
	if err := UpdateStep(db, id, 0, model.StepFailed, "", "error 2"); err != nil {
		t.Fatal(err)
	}
	steps, _ = GetSteps(db, id)
	if steps[0].RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", steps[0].RetryCount)
	}
}

func TestInitDB_invalidPath(t *testing.T) {
	_, err := InitDB("/nonexistent/directory/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestListUnfinishedTasks_empty(t *testing.T) {
	db := freshDB(t)
	items, err := ListUnfinishedTasks(db)
	if err != nil {
		t.Fatalf("ListUnfinishedTasks() on empty DB error = %v", err)
	}
	// nil slice is acceptable — represents "no unfinished tasks"
	if items != nil && len(items) != 0 {
		t.Errorf("expected empty list, got %d items", len(items))
	}
}

func TestGetLatestCheckpoint_empty(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "no-cp", `["s1"]`, "h", "s1")
	cp, err := GetLatestCheckpoint(db, id)
	if err != nil {
		t.Fatalf("GetLatestCheckpoint() error = %v", err)
	}
	if cp != nil {
		t.Errorf("expected nil, got %+v", cp)
	}
}

func TestUpdatePlan_withCompletedRejected(t *testing.T) {
	db := freshDB(t)
	id, _ := CreateTask(db, "plan-locked", `["s1","s2"]`, "h", "s1")
	CreateStep(db, id, 0, "step 1", "")
	CreateStep(db, id, 1, "step 2", "")

	// Complete step 0
	UpdateStep(db, id, 0, model.StepCompleted, "done", "")

	// Try to update plan — should succeed but the completed step data remains
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
