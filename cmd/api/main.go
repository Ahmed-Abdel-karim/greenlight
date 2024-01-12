package main

import (
	"context"
	"database/sql"
	"fmt"
	"github/greenlight/internal/data"
	"github/greenlight/internal/jsonlog"
	"github/greenlight/internal/mailer"
	"github/greenlight/internal/vcs"
	"os"
	"sync"
	"time"

	"github.com/golang-migrate/migrate/v4"               // New import
	_ "github.com/golang-migrate/migrate/v4/source/file" // New import

	_ "github.com/lib/pq"
)

var (
	version = vcs.Version()
)

type application struct {
	config config
	logger *jsonlog.Logger
	models data.Model
	mailer mailer.Mailer
	wg     *sync.WaitGroup
}

func main() {
	checkVersion()
	cfg := getConfig()
	logger := jsonlog.NewLogger(os.Stdout, jsonlog.LevelInfo)
	db, err := openDB(cfg)
	if err != nil {
		logger.PrintFatal(err, nil)
	}
	defer db.Close()

	logger.PrintInfo("database connection pool established", nil)

	if err != nil {
		logger.PrintFatal(err, nil)
	}

	app := application{
		config: *cfg,
		logger: logger,
		models: data.NewModel(db),
		mailer: mailer.New(
			cfg.smtp.host,
			cfg.smtp.port,
			cfg.smtp.username,
			cfg.smtp.password,
			cfg.smtp.sender),
		wg: &sync.WaitGroup{},
	}

	SetupMetric(&app)

	err = app.migrateDb(db)
	if err != nil && err != migrate.ErrNoChange {
		logger.PrintFatal(err, nil)
	}
	err = app.server()
	if err != nil {
		logger.PrintFatal(err, nil)
	}
}

// The openDB() function returns a sql.DB connection pool.
func openDB(cfg *config) (*sql.DB, error) {
	// Use sql.Open() to create an empty connection pool, using the DSN from the config
	// struct.
	db, err := sql.Open("postgres", cfg.db.dsn)
	if err != nil {
		return nil, err
	}

	// Set the maximum number of open (in-use + idle) connections in the pool. Note that
	// passing a value less than or equal to 0 will mean there is no limit.
	db.SetMaxOpenConns(cfg.db.maxOpenConns)
	// Set the maximum number of idle connections in the pool. Again, passing a value
	// less than or equal to 0 will mean there is no limit.
	db.SetMaxIdleConns(cfg.db.maxIdleConns)
	// Use the time.ParseDuration() function to convert the idle timeout duration string
	// to a time.Duration type.
	duration, err := time.ParseDuration(cfg.db.maxIdleTime)
	if err != nil {
		return nil, err
	}
	// Set the maximum idle timeout.
	db.SetConnMaxIdleTime(duration)

	// Create a context with a 5-second timeout deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = db.PingContext(ctx)

	if err != nil {
		return nil, err
	}
	fmt.Println(cfg.db.dsn)
	return db, nil
}
