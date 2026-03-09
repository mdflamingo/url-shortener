package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mdflamingo/url-shortener/internal/logger"
	"go.uber.org/zap"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type URLPair struct {
	ShortURL    string
	OriginalURL string
	UserID      string
}

type DBStorage struct {
	dsn      string
	pool     *pgxpool.Pool
	poolOnce sync.Once
	initErr  error
}

func NewDBStorage(dsn string) (*DBStorage, error) {
	return &DBStorage{
		dsn: dsn,
	}, nil
}

func (d *DBStorage) getPool(ctx context.Context) (*pgxpool.Pool, error) {
	d.poolOnce.Do(func() {
		config, err := pgxpool.ParseConfig(d.dsn)
		if err != nil {
			d.initErr = fmt.Errorf("failed to parse config: %w", err)
			return
		}

		config.MaxConns = 10
		config.MinConns = 2
		config.MaxConnLifetime = time.Hour
		config.MaxConnIdleTime = 30 * time.Minute
		config.HealthCheckPeriod = time.Minute

		config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec

		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			d.initErr = fmt.Errorf("failed to create connection pool: %w", err)
			return
		}

		ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := pool.Ping(ctxPing); err != nil {
			pool.Close()
			d.initErr = fmt.Errorf("failed to ping database: %w", err)
			return
		}

		d.pool = pool

		go d.runMigrationsAsync()
	})

	if d.initErr != nil {
		return nil, d.initErr
	}
	return d.pool, nil
}

func (d *DBStorage) runMigrationsAsync() {
	time.Sleep(1 * time.Second)

	logger.Log.Info("Running database migrations in background")

	db, err := sql.Open("postgres", d.dsn)
	if err != nil {
		logger.Log.Error("Failed to open database for migrations", zap.Error(err))
		return
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		logger.Log.Error("Failed to ping database for migrations", zap.Error(err))
		return
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		logger.Log.Error("Failed to create migration driver", zap.Error(err))
		return
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://migrations",
		"postgres",
		driver)
	if err != nil {
		logger.Log.Error("Failed to create migrate instance", zap.Error(err))
		return
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		logger.Log.Error("Failed to run migrations", zap.Error(err))
		return
	}

	logger.Log.Info("Migrations completed successfully")
}

func (d *DBStorage) Save(shortURL, originalURL, userID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := d.getPool(ctx)
	if err != nil {
		return "", fmt.Errorf("database not available: %w", err)
	}

	var returnedShortURL string

	err = pool.QueryRow(ctx,
		`INSERT INTO urls (short_url, full_url, user_id)
         VALUES ($1, $2, $3)
         ON CONFLICT (full_url)
         DO UPDATE SET full_url = EXCLUDED.full_url
         RETURNING short_url`,
		shortURL, originalURL, userID).Scan(&returnedShortURL)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return "", ErrConflict
		}
		return "", fmt.Errorf("failed to save URL: %w", err)
	}

	if returnedShortURL != shortURL {
		return returnedShortURL, ErrConflict
	}

	return returnedShortURL, nil
}

func (d *DBStorage) SaveMany(urls []URLPair) ([]URLPair, error) {
	if len(urls) == 0 {
		return urls, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := d.getPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("database not available: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}

	for _, url := range urls {
		batch.Queue(
			`INSERT INTO urls (short_url, full_url, user_id)
             VALUES ($1, $2, $3)
             ON CONFLICT (full_url)
             DO NOTHING
             RETURNING short_url`,
			url.ShortURL, url.OriginalURL, url.UserID,
		)
	}

	br := tx.SendBatch(ctx, batch)

	results := make([]string, len(urls))

	for i := 0; i < len(urls); i++ {
		err := br.QueryRow().Scan(&results[i])
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
				var existingShortURL string
				if queryErr := tx.QueryRow(ctx,
					`SELECT short_url FROM urls WHERE full_url = $1`,
					urls[i].OriginalURL).Scan(&existingShortURL); queryErr != nil {
					br.Close()
					return nil, fmt.Errorf("failed to get existing URL for %s: %w", urls[i].OriginalURL, queryErr)
				}

				urls[i].ShortURL = existingShortURL
				results[i] = existingShortURL
			} else {
				br.Close()
				return nil, fmt.Errorf("failed to insert URL at index %d: %w", i, err)
			}
		} else {
			urls[i].ShortURL = results[i]
		}
	}

	if err := br.Close(); err != nil {
		return nil, fmt.Errorf("failed to close batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return urls, nil
}

func (d *DBStorage) Get(shortURL string) (string, bool, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := d.getPool(ctx)
	if err != nil {
		logger.Log.Error("Failed to get database pool", zap.Error(err))
		return "", false, false
	}

	var originalURL string
	var deleted bool
	err = pool.QueryRow(ctx, "SELECT full_url, is_deleted FROM urls WHERE short_url = $1", shortURL).Scan(&originalURL, &deleted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, false
		}
		logger.Log.Error("Failed to get URL", zap.Error(err))
		return "", false, false
	}
	return originalURL, true, deleted
}

func (d *DBStorage) Delete(doneCh chan struct{}, inputCh chan string, userID string) chan error {
	resultChs := d.fanOut(doneCh, inputCh, userID)
	finalCh := d.fanIn(doneCh, resultChs...)

	return finalCh
}

func (d *DBStorage) processBatch(ctx context.Context, batch []string, userID string) error {
	pool, err := d.getPool(ctx)
	if err != nil {
		return fmt.Errorf("database not available: %w", err)
	}

	query := `UPDATE urls SET is_deleted = true
              WHERE short_url = ANY($1) AND user_id = $2 AND is_deleted = false`

	_, err = pool.Exec(ctx, query, batch, userID)
	if err != nil {
		logger.Log.Error("Error deleting batch", zap.Error(err))
	}
	logger.Log.Info("Deleted batch", zap.Int("count", len(batch)), zap.String("userID", userID))
	return err
}

func (d *DBStorage) GetByUserID(userID string) ([]URLPair, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := d.getPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("database not available: %w", err)
	}

	rows, err := pool.Query(ctx,
		"SELECT short_url, full_url FROM urls WHERE user_id = $1",
		userID)

	if err != nil {
		return nil, fmt.Errorf("database query error: %w", err)
	}
	defer rows.Close()

	var urls []URLPair

	for rows.Next() {
		var url URLPair

		if err := rows.Scan(&url.ShortURL, &url.OriginalURL); err != nil {
			return nil, fmt.Errorf("data scan error: %w", err)
		}
		urls = append(urls, url)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows processing error: %w", err)
	}

	return urls, nil
}

func (d *DBStorage) Close() error {
	if d.pool != nil {
		d.pool.Close()
	}
	return nil
}

func (d *DBStorage) Ping(ctx context.Context) error {
	pool, err := d.getPool(ctx)
	if err != nil {
		return err
	}
	return pool.Ping(ctx)
}
