// Package api – registry, journal, and optimizer HTTP handlers.
//
// These handlers are added to the existing Gin router in api/router.go.
// They sit alongside the existing endpoints without modifying them.
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/optimizer"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// Handler groups
// ─────────────────────────────────────────────────────────────────────────────

// RegisterRegistryRoutes adds strategy registry endpoints to router group.
//
//	GET    /registry/strategies/:id/versions
//	GET    /registry/strategies/:id/versions/:version
//	POST   /registry/strategies
//	POST   /registry/strategies/:id/versions
//	PUT    /registry/strategies/:id/versions/:version/status
//	GET    /registry/strategies/:id/lineage
//	GET    /registry/strategies/:id/export/:version
func RegisterRegistryRoutes(g *gin.RouterGroup, svc *registry.Service) {
	g.GET("/strategies/:id/versions", func(c *gin.Context) {
		versions, err := svc.ListVersions(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, versions)
	})

	g.GET("/strategies/:id/versions/:version", func(c *gin.Context) {
		r, err := svc.GetVersion(c.Param("id"), c.Param("version"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, r)
	})

	g.GET("/strategies/:id/versions/:version/latest", func(c *gin.Context) {
		r, err := svc.GetLatest(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, r)
	})

	g.POST("/strategies", func(c *gin.Context) {
		var r registry.StrategyRecord
		if err := c.ShouldBindJSON(&r); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		created, err := svc.Create(&r)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, created)
	})

	g.POST("/strategies/:id/versions", func(c *gin.Context) {
		var req struct {
			ParentVersion   string              `json:"parent_version"`
			BumpType        string              `json:"bump_type"` // "major","minor","patch"
			Author          string              `json:"author"`
			Parameters      registry.Parameters `json:"parameters"`
			MutationSummary string              `json:"mutation_summary"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		child, err := svc.NewVersion(
			c.Param("id"), req.ParentVersion, req.BumpType,
			req.Author, req.Parameters, req.MutationSummary,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, child)
	})

	g.PUT("/strategies/:id/versions/:version/status", func(c *gin.Context) {
		var req struct {
			Status    string `json:"status"`
			ChangedBy string `json:"changed_by"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := svc.SetStatus(
			c.Param("id"), c.Param("version"),
			registry.StrategyStatus(req.Status), req.ChangedBy,
		)
		if err != nil {
			code := http.StatusInternalServerError
			if err == registry.ErrApprovalRequired || err == registry.ErrInvalidStatus {
				code = http.StatusBadRequest
			}
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	g.GET("/strategies/:id/lineage", func(c *gin.Context) {
		nodes, err := svc.GetLineage(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, nodes)
	})

	g.GET("/strategies/:id/export/:version", func(c *gin.Context) {
		r, err := svc.GetVersion(c.Param("id"), c.Param("version"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		data, err := registry.Export(r)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "application/json", data)
	})

	g.POST("/strategies/import", func(c *gin.Context) {
		data, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		r, err := registry.Import(data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		created, err := svc.Create(r)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, created)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Journal routes
// ─────────────────────────────────────────────────────────────────────────────

// RegisterJournalRoutes adds decision journal endpoints.
//
//	GET  /journal/decisions
//	GET  /journal/decisions/:id
//	POST /journal/decisions/:id/outcome
//	POST /journal/decisions/:id/review
//	GET  /journal/summaries/:strategy_id/:version
//	POST /journal/compact/:strategy_id/:version
func RegisterJournalRoutes(g *gin.RouterGroup, svc *journal.Service) {
	g.GET("/decisions", func(c *gin.Context) {
		f := journal.QueryFilter{
			StrategyID:      c.Query("strategy_id"),
			StrategyVersion: c.Query("strategy_version"),
			Symbol:          c.Query("symbol"),
		}
		if v := c.Query("limit"); v != "" {
			f.Limit, _ = strconv.Atoi(v)
		}
		if v := c.Query("offset"); v != "" {
			f.Offset, _ = strconv.Atoi(v)
		}
		if v := c.Query("outcome"); v != "" {
			f.OutcomeClass = journal.OutcomeClass(v)
		}
		if v := c.Query("from"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				f.From = &t
			}
		}
		if v := c.Query("to"); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				f.To = &t
			}
		}
		entries, err := svc.Query(f)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, entries)
	})

	g.GET("/decisions/:id", func(c *gin.Context) {
		e, err := svc.Get(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, e)
	})

	g.POST("/decisions/:id/outcome", func(c *gin.Context) {
		var outcome journal.Outcome
		if err := c.ShouldBindJSON(&outcome); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := svc.RecordOutcome(c.Param("id"), outcome); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	g.POST("/decisions/:id/review", func(c *gin.Context) {
		var req struct {
			Note     string `json:"note"`
			Reviewer string `json:"reviewer"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := svc.AddReviewNote(c.Param("id"), req.Note, req.Reviewer); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	g.GET("/summaries/:strategy_id/:version", func(c *gin.Context) {
		sum, err := svc.GetSummary(c.Param("strategy_id"), c.Param("version"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if sum == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "no summary yet"})
			return
		}
		c.JSON(http.StatusOK, sum)
	})

	g.POST("/compact/:strategy_id/:version", func(c *gin.Context) {
		retain := 30
		if v := c.Query("retain_days"); v != "" {
			retain, _ = strconv.Atoi(v)
		}
		sum, err := svc.Compact(c.Param("strategy_id"), c.Param("version"), retain)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, sum)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Optimizer routes
// ─────────────────────────────────────────────────────────────────────────────

// RegisterOptimizerRoutes adds optimizer endpoints.
//
//	POST /optimizer/jobs
//	GET  /optimizer/jobs/:job_id
//	GET  /optimizer/jobs?strategy_id=X
//	POST /optimizer/jobs/:job_id/run   (async)
func RegisterOptimizerRoutes(g *gin.RouterGroup, svc *optimizer.Service) {
	g.POST("/jobs", func(c *gin.Context) {
		var req struct {
			StrategyID      string                       `json:"strategy_id"`
			StrategyVersion string                       `json:"strategy_version"`
			CreatedBy       string                       `json:"created_by"`
			TrainFrom       time.Time                    `json:"train_from"`
			TrainTo         time.Time                    `json:"train_to"`
			ValFrom         time.Time                    `json:"val_from"`
			ValTo           time.Time                    `json:"val_to"`
			Thresholds      optimizer.PromotionThresholds `json:"thresholds"`
			MaxCandidates   int                          `json:"max_candidates"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.Thresholds.MinValReturn == 0 {
			req.Thresholds = optimizer.DefaultThresholds()
		}
		job, err := svc.Submit(
			req.StrategyID, req.StrategyVersion, req.CreatedBy,
			req.TrainFrom, req.TrainTo, req.ValFrom, req.ValTo,
			req.Thresholds, req.MaxCandidates,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, job)
	})

	g.GET("/jobs/:job_id", func(c *gin.Context) {
		job, err := svc.GetJob(c.Param("job_id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, job)
	})

	g.GET("/jobs", func(c *gin.Context) {
		strategyID := c.Query("strategy_id")
		if strategyID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "strategy_id required"})
			return
		}
		jobs, err := svc.ListJobs(strategyID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, jobs)
	})

	g.POST("/jobs/:job_id/run", func(c *gin.Context) {
		jobID := c.Param("job_id")
		// Fire-and-forget: run in background, return 202 immediately
		go func() {
			ctx := context.Background()
			if err := svc.Run(ctx, jobID); err != nil {
				// Error is persisted inside the job record
				_ = err
			}
		}()
		c.JSON(http.StatusAccepted, gin.H{"job_id": jobID, "status": "running"})
	})
}

// missing import
var _ = context.Background
