package model

import "time"

// ── 状态枚举 ──────────────────────────────────────────────

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
	TaskCancelled  TaskStatus = "cancelled"
	TaskPaused     TaskStatus = "paused"
)

type StepStatus string

const (
	StepPending    StepStatus = "pending"
	StepInProgress StepStatus = "in_progress"
	StepCompleted  StepStatus = "completed"
	StepFailed     StepStatus = "failed"
	StepSkipped    StepStatus = "skipped"
)

type CheckpointType string

const (
	CheckpointNormal             CheckpointType = "normal"
	CheckpointCompressionBoundary CheckpointType = "compression_boundary"
	CheckpointPhaseBoundary      CheckpointType = "phase_boundary"
	CheckpointTaskModified       CheckpointType = "task_modified"
)

// ── 领域模型 ──────────────────────────────────────────────

type Task struct {
	ID              string     `json:"id"`
	Goal            string     `json:"goal"`
	Plan            string     `json:"plan"` // JSON array
	Status          TaskStatus `json:"status"`
	CurrentStep     int        `json:"current_step"`
	AgentID         string     `json:"agent_id"`
	AgentSessionID  string     `json:"agent_session_id"`
	MappedSessionID string     `json:"mapped_session_id,omitempty"`
	Error           string     `json:"error,omitempty"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
}

type Step struct {
	ID              int        `json:"id"`
	TaskID          string     `json:"task_id"`
	StepIndex       int        `json:"step_index"`
	Description     string     `json:"description"`
	ExpectedOutcome string     `json:"expected_outcome"`
	Status          StepStatus `json:"status"`
	ActualOutcome   string     `json:"actual_outcome,omitempty"`
	Error           string     `json:"error,omitempty"`
	RetryCount      int        `json:"retry_count"`
}

type Checkpoint struct {
	ID           int            `json:"id"`
	TaskID       string         `json:"task_id"`
	SnapshotType CheckpointType `json:"snapshot_type"`
	StepIndex    int            `json:"step_index"`
	Monologue    string         `json:"monologue"`
	Environment  string         `json:"environment"` // JSON string
	AgentID      string         `json:"agent_id"`
	CreatedAt    string         `json:"created_at"`
}

type AgentConfig struct {
	AgentID     string `json:"agent_id"`
	ResumeCmd   string `json:"resume_cmd"`
	Description string `json:"description"`
}

// ── 请求类型 ──────────────────────────────────────────────

type CreateTaskRequest struct {
	Goal           string   `json:"goal"`
	Plan           []string `json:"plan"`
	AgentID        string   `json:"agent_id"`
	AgentSessionID string   `json:"agent_session_id"`
}

type UpdateStepRequest struct {
	StepIndex     int        `json:"step_index"`
	Status        StepStatus `json:"status"`
	ActualOutcome string     `json:"actual_outcome,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type PlanUpdateRequest struct {
	Plan             []string     `json:"plan"`
	Steps            []StepUpdate `json:"steps,omitempty"`
	ModificationNote string       `json:"modification_note"`
}

type StepUpdate struct {
	StepIndex       int    `json:"step_index"`
	Description     string `json:"description"`
	ExpectedOutcome string `json:"expected_outcome"`
}

type ReorderStepsRequest struct {
	StepOrder []int `json:"step_order"`
}

type SkipStepRequest struct {
	StepIndex int    `json:"step_index"`
	Reason    string `json:"reason"`
}

type CreateCheckpointRequest struct {
	SnapshotType CheckpointType `json:"snapshot_type"`
	StepIndex    int            `json:"step_index"`
	Monologue    string         `json:"monologue"`
	Environment  interface{}    `json:"environment,omitempty"`
	AgentID      string         `json:"agent_id"`
}

type FinishTaskRequest struct {
	EndReason string `json:"end_reason"`
	Error     string `json:"error,omitempty"`
}

type UpdateSessionRequest struct {
	AgentSessionID string `json:"agent_session_id"`
}

// ── 响应类型 ──────────────────────────────────────────────

type TaskResponse struct {
	Task        *Task        `json:"task,omitempty"`
	Steps       []Step       `json:"steps,omitempty"`
	Checkpoints []Checkpoint `json:"checkpoints,omitempty"`
}

type TaskListItem struct {
	TaskID         string `json:"task_id"`
	AgentID        string `json:"agent_id"`
	AgentSessionID string `json:"agent_session_id"`
	Goal           string `json:"goal"`
	CurrentStep    int    `json:"current_step"`
	UpdatedAt      string `json:"updated_at"`
}

type ResumeResponse struct {
	Status         TaskStatus   `json:"status"`
	AgentID        string       `json:"agent_id"`
	AgentSessionID string       `json:"agent_session_id"`
	ResumeCmd      string       `json:"resume_cmd"`
	CurrentStep    int          `json:"current_step"`
	LastCheckpoint *Checkpoint  `json:"last_checkpoint,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type HealthResponse struct {
	Alive     bool   `json:"alive"`
	DBSize    string `json:"db_size"`
	TaskCount int    `json:"task_count"`
	Uptime    string `json:"uptime"`
}

// ── 辅助函数 ──────────────────────────────────────────────

func NowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func IsTerminalStatus(s TaskStatus) bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCancelled
}
