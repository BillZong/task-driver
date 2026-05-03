package db

import (
	"database/sql"
	"fmt"

	"github.com/BillZong/task-driver/internal/model"
)

// CreateStep inserts a new step into the task_steps table.
func CreateStep(db *sql.DB, taskID string, stepIndex int, description, expectedOutcome string) error {
	_, err := db.Exec(
		`INSERT INTO task_steps (task_id, step_index, description, expected_outcome)
		 VALUES (?, ?, ?, ?)`,
		taskID, stepIndex, description, expectedOutcome,
	)
	if err != nil {
		return fmt.Errorf("create step for task %q: %w", taskID, err)
	}
	return nil
}

// UpdateStep updates the status, actual_outcome, error, and optionally increments
// retry_count when the status is "failed".
func UpdateStep(db *sql.DB, taskID string, stepIndex int, status model.StepStatus, actualOutcome, errorStr string) error {
	_, err := db.Exec(
		`UPDATE task_steps SET status=?, actual_outcome=?, error=?,
		 retry_count = CASE WHEN ?='failed' THEN retry_count+1 ELSE retry_count END
		 WHERE task_id=? AND step_index=?`,
		status, actualOutcome, errorStr, status, taskID, stepIndex,
	)
	if err != nil {
		return fmt.Errorf("update step %d for task %q: %w", stepIndex, taskID, err)
	}
	return nil
}

// GetSteps retrieves all steps for a task, ordered by step_index ascending.
func GetSteps(db *sql.DB, taskID string) ([]model.Step, error) {
	rows, err := db.Query(
		`SELECT id, task_id, step_index, description, expected_outcome, status, actual_outcome, error, retry_count
		 FROM task_steps WHERE task_id=? ORDER BY step_index ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("get steps for task %q: %w", taskID, err)
	}
	defer rows.Close()

	var steps []model.Step
	for rows.Next() {
		var s model.Step
		if err := rows.Scan(&s.ID, &s.TaskID, &s.StepIndex, &s.Description, &s.ExpectedOutcome,
			&s.Status, &s.ActualOutcome, &s.Error, &s.RetryCount); err != nil {
			return nil, fmt.Errorf("scan step for task %q: %w", taskID, err)
		}
		steps = append(steps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate step rows for task %q: %w", taskID, err)
	}
	return steps, nil
}

// ReorderSteps updates the step_index for each step according to the given
// ordering. stepOrder[i] = oldStepIndex means "the step that was at
// oldStepIndex should now be at position i".
func ReorderSteps(db *sql.DB, taskID string, stepOrder []int) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("reorder steps begin tx: %w", err)
	}
	defer tx.Rollback()

	// First, shift all steps to temporary negative indices to avoid UNIQUE conflicts
	// Use: new_index = -(old_index + 1) as temporary offset
	tempStmt, err := tx.Prepare(
		`UPDATE task_steps SET step_index=? WHERE task_id=? AND step_index=?`,
	)
	if err != nil {
		return fmt.Errorf("reorder steps prepare temp: %w", err)
	}
	defer tempStmt.Close()

	for _, oldIndex := range stepOrder {
		tempIndex := -(oldIndex + 1)
		if _, err := tempStmt.Exec(tempIndex, taskID, oldIndex); err != nil {
			return fmt.Errorf("reorder step temp %d->%d for task %q: %w", oldIndex, tempIndex, taskID, err)
		}
	}

	// Now assign the final indices
	for i, oldIndex := range stepOrder {
		if _, err := tx.Exec(
			`UPDATE task_steps SET step_index=? WHERE task_id=? AND step_index=?`,
			i, taskID, -(oldIndex+1),
		); err != nil {
			return fmt.Errorf("reorder step final %d->%d for task %q: %w", -(oldIndex+1), i, taskID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("reorder steps commit tx: %w", err)
	}
	return nil
}

// SkipStep marks a step as skipped with a reason as the actual_outcome.
func SkipStep(db *sql.DB, taskID string, stepIndex int, reason string) error {
	_, err := db.Exec(
		`UPDATE task_steps SET status=?, actual_outcome=? WHERE task_id=? AND step_index=?`,
		model.StepSkipped, reason, taskID, stepIndex,
	)
	if err != nil {
		return fmt.Errorf("skip step %d for task %q: %w", stepIndex, taskID, err)
	}
	return nil
}
