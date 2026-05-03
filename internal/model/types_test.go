package model

import (
	"testing"
	"time"
)

func TestTaskStatusValues(t *testing.T) {
	tests := []struct {
		input TaskStatus
		want  string
	}{
		{TaskPending, "pending"},
		{TaskInProgress, "in_progress"},
		{TaskCompleted, "completed"},
		{TaskFailed, "failed"},
		{TaskCancelled, "cancelled"},
		{TaskPaused, "paused"},
	}
	for _, tt := range tests {
		if string(tt.input) != tt.want {
			t.Errorf("TaskStatus(%s) = %q, want %q", tt.input, tt.input, tt.want)
		}
	}
}

func TestStepStatusValues(t *testing.T) {
	tests := []struct {
		input StepStatus
		want  string
	}{
		{StepPending, "pending"},
		{StepInProgress, "in_progress"},
		{StepCompleted, "completed"},
		{StepFailed, "failed"},
		{StepSkipped, "skipped"},
	}
	for _, tt := range tests {
		if string(tt.input) != tt.want {
			t.Errorf("StepStatus(%s) = %q, want %q", tt.input, tt.input, tt.want)
		}
	}
}

func TestCheckpointTypeValues(t *testing.T) {
	tests := []struct {
		input CheckpointType
		want  string
	}{
		{CheckpointNormal, "normal"},
		{CheckpointCompressionBoundary, "compression_boundary"},
		{CheckpointPhaseBoundary, "phase_boundary"},
		{CheckpointTaskModified, "task_modified"},
	}
	for _, tt := range tests {
		if string(tt.input) != tt.want {
			t.Errorf("CheckpointType(%s) = %q, want %q", tt.input, tt.input, tt.want)
		}
	}
}

func TestNowISO(t *testing.T) {
	got := NowISO()
	_, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Errorf("NowISO() = %q is not valid RFC3339: %v", got, err)
	}
}

func TestIsTerminalStatus(t *testing.T) {
	tests := []struct {
		status TaskStatus
		want   bool
	}{
		{TaskPending, false},
		{TaskInProgress, false},
		{TaskCompleted, true},
		{TaskFailed, true},
		{TaskCancelled, true},
		{TaskPaused, false},
	}
	for _, tt := range tests {
		if got := IsTerminalStatus(tt.status); got != tt.want {
			t.Errorf("IsTerminalStatus(%s) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestParsePlan(t *testing.T) {
	tests := []struct {
		name string
		json string
		want []string
	}{
		{"empty string", "", nil},
		{"valid array", `["step1","step2","step3"]`, []string{"step1", "step2", "step3"}},
		{"single element", `["only"]`, []string{"only"}},
		{"invalid json", `not json`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePlan(tt.json)
			if len(got) != len(tt.want) {
				t.Errorf("ParsePlan() = %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParsePlan()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
