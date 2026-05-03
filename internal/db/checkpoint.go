package db

import (
	"database/sql"
	"fmt"

	"github.com/BillZong/task-driver/internal/model"
)

// CreateCheckpoint inserts a new checkpoint row and returns its auto-generated ID.
func CreateCheckpoint(db *sql.DB, taskID string, snapshotType model.CheckpointType, stepIndex int, monologue, environment, agentID string) (int64, error) {
	result, err := db.Exec(
		`INSERT INTO task_checkpoints (task_id, snapshot_type, step_index, monologue, environment, agent_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		taskID, snapshotType, stepIndex, monologue, environment, agentID, model.NowISO(),
	)
	if err != nil {
		return 0, fmt.Errorf("create checkpoint for task %q: %w", taskID, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("create checkpoint get last insert id for task %q: %w", taskID, err)
	}
	return id, nil
}

// GetCheckpoints retrieves all checkpoints for a task, newest first.
func GetCheckpoints(db *sql.DB, taskID string) ([]model.Checkpoint, error) {
	rows, err := db.Query(
		`SELECT id, task_id, snapshot_type, step_index, monologue, environment, agent_id, created_at
		 FROM task_checkpoints WHERE task_id=? ORDER BY created_at DESC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("get checkpoints for task %q: %w", taskID, err)
	}
	defer rows.Close()

	var checkpoints []model.Checkpoint
	for rows.Next() {
		var c model.Checkpoint
		if err := rows.Scan(&c.ID, &c.TaskID, &c.SnapshotType, &c.StepIndex, &c.Monologue,
			&c.Environment, &c.AgentID, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan checkpoint for task %q: %w", taskID, err)
		}
		checkpoints = append(checkpoints, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checkpoint rows for task %q: %w", taskID, err)
	}
	return checkpoints, nil
}

// GetLatestCheckpoint retrieves the most recent checkpoint for a task.
// Returns nil, nil if no checkpoint exists.
func GetLatestCheckpoint(db *sql.DB, taskID string) (*model.Checkpoint, error) {
	var c model.Checkpoint
	err := db.QueryRow(
		`SELECT id, task_id, snapshot_type, step_index, monologue, environment, agent_id, created_at
		 FROM task_checkpoints WHERE task_id=? ORDER BY created_at DESC LIMIT 1`,
		taskID,
	).Scan(&c.ID, &c.TaskID, &c.SnapshotType, &c.StepIndex, &c.Monologue,
		&c.Environment, &c.AgentID, &c.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest checkpoint for task %q: %w", taskID, err)
	}
	return &c, nil
}
