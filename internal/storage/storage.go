package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"Real_TimePricingEngine/internal/models"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
)

type Store struct {
	redis *redis.Client
	db    *sql.DB
}

func NewStore(
	ctx context.Context,
	redisAddr string,
	redisPassword string,
	postgresDSN string,
) (*Store, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		Password:     redisPassword,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("connect to Redis: %w", err)
	}

	db, err := sql.Open("pgx", postgresDSN)
	if err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		_ = rdb.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}

	store := &Store{
		redis: rdb,
		db:    db,
	}

	if err := store.initSchema(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) initSchema(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS price_history (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			symbol VARCHAR(20) NOT NULL,
			buy DOUBLE PRECISION NOT NULL,
			sell DOUBLE PRECISION NOT NULL,
			high_24h DOUBLE PRECISION NOT NULL,
			low_24h DOUBLE PRECISION NOT NULL,
			change_24h DOUBLE PRECISION NOT NULL,
			price_timestamp TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (symbol, price_timestamp, buy, sell)
		);

		CREATE INDEX IF NOT EXISTS idx_price_history_symbol_timestamp
			ON price_history (symbol, price_timestamp DESC);

		CREATE TABLE IF NOT EXISTS merchants (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			name VARCHAR(150) NOT NULL,
			email VARCHAR(255) NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role VARCHAR(20) NOT NULL DEFAULT 'merchant'
				CHECK (role IN ('merchant', 'admin')),
			kyc_status VARCHAR(20) NOT NULL DEFAULT 'pending'
				CHECK (kyc_status IN ('pending', 'verified', 'rejected')),
			account_status VARCHAR(20) NOT NULL DEFAULT 'active'
				CHECK (account_status IN ('active', 'suspended')),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_merchants_kyc_status
			ON merchants (kyc_status);

		CREATE INDEX IF NOT EXISTS idx_merchants_account_status
			ON merchants (account_status);
	`

	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("initialize database schema: %w", err)
	}

	return nil
}

// SaveSnapshot writes the current snapshot to Redis and historical data to
// PostgreSQL. A failure in either write is returned to the caller
func (s *Store) SaveSnapshot(
	ctx context.Context,
	snapshot models.PriceSnapshot,
) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	// Fast lookup of the latest snapshot for this symbol.
	if err := s.redis.Set(
		ctx,
		"prices:latest:"+snapshot.Symbol,
		data,
		0,
	).Err(); err != nil {
		return fmt.Errorf("save latest price to Redis: %w", err)
	}

	query := `
	INSERT INTO price_history
		(symbol, buy, sell, high_24h, low_24h, change_24h, price_timestamp)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	ON CONFLICT (symbol, price_timestamp, buy, sell) DO NOTHING
	`

	_, err = s.db.ExecContext(
		ctx,
		query,
		snapshot.Symbol,
		snapshot.Buy,
		snapshot.Sell,
		snapshot.High24h,
		snapshot.Low24h,
		snapshot.Change24h,
		snapshot.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("save price history to PostgreSQL: %w", err)
	}

	return nil
}

// GetLatest retrieves the latest cached snapshot for a symbol
func (s *Store) GetLatest(
	ctx context.Context,
	symbol string,
) (models.PriceSnapshot, bool, error) {
	data, err := s.redis.Get(
		ctx,
		"prices:latest:"+symbol,
	).Bytes()

	if err == redis.Nil {
		return models.PriceSnapshot{}, false, nil
	}
	if err != nil {
		return models.PriceSnapshot{}, false, fmt.Errorf(
			"read latest price from Redis: %w", err,
		)
	}

	var snapshot models.PriceSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return models.PriceSnapshot{}, false, fmt.Errorf(
			"decode cached snapshot: %w", err,
		)
	}

	return snapshot, true, nil
}

// LoadLatest retrieves the most recent stored snapshot per symbol from
// PostgreSQL. This provides a recovery source if Redis is empty
func (s *Store) LoadLatest(
	ctx context.Context,
) ([]models.PriceSnapshot, error) {
	query := `
	SELECT DISTINCT ON (symbol)
		symbol, buy, sell, high_24h, low_24h, change_24h, price_timestamp
	FROM price_history
	ORDER BY symbol, price_timestamp DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load latest prices: %w", err)
	}
	defer rows.Close()

	var snapshots []models.PriceSnapshot

	for rows.Next() {
		var snapshot models.PriceSnapshot

		if err := rows.Scan(
			&snapshot.Symbol,
			&snapshot.Buy,
			&snapshot.Sell,
			&snapshot.High24h,
			&snapshot.Low24h,
			&snapshot.Change24h,
			&snapshot.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan latest price: %w", err)
		}

		snapshots = append(snapshots, snapshot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest prices: %w", err)
	}

	return snapshots, nil
}

// for the load history

func (s *Store) LoadHistory(
	ctx context.Context,
	since time.Time,
) ([]models.PriceSnapshot, error) {
	query := `
	SELECT
		symbol, buy, sell, high_24h, low_24h, change_24h, price_timestamp
	FROM price_history
	WHERE price_timestamp >= $1
	ORDER BY price_timestamp ASC, symbol ASC
	`

	rows, err := s.db.QueryContext(ctx, query, since.UTC())
	if err != nil {
		return nil, fmt.Errorf("load price history: %w", err)
	}
	defer rows.Close()

	var snapshots []models.PriceSnapshot

	for rows.Next() {
		var snapshot models.PriceSnapshot

		if err := rows.Scan(
			&snapshot.Symbol,
			&snapshot.Buy,
			&snapshot.Sell,
			&snapshot.High24h,
			&snapshot.Low24h,
			&snapshot.Change24h,
			&snapshot.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan price history: %w", err)
		}

		snapshots = append(snapshots, snapshot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate price history: %w", err)
	}

	return snapshots, nil
}

func (s *Store) Close() error {
	var firstErr error

	if err := s.redis.Close(); err != nil {
		firstErr = fmt.Errorf("close Redis: %w", err)
	}

	if err := s.db.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("close PostgreSQL: %w", err)
	}

	return firstErr
}
