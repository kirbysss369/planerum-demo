package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	hours "planerum"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	demo := os.Getenv("DEMO_MODE") == "1"
	var store hours.Store
	var jobs hours.JobQueue
	if demo {
		store = hours.NewMemoryStore()
		jobs = hours.NewMemoryJobs()
		log.Print("DEMO MODE: fixture content, volatile storage")
	} else {
		url := os.Getenv("DATABASE_URL")
		if url == "" && os.Getenv("PGHOST") == "" {
			return errors.New("DATABASE_URL or PostgreSQL connection variables are required outside demo mode")
		}
		startup, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		pg, err := hours.NewPostgresStore(startup, url)
		if err != nil {
			return err
		}
		defer pg.Pool.Close()
		if err := pg.Migrate(startup); err != nil {
			return err
		}
		store = pg
		queue := hours.PostgresJobs{Pool: pg.Pool}
		if err := queue.Migrate(startup); err != nil {
			return err
		}
		jobs = queue
	}
	var model hours.Model
	if demo {
		model = hours.DemoModel{}
	} else {
		allowed := strings.Split(os.Getenv("CLOUD_ALLOWED_MODEL_IDS"), ",")
		baseURL := os.Getenv("CLOUD_BASE_URL")
		if baseURL == "" {
			baseURL = hours.CloudBaseURL
		}
		responseTokens := hours.DefaultCloudTokens
		if baseURL == hours.GigaSchoolBaseURL {
			responseTokens = hours.DefaultGigaSchoolTokens
		}
		if value := os.Getenv("CLOUD_MAX_RESPONSE_TOKENS"); value != "" {
			var err error
			responseTokens, err = strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("CLOUD_MAX_RESPONSE_TOKENS must be an integer: %w", err)
			}
		}
		var err error
		model, err = hours.NewCloudModelWithConfig(os.Getenv("CLOUD_API_KEY"), os.Getenv("CLOUD_MODEL_ID"), allowed, hours.CloudModelConfig{BaseURL: baseURL, ResponseTokens: responseTokens})
		if err != nil {
			return err
		}
		if os.Getenv("SERVICE_TOKEN") == "" {
			return errors.New("SERVICE_TOKEN is required outside demo mode")
		}
	}
	root := os.Getenv("ARTIFACT_ROOT")
	if root == "" {
		root = filepath.Join("data", "artifacts")
	}
	python := os.Getenv("OFFICE_PYTHON")
	if python == "" {
		python = "python3"
	}
	service := hours.Service{Store: store, Generator: hours.Generator{Model: model}}
	app := hours.App{
		Service:  service,
		Jobs:     jobs,
		Exporter: hours.Exporter{Root: root, Python: python, Script: filepath.Join("office", "export.py")},
		Token:    os.Getenv("SERVICE_TOKEN"),
		Embed:    os.Getenv("EMBED_MODE") == "1",
		Demo:     demo,
	}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		hours.RunGenerationWorker(ctx, jobs, service)
	}()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	host := "0.0.0.0"
	if demo {
		host = "127.0.0.1"
	}
	if configured := os.Getenv("HOST"); configured != "" {
		host = configured
	}
	server := &http.Server{Addr: net.JoinHostPort(host, port), Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	log.Printf("listening on %s:%s", host, port)
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-serverErrors:
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		log.Printf("HTTP shutdown: %v", err)
	}
	select {
	case <-workerDone:
	case <-shutdown.Done():
		return fmt.Errorf("worker shutdown: %w", shutdown.Err())
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}
