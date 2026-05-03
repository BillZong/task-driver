package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/BillZong/task-driver/internal/config"
	"github.com/BillZong/task-driver/internal/db"
	"github.com/BillZong/task-driver/internal/model"
	"github.com/go-chi/chi/v5"
)

// ── helpers ──────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, model.ErrorResponse{Error: msg})
}

func readBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// ── handlers ─────────────────────────────────────────────────────────

// CreateHandler handles POST /task/create.
func CreateHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.CreateTaskRequest
		if err := readBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		if req.Goal == "" {
			writeError(w, http.StatusBadRequest, "goal is required")
			return
		}
		if req.AgentID == "" {
			writeError(w, http.StatusBadRequest, "agent_id is required")
			return
		}

		planBytes, err := json.Marshal(req.Plan)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal plan: "+err.Error())
			return
		}
		planJSON := string(planBytes)

		taskID, err := db.CreateTask(database, req.Goal, planJSON, req.AgentID, req.AgentSessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create task: "+err.Error())
			return
		}

		for i, desc := range req.Plan {
			if err := db.CreateStep(database, taskID, i, desc, ""); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create step: "+err.Error())
				return
			}
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"task_id": taskID,
			"status":  model.TaskPending,
		})
	}
}

// GetHandler handles GET /task/{id}.
func GetHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		task, err := db.GetTask(database, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found: "+err.Error())
			return
		}

		steps, err := db.GetSteps(database, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get steps: "+err.Error())
			return
		}

		checkpoints, err := db.GetCheckpoints(database, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get checkpoints: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, model.TaskResponse{
			Task:        task,
			Steps:       steps,
			Checkpoints: checkpoints,
		})
	}
}

// ListUnfinishedHandler handles GET /task/unfinished.
func ListUnfinishedHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := db.ListUnfinishedTasks(database)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list unfinished tasks: "+err.Error())
			return
		}
		if items == nil {
			items = []model.TaskListItem{}
		}
		writeJSON(w, http.StatusOK, items)
	}
}

// StartHandler handles POST /task/{id}/start.
func StartHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		if err := db.StartTask(database, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start task: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"task_id": id,
			"status":  string(model.TaskInProgress),
		})
	}
}

// UpdateStepHandler handles POST /task/{id}/step.
func UpdateStepHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var req model.UpdateStepRequest
		if err := readBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if err := db.UpdateStep(database, id, req.StepIndex, req.Status, req.ActualOutcome, req.Error); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update step: "+err.Error())
			return
		}

		// Update current_step based on the step index if the step is now in_progress or completed
		if req.Status == model.StepInProgress || req.Status == model.StepCompleted {
			if err := db.UpdateCurrentStep(database, id, req.StepIndex); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update current step: "+err.Error())
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"task_id": id,
			"status":  string(req.Status),
		})
	}
}

// PlanUpdateHandler handles POST /task/{id}/plan-update.
func PlanUpdateHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var req model.PlanUpdateRequest
		if err := readBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		// Validate no completed steps exist for this task
		steps, err := db.GetSteps(database, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get steps: "+err.Error())
			return
		}
		for _, s := range steps {
			if s.Status == model.StepCompleted {
				writeError(w, http.StatusBadRequest, "cannot update plan after steps have been completed")
				return
			}
		}

		planBytes, err := json.Marshal(req.Plan)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to marshal plan: "+err.Error())
			return
		}
		planJSON := string(planBytes)

		// Build new steps from the request
		var newSteps []model.Step
		if len(req.Steps) > 0 {
			for _, su := range req.Steps {
				newSteps = append(newSteps, model.Step{
					StepIndex:       su.StepIndex,
					Description:     su.Description,
					ExpectedOutcome: su.ExpectedOutcome,
					Status:          model.StepPending,
				})
			}
		} else {
			// If only plan strings provided (no detailed steps), build steps from plan
			for i, desc := range req.Plan {
				newSteps = append(newSteps, model.Step{
					StepIndex:   i,
					Description: desc,
					Status:      model.StepPending,
				})
			}
		}

		if err := db.UpdatePlan(database, id, planJSON, newSteps); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update plan: "+err.Error())
			return
		}

		// Create a task_modified checkpoint
		envJSON, _ := json.Marshal(map[string]string{"note": req.ModificationNote})
		if _, err := db.CreateCheckpoint(database, id, model.CheckpointTaskModified, 0, "", string(envJSON), ""); err != nil {
			// Non-fatal: log but don't fail the request
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"task_id": id,
			"status":  "plan_updated",
		})
	}
}

// ReorderStepsHandler handles POST /task/{id}/step/reorder.
func ReorderStepsHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var req model.ReorderStepsRequest
		if err := readBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if err := db.ReorderSteps(database, id, req.StepOrder); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reorder steps: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"task_id": id,
			"status":  "reordered",
		})
	}
}

// SkipStepHandler handles POST /task/{id}/step/skip.
func SkipStepHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var req model.SkipStepRequest
		if err := readBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if err := db.SkipStep(database, id, req.StepIndex, req.Reason); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to skip step: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"task_id": id,
			"status":  "skipped",
		})
	}
}

// CreateCheckpointHandler handles POST /task/{id}/checkpoint.
func CreateCheckpointHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var req model.CreateCheckpointRequest
		if err := readBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		// Marshal environment if present
		var envStr string
		if req.Environment != nil {
			envBytes, err := json.Marshal(req.Environment)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to marshal environment: "+err.Error())
				return
			}
			envStr = string(envBytes)
		}

		cpID, err := db.CreateCheckpoint(database, id, req.SnapshotType, req.StepIndex, req.Monologue, envStr, req.AgentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create checkpoint: "+err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"checkpoint_id": cpID,
			"task_id":       id,
		})
	}
}

// FinishHandler handles POST /task/{id}/finish.
func FinishHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var req model.FinishTaskRequest
		if err := readBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if err := db.FinishTask(database, id, req.EndReason, req.Error); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to finish task: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"task_id": id,
			"status":  "finished",
		})
	}
}

// UpdateSessionHandler handles POST /task/{id}/session.
func UpdateSessionHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		var req model.UpdateSessionRequest
		if err := readBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if err := db.UpdateTaskSession(database, id, req.AgentSessionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update session: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"task_id": id,
			"status":  "session_updated",
		})
	}
}

// HeartbeatHandler handles POST /task/{id}/heartbeat.
func HeartbeatHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		if err := db.Heartbeat(database, id); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to heartbeat: "+err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"task_id": id,
			"status":  "alive",
		})
	}
}

// ResumeHandler handles POST /task/{id}/resume.
func ResumeHandler(database *sql.DB, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		task, err := db.GetTask(database, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found: "+err.Error())
			return
		}

		lastCheckpoint, err := db.GetLatestCheckpoint(database, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get latest checkpoint: "+err.Error())
			return
		}

		// Look up substitute agent config from the configuration
		agentCfg, ok := cfg.Agents[task.AgentID]
		resumeCmd := ""
		if ok {
			resumeCmd = strings.ReplaceAll(agentCfg.ResumeCmd, "{session_id}", task.AgentSessionID)
		}

		resp := model.ResumeResponse{
			Status:         task.Status,
			AgentID:        task.AgentID,
			AgentSessionID: task.AgentSessionID,
			ResumeCmd:      resumeCmd,
			CurrentStep:    task.CurrentStep,
			LastCheckpoint: lastCheckpoint,
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
