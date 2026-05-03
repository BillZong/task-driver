package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BillZong/task-driver/internal/api"
	"github.com/BillZong/task-driver/internal/config"
	"github.com/BillZong/task-driver/internal/db"
)

func expandHome(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return home + path[1:]
}

func main() {
	configFlag := flag.String("config", "~/.config/task-driver/config.yaml", "path to config file")
	dbFlag := flag.String("db", "", "override database path from config")
	flag.Parse()

	cfgPath := expandHome(*configFlag)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config from %s: %v", cfgPath, err)
	}

	if *dbFlag != "" {
		cfg.DB = *dbFlag
	}

	dbPath := expandHome(cfg.DB)
	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database at %s: %v", dbPath, err)
	}
	defer database.Close()

	router := api.NewRouter(database, cfg)

	server := &http.Server{
		Addr:    cfg.Listen,
		Handler: router,
	}

	go func() {
		log.Printf("Task Driver listening on %s (db: %s)", cfg.Listen, dbPath)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	<-sig
	log.Printf("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Printf("Task Driver shut down")
}
