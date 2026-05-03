package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// InitDB opens a SQLite database at dbPath with WAL mode and busy_timeout,
// then creates all required tables if they do not already exist.
func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode and set busy timeout
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma %q: %w", p, err)
		}
	}

	// Create all tables
	statements := []string{
		createTaskStateTable(),
		createTaskStepsTable(),
		createTaskCheckpointsTable(),
		createAgentConfigTable(),
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to create table: %w", err)
		}
	}

	return db, nil
}

func createTaskStateTable() string {
	return fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS task_state (
			id               TEXT PRIMARY KEY,
			goal             TEXT,
			plan             TEXT,
			status           TEXT,
			current_step     INTEGER DEFAULT 0,
			agent_id         TEXT,
			agent_session_id TEXT,
			mapped_session_id TEXT DEFAULT '',
			error             TEXT DEFAULT '',
			created_at       TEXT,
			updated_at       TEXT
		)
	`)
}

func createTaskStepsTable() string {
	return fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS task_steps (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id          TEXT NOT NULL,
			step_index       INTEGER NOT NULL,
			description      TEXT,
			expected_outcome TEXT,
			status           TEXT DEFAULT 'pending',
			actual_outcome   TEXT DEFAULT '',
			error            TEXT DEFAULT '',
			retry_count      INTEGER DEFAULT 0,
			UNIQUE(task_id, step_index),
			FOREIGN KEY (task_id) REFERENCES task_state(id)
		)
	`)
}

func createTaskCheckpointsTable() string {
	return fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS task_checkpoints (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id         TEXT NOT NULL,
			snapshot_type   TEXT DEFAULT 'normal',
			step_index      INTEGER DEFAULT 0,
			monologue       TEXT DEFAULT '',
			environment     TEXT DEFAULT '',
			agent_id        TEXT DEFAULT '',
			created_at      TEXT,
			FOREIGN KEY (task_id) REFERENCES task_state(id)
		)
	`)
}

func createAgentConfigTable() string {
	return fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS agent_config (
			agent_id    TEXT PRIMARY KEY,
			resume_cmd  TEXT,
			description TEXT,
			created_at  TEXT
		)
	`)
}
