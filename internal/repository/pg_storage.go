// Package repository содержит PostgreSQL хранилище URL с поддержкой миграций и пула соединений
//
// Использует pgxpool для конкурентного доступа и автоматические миграции.
// Поддерживает soft delete (is_deleted), батч операции, fan-out/fan-in удаление.
// Уникальность обеспечивается через UNIQUE constraint на full_url.
//
// Таблица urls:
//
//	short_url (PK), full_url (UNIQUE), user_id, is_deleted (bool)
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/mdflamingo/url-shortener/internal/logger"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// DBStorage - PostgreSQL хранилище с пулом соединений и миграциями
type DBStorage struct {
	dsn      string        // DSN строка подключения
	pool     *pgxpool.Pool // пул соединений (lazy init)
	poolOnce sync.Once     // гарантия единственной инициализации пула
	initErr  error         // ошибка инициализации
}

// NewDBStorage создает PostgreSQL хранилище
//
// dsn - строка подключения (postgres://user:pass@host:port/db?sslmode=disable).
// Пул инициализируется лениво при первом запросе.
func NewDBStorage(dsn string) (*DBStorage, error) {
	return &DBStorage{
		dsn: dsn,
	}, nil
}

// getPool возвращает инициализированный пул соединений (lazy singleton)
//
// Настройки пула:
//
//	MaxConns=10, MinConns=2, MaxConnLifetime=1h, HealthCheck=1m
//
// Автоматически запускает миграции в фоне.
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

		if err := d.runMigrationsSync(); err != nil {
			d.initErr = fmt.Errorf("failed to run migrations: %w", err)
			return
		}
	})

	if d.initErr != nil {
		return nil, d.initErr
	}
	return d.pool, nil
}

// Использует golang-migrate из папки migrations/.
// Логирует результат выполнения.
// runMigrationsSync синхронно запускает миграции
func (d *DBStorage) runMigrationsSync() error {
	logger.Log.Info("Running database migrations (sync)")

	db, err := sql.Open("postgres", d.dsn)
	if err != nil {
		return fmt.Errorf("failed to open database for migrations: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database for migrations: %w", err)
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

	logger.Log.Info("Migrations completed successfully")
	return nil
}

// Save сохраняет URL с обработкой конфликтов по full_url
//
// Использует UPSERT (ON CONFLICT full_url). Если возвращен другой shortURL - конфликт.
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

// SaveMany сохраняет батч URL в транзакции с обработкой конфликтов
//
// Использует pgx.Batch + транзакцию. При конфликте возвращает существующий shortURL.
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

// Get получает URL по shortURL с флагом удаления
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

// Delete асинхронно удаляет батчи URL (soft delete, fan-out/fan-in)
func (d *DBStorage) Delete(doneCh chan struct{}, inputCh chan string, userID string) chan error {
	resultChs := d.fanOut(doneCh, inputCh, userID)
	finalCh := d.fanIn(doneCh, resultChs...)

	return finalCh
}

// processBatch удаляет батч URL пользователя (is_deleted = true)
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

// GetByUserID возвращает все URL пользователя (активные)
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

// GetStats возвращает количество сокращенных url и количество пользователей в сервисе
func (d *DBStorage) GetStats() (URLStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pool, err := d.getPool(ctx)
	if err != nil {
		logger.Log.Error("Failed to get database pool", zap.Error(err))
		return URLStats{}, err
	}

	var Stats URLStats

	err = pool.QueryRow(ctx, "SELECT count(short_url) as urls, count(distinct(user_id)) as users FROM urls;").Scan(&Stats.Urls, &Stats.Users)
	if err != nil {
		return URLStats{}, err
	}
	return Stats, nil
}

// Close закрывает пул соединений
func (d *DBStorage) Close() error {
	if d.pool != nil {
		d.pool.Close()
	}
	return nil
}

// Ping проверяет доступность БД через пул
func (d *DBStorage) Ping(ctx context.Context) error {
	pool, err := d.getPool(ctx)
	if err != nil {
		return err
	}
	return pool.Ping(ctx)
}
