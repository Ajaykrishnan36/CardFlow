package records

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Default list views (D-54), like Salesforce's standard list views: the first time a
// workspace opens an object's views it gets a few shared ones. People can edit or delete
// them; they aren't recreated (crm.connector_state remembers).

type seedView struct {
	name, kind string
	def        ViewDefinition
}

func cond(field, op string, value any) FilterNode {
	return FilterNode{Field: field, Op: op, Value: value}
}
func all(nodes ...FilterNode) *FilterNode { return &FilterNode{Op: "and", Filters: nodes} }

func defaultViewsFor(object string) []seedView {
	desc := []SortSpec{{Field: "createdAt", Dir: "desc"}}
	switch object {
	case "leads":
		return []seedView{
			{"My open leads", "table", ViewDefinition{Filter: all(cond("ownerId", "isMe", nil), cond("status", "notIn", []any{"converted", "lost"})), Sorts: desc}},
			{"Lead pipeline", "kanban", ViewDefinition{GroupBy: "status", Filter: all(cond("status", "notIn", []any{"converted"}))}},
			{"New this week", "table", ViewDefinition{Filter: all(cond("createdAt", "thisWeek", nil)), Sorts: desc}},
			{"Converted", "table", ViewDefinition{Filter: all(cond("status", "eq", "converted")), Sorts: []SortSpec{{Field: "convertedAt", Dir: "desc"}}}},
		}
	case "accounts":
		return []seedView{
			{"Business accounts", "table", ViewDefinition{Filter: all(cond("kind", "eq", "business")), Sorts: []SortSpec{{Field: "title", Dir: "asc"}}}},
			{"Personal accounts (older)", "table", ViewDefinition{Filter: all(cond("kind", "eq", "individual")), Sorts: desc}},
			{"My accounts", "table", ViewDefinition{Filter: all(cond("ownerId", "isMe", nil)), Sorts: []SortSpec{{Field: "title", Dir: "asc"}}}},
		}
	case "contacts":
		return []seedView{
			{"My contacts", "table", ViewDefinition{Filter: all(cond("ownerId", "isMe", nil)), Sorts: desc}},
			{"Without an account", "table", ViewDefinition{Filter: all(cond("accountId", "empty", nil)), Sorts: desc}},
		}
	case "opportunities":
		return []seedView{
			{"Pipeline", "kanban", ViewDefinition{GroupBy: "status", Aggregates: []string{"amount:sum"}}},
			{"My open deals", "table", ViewDefinition{Filter: all(cond("ownerId", "isMe", nil), cond("status", "notIn", []any{"closed_won", "closed_lost"})), Sorts: []SortSpec{{Field: "closeDate", Dir: "asc"}}, Aggregates: []string{"amount:sum"}}},
			{"Closing this month", "table", ViewDefinition{Filter: all(cond("closeDate", "thisMonth", nil)), Sorts: []SortSpec{{Field: "closeDate", Dir: "asc"}}, Aggregates: []string{"amount:sum"}}},
			{"Won", "table", ViewDefinition{Filter: all(cond("status", "eq", "closed_won")), Sorts: []SortSpec{{Field: "closeDate", Dir: "desc"}}, Aggregates: []string{"amount:sum"}}},
		}
	case "tasks":
		return []seedView{
			{"My open tasks", "table", ViewDefinition{Filter: all(cond("ownerId", "isMe", nil), cond("status", "notIn", []any{"completed", "deferred"})), Sorts: []SortSpec{{Field: "dueDate", Dir: "asc"}}}},
			{"Overdue", "table", ViewDefinition{Filter: all(cond("dueDate", "overdue", nil), cond("status", "notIn", []any{"completed", "deferred"})), Sorts: []SortSpec{{Field: "dueDate", Dir: "asc"}}}},
			{"Task board", "kanban", ViewDefinition{GroupBy: "status"}},
		}
	case "events":
		return []seedView{
			{"Calendar", "calendar", ViewDefinition{CalendarField: "startsAt", CalendarMode: "month"}},
			{"Upcoming", "table", ViewDefinition{Filter: all(cond("startsAt", "nextDays", 30)), Sorts: []SortSpec{{Field: "startsAt", Dir: "asc"}}}},
		}
	case "cases":
		return []seedView{
			{"Open cases", "table", ViewDefinition{Filter: all(cond("status", "notIn", []any{"resolved", "closed"})), Sorts: desc}},
			{"Case board", "kanban", ViewDefinition{GroupBy: "status"}},
		}
	case "subscriptions":
		return []seedView{
			{"Active", "table", ViewDefinition{Filter: all(cond("status", "eq", "active")), Sorts: []SortSpec{{Field: "endDate", Dir: "asc"}}}},
			{"Renewing in 30 days", "table", ViewDefinition{Filter: all(cond("endDate", "nextDays", 30)), Sorts: []SortSpec{{Field: "endDate", Dir: "asc"}}}},
		}
	}
	return nil
}

// ensureDefaultViews creates an object's standard views once per workspace.
func (h *Handler) ensureDefaultViews(ctx context.Context, ws uuid.UUID, spec *objectSpec) {
	seeds := defaultViewsFor(spec.Key)
	if len(seeds) == 0 {
		return
	}
	key := "views_seeded:" + ws.String() + ":" + spec.Key
	tag, err := h.store.Pool.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, '{}') ON CONFLICT (key) DO NOTHING`, key)
	if err != nil || tag.RowsAffected() == 0 {
		return
	}
	fields, err := allFieldsRaw(ctx, h.store.Pool, ws, spec)
	if err != nil {
		return
	}
	for i, v := range seeds {
		// Skip a view whose fields this object doesn't have (an edited standard object).
		if _, fe := compileFilter(v.def.Filter, fields, filterEnv{}, &sqlBuilder{}); len(fe) > 0 {
			continue
		}
		if v.def.GroupBy != "" {
			if _, ok := findField(fields, v.def.GroupBy); !ok {
				continue
			}
		}
		raw, _ := json.Marshal(v.def)
		_, _ = h.store.Pool.Exec(ctx, `INSERT INTO crm.views (workspace_id, object_key, name, kind, visibility, definition, position) VALUES ($1, $2, $3, $4, 'shared', $5, $6)`,
			ws, spec.Key, v.name, v.kind, raw, i)
	}
}
