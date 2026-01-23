package db

import (
    "context"
    "time"
    "github.com/jackc/pgx/v5/pgxpool"
)

func New(dsn string) (*pgxpool.Pool, error) {
    cfg, err := pgxpool.ParseConfig(dsn)
    if err != nil {
        return nil, err
    }

    cfg.MaxConns = 5
    cfg.MaxConnLifetime = time.Hour

    return pgxpool.NewWithConfig(context.Background(), cfg)
}
