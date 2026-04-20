// Package notifications delivers event alerts to Slack and Telegram.
//
// Events that trigger notifications:
//   - Strategy version promoted to StatusPaper (optimizer auto-promotion)
//   - Strategy version promoted to StatusApproved (human approval)
//   - Strategy version deprecated or disabled
//   - Optimization job completed (with promoted count)
//   - Large win or loss (configurable PnL threshold)
//   - Risk rejection storm (many rejections in a short window)
//
// Both channels are optional.  If a webhook URL is empty, that channel
// is silently skipped.  Notification delivery is best-effort — a failed
// send is logged but never panics the trading pipeline.
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Config
// ─────────────────────────────────────────────────────────────────────────────

// Config holds all notification settings.
type Config struct {
	// Slack incoming webhook URL.
	// https://api.slack.com/messaging/webhooks
	SlackWebhookURL string

	// Telegram bot token and chat ID.
	// Create a bot via @BotFather, then get the chat ID from the API.
	TelegramBotToken string
	TelegramChatID   string

	// PnLAlertThreshold: absolute USDT PnL above which a win/loss fires an alert.
	// Default: 0 (disabled).
	PnLAlertThreshold float64

	// RateLimitSeconds: minimum seconds between alerts of the same type.
	// Prevents notification spam during volatile markets. Default: 60.
	RateLimitSeconds int
}

// DefaultConfig returns a config with all webhooks empty (disabled).
func DefaultConfig() Config {
	return Config{
		RateLimitSeconds: 60,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Event types
// ─────────────────────────────────────────────────────────────────────────────

// EventKind classifies an alert.
type EventKind string

const (
	EventStrategyPromotedToPaper    EventKind = "strategy_promoted_paper"
	EventStrategyApproved           EventKind = "strategy_approved"
	EventStrategyDeprecated         EventKind = "strategy_deprecated"
	EventOptimizerJobDone           EventKind = "optimizer_job_done"
	EventLargeWin                   EventKind = "large_win"
	EventLargeLoss                  EventKind = "large_loss"
	EventRiskRejectionStorm         EventKind = "risk_rejection_storm"
)

// Event is one notification payload.
type Event struct {
	Kind      EventKind
	Title     string
	Body      string
	Fields    []Field // optional key-value pairs shown in Slack attachments
	Severity  string  // "info", "warning", "critical"
	Timestamp time.Time
}

// Field is one key-value pair in an alert.
type Field struct {
	Key   string
	Value string
	Short bool // display inline in Slack
}

// ─────────────────────────────────────────────────────────────────────────────
// Service
// ─────────────────────────────────────────────────────────────────────────────

// Service sends event alerts to configured channels.
type Service struct {
	cfg        Config
	client     *http.Client
	mu         sync.Mutex
	lastSent   map[string]time.Time // rate limiting per event kind
}

// New creates a notification service.
func New(cfg Config) *Service {
	return &Service{
		cfg:      cfg,
		client:   &http.Client{Timeout: 10 * time.Second},
		lastSent: make(map[string]time.Time),
	}
}

// Send dispatches an event to all configured channels.
// It is non-blocking — delivery happens in a goroutine.
func (s *Service) Send(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	go s.send(event)
}

func (s *Service) send(event Event) {
	if s.rateLimited(string(event.Kind)) {
		return
	}
	s.markSent(string(event.Kind))

	if s.cfg.SlackWebhookURL != "" {
		if err := s.sendSlack(event); err != nil {
			log.Printf("notifications: slack send failed: %v", err)
		}
	}
	if s.cfg.TelegramBotToken != "" && s.cfg.TelegramChatID != "" {
		if err := s.sendTelegram(event); err != nil {
			log.Printf("notifications: telegram send failed: %v", err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Slack
// ─────────────────────────────────────────────────────────────────────────────

func (s *Service) sendSlack(event Event) error {
	color := slackColor(event.Severity)

	// Build Slack attachment fields
	var fields []map[string]any
	for _, f := range event.Fields {
		fields = append(fields, map[string]any{
			"title": f.Key,
			"value": f.Value,
			"short": f.Short,
		})
	}

	// Add timestamp field
	fields = append(fields, map[string]any{
		"title": "Time",
		"value": event.Timestamp.Format("2006-01-02 15:04:05 UTC"),
		"short": true,
	})

	payload := map[string]any{
		"attachments": []map[string]any{
			{
				"fallback":    fmt.Sprintf("[EvolvX] %s: %s", event.Title, event.Body),
				"color":       color,
				"title":       fmt.Sprintf("EvolvX · %s", event.Title),
				"text":        event.Body,
				"fields":      fields,
				"footer":      "EvolvX v1.3",
				"footer_icon": "https://github.com/JPeetz/EvolvX/raw/main/docs/icon.png",
				"ts":          event.Timestamp.Unix(),
			},
		},
	}

	body, _ := json.Marshal(payload)
	resp, err := s.client.Post(s.cfg.SlackWebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func slackColor(severity string) string {
	switch severity {
	case "critical":
		return "#dc2626" // red
	case "warning":
		return "#f59e0b" // amber
	default:
		return "#10b981" // emerald
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Telegram
// ─────────────────────────────────────────────────────────────────────────────

func (s *Service) sendTelegram(event Event) error {
	emoji := telegramEmoji(event.Severity)
	text := fmt.Sprintf("%s *EvolvX · %s*\n%s", emoji, escapeMarkdown(event.Title), escapeMarkdown(event.Body))
	for _, f := range event.Fields {
		text += fmt.Sprintf("\n• *%s*: %s", escapeMarkdown(f.Key), escapeMarkdown(f.Value))
	}
	text += fmt.Sprintf("\n_🕐 %s_", event.Timestamp.Format("2006-01-02 15:04 UTC"))

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", s.cfg.TelegramBotToken)
	payload := map[string]any{
		"chat_id":    s.cfg.TelegramChatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}
	body, _ := json.Marshal(payload)
	resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func telegramEmoji(severity string) string {
	switch severity {
	case "critical":
		return "🚨"
	case "warning":
		return "⚠️"
	default:
		return "✅"
	}
}

// escapeMarkdown escapes Telegram MarkdownV2 special characters.
func escapeMarkdown(s string) string {
	special := `\_*[]()~` + "`" + `>#+-=|{}.!`
	out := make([]byte, 0, len(s)*2)
	for _, c := range s {
		for _, r := range special {
			if c == r {
				out = append(out, '\\')
				break
			}
		}
		out = append(out, byte(c))
	}
	return string(out)
}

// ─────────────────────────────────────────────────────────────────────────────
// Rate limiting
// ─────────────────────────────────────────────────────────────────────────────

func (s *Service) rateLimited(kind string) bool {
	if s.cfg.RateLimitSeconds <= 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.lastSent[kind]
	if !ok {
		return false
	}
	return time.Since(last) < time.Duration(s.cfg.RateLimitSeconds)*time.Second
}

func (s *Service) markSent(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSent[kind] = time.Now()
}

// ─────────────────────────────────────────────────────────────────────────────
// Pre-built event constructors
// ─────────────────────────────────────────────────────────────────────────────

// StrategyPromotedToPaper builds the alert for optimizer auto-promotion.
func StrategyPromotedToPaper(strategyName, version, author, mutationDesc string, score float64) Event {
	return Event{
		Kind:     EventStrategyPromotedToPaper,
		Title:    "Strategy Promoted to Paper",
		Body:     fmt.Sprintf("*%s* v%s has been promoted to paper trading by the optimizer.", strategyName, version),
		Severity: "info",
		Fields: []Field{
			{Key: "Strategy", Value: strategyName, Short: true},
			{Key: "Version", Value: version, Short: true},
			{Key: "Score", Value: fmt.Sprintf("%.3f", score), Short: true},
			{Key: "Mutation", Value: mutationDesc, Short: false},
			{Key: "Next Step", Value: "Review in Registry and approve for live trading", Short: false},
		},
	}
}

// StrategyApproved builds the alert for human approval.
func StrategyApproved(strategyName, version, approvedBy string) Event {
	return Event{
		Kind:     EventStrategyApproved,
		Title:    "Strategy Approved for Live",
		Body:     fmt.Sprintf("*%s* v%s has been approved for live trading by %s.", strategyName, version, approvedBy),
		Severity: "warning", // warning = amber = important, needs attention
		Fields: []Field{
			{Key: "Strategy", Value: strategyName, Short: true},
			{Key: "Version", Value: version, Short: true},
			{Key: "Approved By", Value: approvedBy, Short: true},
		},
	}
}

// StrategyDeprecated builds the alert for deprecation.
func StrategyDeprecated(strategyName, version, reason string) Event {
	return Event{
		Kind:     EventStrategyDeprecated,
		Title:    "Strategy Deprecated",
		Body:     fmt.Sprintf("*%s* v%s has been deprecated.", strategyName, version),
		Severity: "warning",
		Fields: []Field{
			{Key: "Strategy", Value: strategyName, Short: true},
			{Key: "Version", Value: version, Short: true},
			{Key: "Reason", Value: reason, Short: false},
		},
	}
}

// OptimizerJobDone builds the job completion alert.
func OptimizerJobDone(jobID, strategyName string, evaluated, promoted int, duration time.Duration) Event {
	severity := "info"
	if promoted == 0 {
		severity = "warning"
	}
	return Event{
		Kind:     EventOptimizerJobDone,
		Title:    "Optimization Job Complete",
		Body:     fmt.Sprintf("Job for *%s* finished. %d/%d candidates promoted.", strategyName, promoted, evaluated),
		Severity: severity,
		Fields: []Field{
			{Key: "Strategy", Value: strategyName, Short: true},
			{Key: "Evaluated", Value: fmt.Sprintf("%d", evaluated), Short: true},
			{Key: "Promoted", Value: fmt.Sprintf("%d", promoted), Short: true},
			{Key: "Duration", Value: duration.Round(time.Second).String(), Short: true},
			{Key: "Job ID", Value: jobID[:12] + "…", Short: false},
		},
	}
}

// LargeWin builds the large win alert.
func LargeWin(symbol, strategyName string, pnl, returnPct float64) Event {
	return Event{
		Kind:     EventLargeWin,
		Title:    "Large Win",
		Body:     fmt.Sprintf("%s on *%s* (%s)", fmt.Sprintf("+%.2f USDT", pnl), symbol, strategyName),
		Severity: "info",
		Fields: []Field{
			{Key: "Symbol", Value: symbol, Short: true},
			{Key: "PnL", Value: fmt.Sprintf("+%.2f USDT", pnl), Short: true},
			{Key: "Return", Value: fmt.Sprintf("+%.2f%%", returnPct*100), Short: true},
		},
	}
}

// LargeLoss builds the large loss alert.
func LargeLoss(symbol, strategyName string, pnl, returnPct float64) Event {
	return Event{
		Kind:     EventLargeLoss,
		Title:    "Large Loss",
		Body:     fmt.Sprintf("%s on *%s* (%s)", fmt.Sprintf("%.2f USDT", pnl), symbol, strategyName),
		Severity: "critical",
		Fields: []Field{
			{Key: "Symbol", Value: symbol, Short: true},
			{Key: "PnL", Value: fmt.Sprintf("%.2f USDT", pnl), Short: true},
			{Key: "Return", Value: fmt.Sprintf("%.2f%%", returnPct*100), Short: true},
		},
	}
}

// keep unused context import from being flagged
var _ = context.Background
