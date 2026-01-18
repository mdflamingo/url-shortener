package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

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
}

type DBStorage struct {
	pool *pgxpool.Pool
}

func NewDBStorage(dsn string) (*DBStorage, error) {
	ctx := context.Background()

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute
	config.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(ctxPing); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if err := runMigrations(dsn); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &DBStorage{pool: pool}, nil
}

func (d *DBStorage) Save(shortURL, originalURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var returnedShortURL string

	err := d.pool.QueryRow(ctx,
		`INSERT INTO urls (short_url, full_url)
         VALUES ($1, $2)
         ON CONFLICT (full_url)
         DO UPDATE SET full_url = EXCLUDED.full_url
         RETURNING short_url`,
		shortURL, originalURL).Scan(&returnedShortURL)

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

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batch := &pgx.Batch{}

	for _, url := range urls {
		batch.Queue(
			`INSERT INTO urls (short_url, full_url)
             VALUES ($1, $2)
             ON CONFLICT (full_url)
             DO UPDATE SET short_url = EXCLUDED.short_url
             RETURNING short_url`,
			url.ShortURL, url.OriginalURL,
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

func (d *DBStorage) Get(shortURL string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var originalURL string
	err := d.pool.QueryRow(ctx,
		"SELECT full_url FROM urls WHERE short_url = $1 AND is_deleted = False",
		shortURL).Scan(&originalURL)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false
		}
		return "", false
	}

	return originalURL, true
}

func (d *DBStorage) Delete(doneCh chan struct{}, inputCh chan string, userID string) chan error {
	resultChs := d.fanOut(doneCh, inputCh, userID)
	finalCh := d.fanIn(doneCh, resultChs...)

	return finalCh
}

func (d *DBStorage) processBatch(ctx context.Context, batch []string, userID string) error {
	query := `UPDATE urls SET is_deleted = true
              WHERE short_url = ANY($1) AND user_id = $2 AND is_deleted = false`

	_, err := d.pool.Exec(ctx, query, batch, userID)
	if err != nil {
		fmt.Printf("Error deleting batch: %v\n", err)
	}
	return err
}

func (d *DBStorage) GetByUserID(userID string) ([]URLPair, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := d.pool.Query(ctx,
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

	if len(urls) == 0 {
		return []URLPair{}, nil
	}

	return urls, nil
}

func (d *DBStorage) Close() error {
	d.pool.Close()
	return nil
}

func runMigrations(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create migration driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://migrations",
		"postgres",
		driver)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func (d *DBStorage) Ping(ctx context.Context) error {
	return d.pool.Ping(ctx)
}
