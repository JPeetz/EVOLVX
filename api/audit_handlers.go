// Package api – audit log HTTP handlers.
//
// Exposes the append-only event_log table (written by SQLiteEventLogger)
// as queryable REST endpoints used by the AuditLog UI page.
package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

// ─────────────────────────────────────────────────────────────────────────────
// Response types
// ─────────────────────────────────────────────────────────────────────────────

// AuditLogEntry is one row from the event_log table.
type AuditLogEntry struct {
	EntryID   string `json:"entry_id"`
	Kind      string `json:"kind"`
	Timestamp string `json:"timestamp"` // RFC3339
	Mode      string `json:"mode"`
	SessionID string `json:"session_id"`
	Payload   any    `json:"payload"`
}

// SessionInfo summarises one pipeline session.
type SessionInfo struct {
	SessionID   string `json:"session_id"`
	Mode        string `json:"mode"`
	StrategyID  string `json:"strategy_id"`
	FirstEvent  string `json:"first_event"`
	LastEvent   string `json:"last_event"`
	EventCount  int    `json:"event_count"`
	FillCount   int    `json:"fill_count"`
	ErrorCount  int    `json:"error_count"`
}

// ─────────────────────────────────────────────────────────────────────────────
// AuditService — wraps the event_log database
// ─────────────────────────────────────────────────────────────────────────────

// AuditService reads from the event_log SQLite database.
type AuditService struct {
	db *sql.DB
}

// NewAuditService opens the event log database at dbPath.
func NewAuditService(dbPath string) (*AuditService, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("audit: open db: %w", err)
	}
	return &AuditService{db: db}, nil
}

// Close releases the database.
func (a *AuditService) Close() error { return a.db.Close() }

// ─────────────────────────────────────────────────────────────────────────────
// Route registration
// ─────────────────────────────────────────────────────────────────────────────

// RegisterAuditRoutes mounts the audit log endpoints.
//
//   GET /audit/events    — query the event log
//   GET /audit/sessions  — list session summaries
func RegisterAuditRoutes(g *gin.RouterGroup, svc *AuditService) {
	g.GET("/events", func(c *gin.Context) {
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
		if limit > 500 {
			limit = 500
		}

		where, args := buildAuditWhere(
			c.Query("session_id"),
			c.Query("kind"),
			c.Query("mode"),
			c.Query("from"),
			c.Query("to"),
		)

		q := fmt.Sprintf(`
			SELECT id, kind, ts, mode, session_id, payload
			FROM   event_log
			WHERE  %s
			ORDER  BY ts DESC
			LIMIT  ? OFFSET ?`, where)
		args = append(args, limit, offset)

		rows, err := svc.db.Query(q, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var entries []AuditLogEntry
		for rows.Next() {
			var e AuditLogEntry
			var tsMs int64
			var payloadJSON string
			if err := rows.Scan(&e.EntryID, &e.Kind, &tsMs, &e.Mode, &e.SessionID, &payloadJSON); err != nil {
				continue
			}
			e.Timestamp = time.UnixMilli(tsMs).UTC().Format(time.RFC3339)
			// Unmarshal payload to preserve structure in response
			json.Unmarshal([]byte(payloadJSON), &e.Payload)
			entries = append(entries, e)
		}
		if entries == nil {
			entries = []AuditLogEntry{}
		}
		c.JSON(http.StatusOK, entries)
	})

	g.GET("/sessions", func(c *gin.Context) {
		rows, err := svc.db.Query(`
			SELECT
				session_id,
				mode,
				COUNT(*) as event_count,
				SUM(CASE WHEN kind = 'fill'  THEN 1 ELSE 0 END) as fill_count,
				SUM(CASE WHEN kind = 'error' THEN 1 ELSE 0 END) as error_count,
				MIN(ts) as first_ts,
				MAX(ts) as last_ts
			FROM event_log
			GROUP BY session_id, mode
			ORDER BY first_ts DESC
			LIMIT 50`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var sessions []SessionInfo
		for rows.Next() {
			var s SessionInfo
			var firstTs, lastTs int64
			if err := rows.Scan(&s.SessionID, &s.Mode, &s.EventCount,
				&s.FillCount, &s.ErrorCount, &firstTs, &lastTs); err != nil {
				continue
			}
			s.FirstEvent = time.UnixMilli(firstTs).UTC().Format(time.RFC3339)
			s.LastEvent  = time.UnixMilli(lastTs).UTC().Format(time.RFC3339)
			sessions = append(sessions, s)
		}
		if sessions == nil {
			sessions = []SessionInfo{}
		}
		c.JSON(http.StatusOK, sessions)
	})

	// Single event detail
	g.GET("/events/:id", func(c *gin.Context) {
		row := svc.db.QueryRow(
			`SELECT id, kind, ts, mode, session_id, payload FROM event_log WHERE id=?`,
			c.Param("id"),
		)
		var e AuditLogEntry
		var tsMs int64
		var payloadJSON string
		if err := row.Scan(&e.EntryID, &e.Kind, &tsMs, &e.Mode, &e.SessionID, &payloadJSON); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "event not found"})
			return
		}
		e.Timestamp = time.UnixMilli(tsMs).UTC().Format(time.RFC3339)
		json.Unmarshal([]byte(payloadJSON), &e.Payload)
		c.JSON(http.StatusOK, e)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Query builder
// ─────────────────────────────────────────────────────────────────────────────

func buildAuditWhere(sessionID, kind, mode, from, to string) (string, []any) {
	var clauses []string
	var args []any
	clauses = append(clauses, "1=1")

	if sessionID != "" {
		clauses = append(clauses, "session_id=?")
		args = append(args, sessionID)
	}
	if kind != "" {
		clauses = append(clauses, "kind=?")
		args = append(args, kind)
	}
	if mode != "" {
		clauses = append(clauses, "mode=?")
		args = append(args, mode)
	}
	if from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			clauses = append(clauses, "ts >= ?")
			args = append(args, t.UnixMilli())
		}
	}
	if to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			clauses = append(clauses, "ts <= ?")
			args = append(args, t.UnixMilli())
		}
	}

	return strings.Join(clauses, " AND "), args
}
