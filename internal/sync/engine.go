package sync

import (
	"context"
	"fmt"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

const historyCap = 50

type SyncResult struct {
	Timestamp          time.Time
	DurationMS         float64
	SyncedCategories   int
	SyncedLocations    int
	SyncedParts        int
	SyncedStockRecords int
	TotalStockQty      int
}

type Stats struct {
	LastResult       *SyncResult
	LastError        string
	History          []SyncResult // newest first, capped at historyCap
	TotalSyncCount   int
	TotalErrorCount  int
	APIRequestsTotal int64
	StartedAt        time.Time
}

// Engine orchestrates PartDB reads and ModernWMS writes; SyncNow is
// serialized under mu like the original (sync_service.py runs its
// background loop and any manually-triggered sync under one lock).
type Engine struct {
	WMS    *wmsdb.Client
	PartDB *partdb.DB

	mu               sync.Mutex
	lastResult       *SyncResult
	lastErr          error
	history          []SyncResult
	totalSyncCount   int
	totalErrorCount  int
	apiRequestsTotal int64
	startedAt        time.Time
}

func NewEngine(wms *wmsdb.Client, db *partdb.DB) *Engine {
	return &Engine{WMS: wms, PartDB: db, startedAt: time.Now()}
}

func (e *Engine) IncAPIRequests() {
	atomic.AddInt64(&e.apiRequestsTotal, 1)
}

func (e *Engine) Stats() Stats {
	e.mu.Lock()
	defer e.mu.Unlock()
	errMsg := ""
	if e.lastErr != nil {
		errMsg = e.lastErr.Error()
	}
	hist := make([]SyncResult, len(e.history))
	copy(hist, e.history)
	return Stats{
		LastResult:       e.lastResult,
		LastError:        errMsg,
		History:          hist,
		TotalSyncCount:   e.totalSyncCount,
		TotalErrorCount:  e.totalErrorCount,
		APIRequestsTotal: atomic.LoadInt64(&e.apiRequestsTotal),
		StartedAt:        e.startedAt,
	}
}

func (e *Engine) SyncNow(ctx context.Context) (*SyncResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()
	result, err := e.doSync(ctx, start)

	e.totalSyncCount++
	if err != nil {
		e.totalErrorCount++
		e.lastErr = err
	} else {
		e.lastErr = nil
		e.lastResult = result
		e.history = append([]SyncResult{*result}, e.history...)
		if len(e.history) > historyCap {
			e.history = e.history[:historyCap]
		}
	}
	return result, err
}

func (e *Engine) doSync(ctx context.Context, start time.Time) (*SyncResult, error) {
	cats, err := e.PartDB.AllCategories()
	if err != nil {
		return nil, fmt.Errorf("fetching partdb categories: %w", err)
	}
	parts, err := e.PartDB.AllParts()
	if err != nil {
		return nil, fmt.Errorf("fetching partdb parts: %w", err)
	}
	lots, err := e.PartDB.AllPartLots()
	if err != nil {
		return nil, fmt.Errorf("fetching partdb part_lots: %w", err)
	}

	var existing struct {
		CategoryIDs []int `json:"category_ids"`
		SPUIDs      []int `json:"spu_ids"`
	}
	if err := e.WMS.RunScriptJSON(ctx, existingIDsScript(e.WMS.DBPath), &existing); err != nil {
		return nil, fmt.Errorf("fetching existing modernwms ids: %w", err)
	}

	activeCatIDs := make([]int, len(cats))
	catRows := make([]categoryRow, len(cats))
	for i, c := range cats {
		activeCatIDs[i] = c.ID
		parentID := 0
		if c.ParentID.Valid {
			parentID = int(c.ParentID.Int64)
		}
		catRows[i] = categoryRow{ID: c.ID, Name: c.Name, ParentID: parentID}
	}

	activePartIDs := make([]int, len(parts))
	spuRows := make([]spuRow, len(parts))
	for i, p := range parts {
		specCode := p.Name
		descSource := DeriveDescriptionSource(p.Description, p.Comment, specCode)
		code := DeriveSPUCode(p.MfgPN, p.IPN, specCode, p.ID)
		name := DeriveSPUName(specCode, descSource)
		desc := DeriveSPUDescription(specCode, descSource)
		gtin := DeriveGTIN(p.GTIN, p.MfgPN, code)
		mass := "0"
		if p.Mass.Valid {
			mass = fmt.Sprintf("%g", p.Mass.Float64)
		}
		activePartIDs[i] = p.ID
		spuRows[i] = spuRow{
			ID: p.ID, Code: code, Name: name, Description: desc,
			BarCode: gtin, CategoryID: p.CategoryID, Mass: mass,
		}
	}

	lotPartIDs := make([]int, len(lots))
	lotAmounts := make([]float64, len(lots))
	for i, l := range lots {
		lotPartIDs[i] = l.PartID
		lotAmounts[i] = l.Amount
	}
	stock := AggregateStock(activePartIDs, lotPartIDs, lotAmounts)

	payload := syncPayload{
		Now:                 start.Format("2006-01-02 15:04:05.000000"),
		Categories:          catRows,
		CategorySoftDeletes: SoftDeleteTargets(existing.CategoryIDs, activeCatIDs),
		SPUs:                spuRows,
		SPUSoftDeletes:      SoftDeleteTargets(existing.SPUIDs, activePartIDs),
		Stock:               stock,
		ViewOnlyAuth:        config.Get(config.SyncViewOnlyAuth),
		ViewOnlyEmail:       config.Get(config.SyncViewOnlyEmail),
	}

	script, err := buildApplyScript(e.WMS.DBPath, payload)
	if err != nil {
		return nil, fmt.Errorf("building sync script: %w", err)
	}

	var summary struct {
		SyncedCategories   int `json:"synced_categories"`
		SyncedLocations    int `json:"synced_locations"`
		SyncedParts        int `json:"synced_parts"`
		SyncedStockRecords int `json:"synced_stock_records"`
		TotalStockQty      int `json:"total_stock_qty"`
	}
	if err := e.WMS.RunScriptJSON(ctx, script, &summary); err != nil {
		return nil, fmt.Errorf("applying sync to modernwms: %w", err)
	}

	return &SyncResult{
		Timestamp:          start,
		DurationMS:         float64(time.Since(start).Microseconds()) / 1000.0,
		SyncedCategories:   summary.SyncedCategories,
		SyncedLocations:    summary.SyncedLocations,
		SyncedParts:        summary.SyncedParts,
		SyncedStockRecords: summary.SyncedStockRecords,
		TotalStockQty:      summary.TotalStockQty,
	}, nil
}

// BackgroundLoop matches the original's 2-second poll-and-resync loop.
func (e *Engine) BackgroundLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := e.SyncNow(ctx); err != nil {
			// Errors are recorded in Stats()/history; the loop itself must
			// keep running regardless, matching the original's daemon.
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Engine) ResetModernWMSPassword(ctx context.Context, newPassword, username string) error {
	return e.WMS.ResetPassword(ctx, username, auth.HashModernWMS(newPassword), false)
}
