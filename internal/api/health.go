package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/BillZong/task-driver/internal/model"
)

// HealthHandler returns an http.HandlerFunc that reports service health.
// dbPath is the filesystem path to the SQLite database file.
func HealthHandler(db *sql.DB, dbPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get database file size
		dbSize := "unknown"
		if fi, err := os.Stat(dbPath); err == nil {
			dbSize = formatBytes(fi.Size())
		}

		// Get task count
		var taskCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM task_state").Scan(&taskCount); err != nil {
			taskCount = 0
		}

		uptime := time.Since(startTime).String()

		resp := model.HealthResponse{
			Alive:     true,
			DBSize:    dbSize,
			TaskCount: taskCount,
			Uptime:    uptime,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}
}

// formatBytes converts a byte count to a human-readable string.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
