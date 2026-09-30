package records

import (
	"context"
	"sync"
	"time"

	"cardflow-backend/internal/crm/platform"
	"github.com/google/uuid"
)

// A product's setup names its lead statuses and its opportunity pipeline (D-68), so
// "Sales process" in the setup decides the pick-lists people see: the Lead
// status of leads and the Stage of opportunities, in that product only.

type setupCacheEntry struct {
	cfg platform.ProductConfig
	at  time.Time
}

var setupCache = struct {
	sync.Mutex
	m map[uuid.UUID]setupCacheEntry
}{m: map[uuid.UUID]setupCacheEntry{}}

// ForgetSetup drops cached setups (after a setup is published or assigned).
func ForgetSetup() {
	setupCache.Lock()
	setupCache.m = map[uuid.UUID]setupCacheEntry{}
	setupCache.Unlock()
}

func cachedSetup(ctx context.Context, q querier, ws uuid.UUID) (platform.ProductConfig, bool) {
	setupCache.Lock()
	e, ok := setupCache.m[ws]
	setupCache.Unlock()
	if ok && time.Since(e.at) < 30*time.Second {
		return e.cfg, true
	}
	cfg, err := platform.WorkspaceSetup(ctx, q, ws)
	if err != nil {
		return cfg, false
	}
	setupCache.Lock()
	setupCache.m[ws] = setupCacheEntry{cfg: cfg, at: time.Now()}
	setupCache.Unlock()
	return cfg, true
}

var leadTones = map[string]string{"new": "primary", "working": "warning", "qualified": "success", "converted": "success", "lost": "danger"}

// statusesFor is an object's status options in a workspace: the setup's list for leads
// and opportunities, otherwise the object's own.
func statusesFor(ctx context.Context, q querier, ws uuid.UUID, spec *objectSpec) []StatusOption {
	if spec.StatusField == "" || (spec.Key != "leads" && spec.Key != "opportunities") || ws == uuid.Nil {
		return spec.Statuses
	}
	cfg, ok := cachedSetup(ctx, q, ws)
	if !ok {
		return spec.Statuses
	}
	out := []StatusOption{}
	switch spec.Key {
	case "leads":
		if len(cfg.LeadStatuses) == 0 {
			return spec.Statuses
		}
		seen := map[string]bool{}
		for _, o := range cfg.LeadStatuses {
			tone := leadTones[o.Value]
			if tone == "" {
				tone = "neutral"
			}
			out = append(out, StatusOption{Option{o.Value, o.Label}, tone})
			seen[o.Value] = true
		}
		// Conversion needs these two, even when a setup leaves them out.
		if !seen["converted"] {
			out = append(out, StatusOption{Option{"converted", "Converted"}, "success"})
		}
		if !seen["lost"] {
			out = append(out, StatusOption{Option{"lost", "Closed lost"}, "danger"})
		}
	case "opportunities":
		if len(cfg.PipelineStages) == 0 {
			return spec.Statuses
		}
		for _, s := range cfg.PipelineStages {
			tone := "primary"
			switch {
			case s.Probability >= 100:
				tone = "success"
			case s.Probability == 0 && s.Key != cfg.PipelineStages[0].Key:
				tone = "danger"
			case s.Probability >= 60:
				tone = "warning"
			}
			out = append(out, StatusOption{Option{s.Key, s.Label}, tone})
		}
	}
	return out
}

// stageProbability is the win probability the setup gives an opportunity stage.
func stageProbability(ctx context.Context, q querier, ws uuid.UUID, stage string) (int, bool) {
	cfg, ok := cachedSetup(ctx, q, ws)
	if !ok {
		return 0, false
	}
	for _, s := range cfg.PipelineStages {
		if s.Key == stage {
			return s.Probability, true
		}
	}
	return 0, false
}
