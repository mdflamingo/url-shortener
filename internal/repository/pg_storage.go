package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type URLPair struct {
	ShortURL    string
	OriginalURL string
}

type DBStorage struct {
	db *sql.DB
}

func NewDBStorage(dsn string) (*DBStorage, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS urls (
			id SERIAL PRIMARY KEY,
			short_url VARCHAR(255) NOT NULL,
			full_url VARCHAR NOT NULL UNIQUE
		);
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %w", err)
	}

	_, err = db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_urls_short_urls ON urls(short_url);`)
	if err != nil {
		return nil, fmt.Errorf("failed to create index: %w", err)
	}
	return &DBStorage{db: db}, nil
}

func (d *DBStorage) Save(shortURL, originalURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var returnedShortURL string

	err := d.db.QueryRowContext(ctx,
		`INSERT INTO urls (short_url, full_url)
         VALUES ($1, $2)
         ON CONFLICT (full_url)
         DO UPDATE SET full_url = EXCLUDED.full_url
         RETURNING short_url`,
		shortURL, originalURL).Scan(&returnedShortURL)

	if err != nil {
		return "", fmt.Errorf("failed to save URL: %w", err)
	}

	if returnedShortURL != shortURL {
		return returnedShortURL, ErrConflict
	}

	return returnedShortURL, nil
}

func (d *DBStorage) SaveMany(urls []URLPair) error {
	if len(urls) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	rolledBack := false
	defer func() {
		if !rolledBack && err != nil {
			tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO urls (short_url, full_url) VALUES ($1, $2) ON CONFLICT (full_url) DO NOTHING RETURNING short_url")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, url := range urls {
		_, execErr := stmt.ExecContext(ctx, url.ShortURL, url.OriginalURL)
		if execErr != nil {
			var pgErr *pgconn.PgError
			if errors.As(execErr, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
				tx.Rollback()
				rolledBack = true
				return ErrConflict
			}
			tx.Rollback()
			rolledBack = true
			return fmt.Errorf("failed to execute insert: %w", execErr)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (d *DBStorage) Get(shortURL string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var originalURL string
	err := d.db.QueryRowContext(ctx,
		"SELECT full_url FROM urls WHERE short_url = $1",
		shortURL).Scan(&originalURL)

	if err != nil {
		if err == sql.ErrNoRows {
			return "", false
		}
		return "", false
	}

	return originalURL, true
}

func (d *DBStorage) Close() error {
	return d.db.Close()
}
