package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// Client is a thin wrapper around a sql.DB connected to a SQLite database.
// More query/helper methods will be added later as the tool evolves.
// Use NewClient to construct it.
//
// The underlying SQLite driver used is modernc.org/sqlite to avoid CGO.
// Driver name: "sqlite"
// DSN: path to the database file (relative or absolute)
//
// Note: SQLite is not highly concurrent. We limit MaxOpenConns to 1 by default
// to avoid locking issues for this CLI tool.
//
// Close the client when finished to free resources.
//
// Example:
//
//	c, err := db.NewClient(Cfg.Database)
//	if err != nil { return err }
//	defer c.Close()
//
//	// Use c.DB to run queries
type Client struct {
	DB   *sql.DB
	Path string
}

// NewClient opens a connection to the given SQLite database path and verifies it.
// Returns an error if the path is empty or the connection cannot be established.
func NewClient(path string) (*Client, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is empty")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// SQLite + CLI: keep it simple, avoid many concurrent connections
	db.SetMaxOpenConns(1)
	// No idle connections needed for a short-lived CLI
	db.SetMaxIdleConns(1)

	// Verify connectivity
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	return &Client{DB: db, Path: path}, nil
}

// Close closes the underlying sql.DB. Safe to call multiple times or on a nil client.
func (c *Client) Close() error {
	if c == nil || c.DB == nil {
		return nil
	}
	return c.DB.Close()
}
