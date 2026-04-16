// Package pipeline – SQLite event logger.
package pipeline

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

// SQLiteEventLogger persists every pipeline log entry to a SQLite database.
// It is safe for concurrent writes (serialised via mutex).
type SQLiteEventLogger struct {
	db  *sql.DB
	mu  sync.Mutex
	buf []*core.LogEntry
	cap int
}

// NewSQLiteEventLogger opens (or creates) the log database at path.
func NewSQLiteEventLogger(path string, bufferCap int) (*SQLiteEventLogger, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("event logger: open db: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("event logger: migrate: %w", err)
	}
	if bufferCap <= 0 {
		bufferCap = 200
	}
	return &SQLiteEventLogger{db: db, cap: bufferCap}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS event_log (
		id         TEXT PRIMARY KEY,
		kind       TEXT NOT NULL,
		ts         INTEGER NOT NULL,
		mode       TEXT NOT NULL,
		session_id TEXT NOT NULL,
		payload    TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_event_log_session ON event_log(session_id);
	CREATE INDEX IF NOT EXISTS idx_event_log_kind    ON event_log(kind);
	CREATE INDEX IF NOT EXISTS idx_event_log_ts      ON event_log(ts);
	`)
	return err
}

// Log appends one entry to the buffer; flushes when the buffer is full.
func (l *SQLiteEventLogger) Log(entry *core.LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry.EntryID == "" {
		entry.EntryID = uuid.NewString()
	}
	l.buf = append(l.buf, entry)
	if len(l.buf) >= l.cap {
		return l.flush()
	}
	return nil
}

// Close flushes any remaining buffered entries.
func (l *SQLiteEventLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.flush(); err != nil {
		return err
	}
	return l.db.Close()
}

func (l *SQLiteEventLogger) flush() error {
	if len(l.buf) == 0 {
		return nil
	}
	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO event_log(id,kind,ts,mode,session_id,payload) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, e := range l.buf {
		payload, _ := json.Marshal(e.Payload)
		ts := e.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		if _, err := stmt.Exec(e.EntryID, string(e.Kind), ts.UnixMilli(), string(e.Mode), e.SessionID, string(payload)); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	l.buf = l.buf[:0]
	return nil
}
