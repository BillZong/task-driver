package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BillZong/task-driver/internal/api"
	"github.com/BillZong/task-driver/internal/config"
	"github.com/BillZong/task-driver/internal/daemon"
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

func usage() {
	fmt.Fprintf(os.Stderr, `Task Driver — Agent-agnostic task state persistence service

Usage:
  task-driver [options]              Run as foreground server
  task-driver daemon [options]       Run as background daemon (fork)
  task-driver start [options]        Same as 'daemon'
  task-driver stop                   Stop the daemon
  task-driver status                 Check daemon status
  task-driver install [options]      Install as system service
  task-driver uninstall              Remove system service

Options (for run/daemon/start/install):
  --config <path>    Config file path (default: ~/.config/task-driver/config.yaml)
  --db <path>        Override database path
  --pidfile <path>   PID file path (default: ~/.config/task-driver/task-driver.pid)
  --log-file <path>  Log file path (default: ~/.config/task-driver/task-driver.log)
`)
}

func main() {
	// Parse subcommand
	args := os.Args[1:]
	subcommand := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand = args[0]
		args = args[1:]
	}

	// Common flags
	fs := flag.NewFlagSet("task-driver", flag.ExitOnError)
	configFlag := fs.String("config", "~/.config/task-driver/config.yaml", "path to config file")
	dbFlag := fs.String("db", "", "override database path")
	pidFileFlag := fs.String("pidfile", "", "PID file (daemon mode)")
	logFileFlag := fs.String("log-file", "", "log file (daemon mode)")
	_ = fs.Parse(args) // subcommand-specific flags we extract manually

	switch subcommand {
	case "", "serve":
		runForeground(expandHome(*configFlag), expandHome(*dbFlag))
	case "daemon", "start":
		opts := daemon.Options{
			ConfigPath: expandHome(*configFlag),
			DBPath:     expandHome(*dbFlag),
			PIDFile:    *pidFileFlag,
			LogFile:    *logFileFlag,
		}
		if err := daemon.Start(opts); err != nil {
			log.Fatalf("Failed to start daemon: %v", err)
		}
		pidFile := opts.PIDFile
		if pidFile == "" {
			pidFile = daemon.DefaultPIDFile
		}
		log.Printf("Task Driver daemon started (PID file: %s)", expandHome(pidFile))
	case "stop":
		opts := daemon.Options{
			PIDFile: *pidFileFlag,
		}
		if err := daemon.Stop(opts); err != nil {
			log.Fatalf("Failed to stop daemon: %v", err)
		}
		log.Println("Task Driver daemon stopped")
	case "status":
		opts := daemon.Options{
			PIDFile: *pidFileFlag,
		}
		pid, err := daemon.Status(opts)
		if err != nil {
			log.Fatalf("Failed to check status: %v", err)
		}
		if pid > 0 {
			log.Printf("Task Driver is running (PID %d)", pid)
		} else {
			log.Println("Task Driver is not running")
			os.Exit(1)
		}
	case "install":
		opts := daemon.Options{
			BinaryPath: os.Args[0],
			ConfigPath: expandHome(*configFlag),
			PIDFile:    *pidFileFlag,
			LogFile:    *logFileFlag,
		}
		if err := daemon.InstallService(opts); err != nil {
			log.Fatalf("Failed to install service: %v", err)
		}
		log.Println("Task Driver service installed")
	case "uninstall":
		opts := daemon.Options{}
		if err := daemon.UninstallService(opts); err != nil {
			log.Fatalf("Failed to uninstall service: %v", err)
		}
		log.Println("Task Driver service uninstalled")
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n\n", subcommand)
		usage()
		os.Exit(1)
	}
}

func runForeground(cfgPath, dbOverride string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config from %s: %v", cfgPath, err)
	}

	if dbOverride != "" {
		cfg.DB = dbOverride
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
