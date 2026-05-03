package api

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/BillZong/task-driver/internal/config"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// startTime records when the server started, used by the health endpoint.
var startTime = time.Now()

// NewRouter creates a chi router with all API routes registered.
func NewRouter(db *sql.DB, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	// CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	// Logger and recoverer
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health
	r.Get("/health", HealthHandler(db, cfg.DB))

	// Task routes
	r.Post("/task/create", CreateHandler(db, cfg))
	r.Get("/task/unfinished", ListUnfinishedHandler(db, cfg))
	r.Get("/task/{id}", GetHandler(db, cfg))
	r.Post("/task/{id}/start", StartHandler(db, cfg))
	r.Post("/task/{id}/step", UpdateStepHandler(db, cfg))
	r.Post("/task/{id}/plan-update", PlanUpdateHandler(db, cfg))
	r.Post("/task/{id}/step/reorder", ReorderStepsHandler(db, cfg))
	r.Post("/task/{id}/step/skip", SkipStepHandler(db, cfg))
	r.Post("/task/{id}/checkpoint", CreateCheckpointHandler(db, cfg))
	r.Post("/task/{id}/finish", FinishHandler(db, cfg))
	r.Post("/task/{id}/session", UpdateSessionHandler(db, cfg))
	r.Post("/task/{id}/heartbeat", HeartbeatHandler(db, cfg))
	r.Post("/task/{id}/resume", ResumeHandler(db, cfg))

	return r
}
