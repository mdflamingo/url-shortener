package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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

	return &DBStorage{db: db}, nil
}

func (d *DBStorage) Save(shortURL, originalURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := d.db.ExecContext(ctx,
		"INSERT INTO urls (short_url, full_url) VALUES ($1, $2)",
		shortURL, originalURL)

	if err != nil {
		return fmt.Errorf("failed to save URL: %w", err)
	}

	return nil
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

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO urls (short_url, full_url) VALUES ($1, $2) ON CONFLICT (full_url) DO NOTHING")
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, url := range urls {
		_, err = stmt.ExecContext(ctx, url.ShortURL, url.OriginalURL)
		if err != nil {
			return fmt.Errorf("failed to execute insert: %w", err)
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
