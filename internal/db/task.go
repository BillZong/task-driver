package db

import (
	"database/sql"
	"fmt"

	"github.com/BillZong/task-driver/internal/model"
	"github.com/google/uuid"
)

// CreateTask inserts a new task_state row and returns the generated UUID.
func CreateTask(db *sql.DB, goal, planJSON, agentID, agentSessionID string) (taskID string, err error) {
	taskID = uuid.New().String()
	now := model.NowISO()
	_, err = db.Exec(
		`INSERT INTO task_state (id, goal, plan, status, current_step, agent_id, agent_session_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		taskID, goal, planJSON, model.TaskPending, agentID, agentSessionID, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}
	return taskID, nil
}

// GetTask retrieves a single task by its ID.
func GetTask(db *sql.DB, taskID string) (*model.Task, error) {
	var t model.Task
	err := db.QueryRow(
		`SELECT id, goal, plan, status, current_step, agent_id, agent_session_id, mapped_session_id, error, created_at, updated_at
		 FROM task_state WHERE id = ?`, taskID,
	).Scan(
		&t.ID, &t.Goal, &t.Plan, &t.Status, &t.CurrentStep,
		&t.AgentID, &t.AgentSessionID, &t.MappedSessionID, &t.Error,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get task %q: %w", taskID, err)
	}
	return &t, nil
}

// ListUnfinishedTasks returns all tasks that are not in a terminal state.
func ListUnfinishedTasks(db *sql.DB) ([]model.TaskListItem, error) {
	rows, err := db.Query(
		`SELECT id, agent_id, agent_session_id, goal, current_step, updated_at
		 FROM task_state
		 WHERE status NOT IN ('completed', 'failed', 'cancelled')
		 ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list unfinished tasks: %w", err)
	}
	defer rows.Close()

	var items []model.TaskListItem
	for rows.Next() {
		var item model.TaskListItem
		if err := rows.Scan(&item.TaskID, &item.AgentID, &item.AgentSessionID, &item.Goal, &item.CurrentStep, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task list item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task list rows: %w", err)
	}
	return items, nil
}

// StartTask sets a task's status to in_progress.
func StartTask(db *sql.DB, taskID string) error {
	_, err := db.Exec(
		`UPDATE task_state SET status=?, updated_at=? WHERE id=?`,
		model.TaskInProgress, model.NowISO(), taskID,
	)
	if err != nil {
		return fmt.Errorf("start task %q: %w", taskID, err)
	}
	return nil
}

// FinishTask sets a task's status based on endReason and records an optional error.
func FinishTask(db *sql.DB, taskID, endReason, errorStr string) error {
	var status model.TaskStatus
	switch endReason {
	case "completed":
		status = model.TaskCompleted
	case "failed":
		status = model.TaskFailed
	case "paused":
		status = model.TaskPaused
	default:
		status = model.TaskCancelled
	}

	_, err := db.Exec(
		`UPDATE task_state SET status=?, error=?, updated_at=? WHERE id=?`,
		status, errorStr, model.NowISO(), taskID,
	)
	if err != nil {
		return fmt.Errorf("finish task %q: %w", taskID, err)
	}
	return nil
}

// UpdateTaskSession updates the agent_session_id for a task.
func UpdateTaskSession(db *sql.DB, taskID, newSessionID string) error {
	_, err := db.Exec(
		`UPDATE task_state SET agent_session_id=?, updated_at=? WHERE id=?`,
		newSessionID, model.NowISO(), taskID,
	)
	if err != nil {
		return fmt.Errorf("update task session %q: %w", taskID, err)
	}
	return nil
}

// Heartbeat updates the updated_at timestamp for a task.
func Heartbeat(db *sql.DB, taskID string) error {
	_, err := db.Exec(
		`UPDATE task_state SET updated_at=? WHERE id=?`,
		model.NowISO(), taskID,
	)
	if err != nil {
		return fmt.Errorf("heartbeat task %q: %w", taskID, err)
	}
	return nil
}

// UpdateCurrentStep updates the current_step index for a task.
func UpdateCurrentStep(db *sql.DB, taskID string, stepIndex int) error {
	_, err := db.Exec(
		`UPDATE task_state SET current_step=?, updated_at=? WHERE id=?`,
		stepIndex, model.NowISO(), taskID,
	)
	if err != nil {
		return fmt.Errorf("update current step %q: %w", taskID, err)
	}
	return nil
}

// UpdatePlan updates the plan JSON for a task and upserts the given steps.
func UpdatePlan(db *sql.DB, taskID, planJSON string, newSteps []model.Step) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("update plan begin tx: %w", err)
	}
	defer tx.Rollback()

	// Update the plan column
	if _, err := tx.Exec(
		`UPDATE task_state SET plan=?, updated_at=? WHERE id=?`,
		planJSON, model.NowISO(), taskID,
	); err != nil {
		return fmt.Errorf("update plan %q: %w", taskID, err)
	}

	// Upsert each step using INSERT OR REPLACE
	for _, step := range newSteps {
		_, err := tx.Exec(
			`INSERT OR REPLACE INTO task_steps (task_id, step_index, description, expected_outcome, status, actual_outcome, error, retry_count)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			taskID, step.StepIndex, step.Description, step.ExpectedOutcome,
			step.Status, step.ActualOutcome, step.Error, step.RetryCount,
		)
		if err != nil {
			return fmt.Errorf("upsert step %d for task %q: %w", step.StepIndex, taskID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update plan commit tx: %w", err)
	}
	return nil
}
