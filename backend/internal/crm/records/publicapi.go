package records

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/jackc/pgx/v5"
)

// Public API (D-61): the REST endpoints the web app uses are the API — with an API key
// they're reached at /api/crm/v1/w/<code>/crm/<object>… — plus a GraphQL endpoint and
// an OpenAPI description, both generated from the workspace's own objects and the
// fields the caller may see, so custom objects and fields appear automatically.

var nonIdent = regexp.MustCompile(`[^A-Za-z0-9]+`)

func pascal(s string) string {
	parts := nonIdent.Split(s, -1)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	out := b.String()
	if out == "" || unicode.IsDigit(rune(out[0])) {
		out = "X" + out
	}
	return out
}

func camel(s string) string {
	p := pascal(s)
	r := []rune(p)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// apiObjects are the objects the caller can read in this workspace.
func apiObjects(sc *Scope) []*objectSpec {
	out := []*objectSpec{}
	all := []*objectSpec{&leadSpec, &accountSpec, &contactSpec}
	all = append(all, dynamicSpecs()...)
	for _, s := range all {
		if sc.Enabled(s.Key) && sc.Can(s.Key, "read") {
			out = append(out, s)
		}
	}
	return out
}

// ---- GraphQL ----

var jsonScalar = graphql.NewScalar(graphql.ScalarConfig{
	Name:        "JSON",
	Description: "Any JSON value",
	Serialize:   func(v any) any { return v },
	ParseValue:  func(v any) any { return v },
	ParseLiteral: func(v ast.Value) any {
		return parseLiteral(v)
	},
})

func parseLiteral(v ast.Value) any {
	switch x := v.(type) {
	case *ast.StringValue:
		return x.Value
	case *ast.IntValue:
		n, _ := asNumber(x.Value)
		return n
	case *ast.FloatValue:
		n, _ := asNumber(x.Value)
		return n
	case *ast.BooleanValue:
		return x.Value
	case *ast.ListValue:
		out := []any{}
		for _, item := range x.Values {
			out = append(out, parseLiteral(item))
		}
		return out
	case *ast.ObjectValue:
		out := map[string]any{}
		for _, f := range x.Fields {
			out[f.Name.Value] = parseLiteral(f.Value)
		}
		return out
	}
	return nil
}

func gqlType(f Field) graphql.Output {
	switch valueKind(f) {
	case "num":
		return graphql.Float
	case "bool":
		return graphql.Boolean
	case "text", "select", "lookup", "date", "time":
		return graphql.String
	}
	return jsonScalar
}

func rowMap(r *Row) map[string]any {
	m := rowSnapshot(r)
	m["createdAt"], m["updatedAt"] = r.CreatedAt, r.UpdatedAt
	for k, l := range r.Lookups {
		m[k+"Label"] = l.Label
	}
	return m
}

func (h *Handler) graphQLSchema(ctx context.Context, sc *Scope) (graphql.Schema, error) {
	query := graphql.Fields{}
	mutation := graphql.Fields{}
	me := identityOf(ctx)
	for _, spec := range apiObjects(sc) {
		spec := spec
		fields, err := allFields(ctx, h.store.Pool, sc.WS, spec)
		if err != nil {
			return graphql.Schema{}, err
		}
		tf := graphql.Fields{
			"id":          &graphql.Field{Type: graphql.NewNonNull(graphql.ID)},
			"code":        &graphql.Field{Type: graphql.String},
			"displayName": &graphql.Field{Type: graphql.String, Description: "The record's name"},
			"version":     &graphql.Field{Type: graphql.Int},
			"createdAt":   &graphql.Field{Type: graphql.String},
			"updatedAt":   &graphql.Field{Type: graphql.String},
		}
		for _, f := range fields {
			name := camel(f.Key)
			if _, taken := tf[name]; taken {
				continue
			}
			tf[name] = &graphql.Field{Type: gqlType(f), Description: f.Label, Resolve: func(key string) graphql.FieldResolveFn {
				return func(p graphql.ResolveParams) (any, error) {
					if m, ok := p.Source.(map[string]any); ok {
						return m[key], nil
					}
					return nil, nil
				}
			}(f.Key)}
			if f.Type == "lookup" {
				tf[name+"Label"] = &graphql.Field{Type: graphql.String, Description: f.Label + " (name)", Resolve: func(key string) graphql.FieldResolveFn {
					return func(p graphql.ResolveParams) (any, error) {
						if m, ok := p.Source.(map[string]any); ok {
							return m[key+"Label"], nil
						}
						return nil, nil
					}
				}(f.Key)}
			}
		}
		typeName := pascal(spec.Singular)
		obj := graphql.NewObject(graphql.ObjectConfig{Name: typeName, Fields: tf, Description: spec.Plural})
		conn := graphql.NewObject(graphql.ObjectConfig{Name: typeName + "Connection", Fields: graphql.Fields{
			"totalCount": &graphql.Field{Type: graphql.Int},
			"nodes":      &graphql.Field{Type: graphql.NewList(obj)},
		}})
		plural := camel(spec.Key)
		single := camel(spec.Singular)
		if single == plural {
			single += "Record"
		}
		query[plural] = &graphql.Field{Type: conn, Description: "List " + spec.Plural,
			Args: graphql.FieldConfigArgument{
				"filter": &graphql.ArgumentConfig{Type: jsonScalar, Description: `Filter tree, e.g. {"op":"and","filters":[{"field":"status","op":"eq","value":"new"}]}`},
				"sorts":  &graphql.ArgumentConfig{Type: jsonScalar, Description: `e.g. [{"field":"createdAt","dir":"desc"}]`},
				"search": &graphql.ArgumentConfig{Type: graphql.String},
				"first":  &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 50},
				"offset": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 0},
			},
			Resolve: func(p graphql.ResolveParams) (any, error) {
				lp := listParams{Q: asString(p.Args["search"]), Owners: sc.OwnersFor(spec.Key, me), Env: filterEnv{Me: me}}
				if n, ok := p.Args["first"].(int); ok {
					lp.Limit = n
				}
				if lp.Limit < 1 || lp.Limit > 200 {
					lp.Limit = 50
				}
				if n, ok := p.Args["offset"].(int); ok && n >= 0 {
					lp.Offset = n
				}
				if raw, ok := p.Args["filter"]; ok && raw != nil {
					b, _ := json.Marshal(raw)
					var fnode FilterNode
					if err := json.Unmarshal(b, &fnode); err != nil {
						return nil, errors.New("filter isn't valid")
					}
					lp.Filter = &fnode
				}
				if raw, ok := p.Args["sorts"]; ok && raw != nil {
					b, _ := json.Marshal(raw)
					_ = json.Unmarshal(b, &lp.Sorts)
				}
				rows, total, err := h.list(p.Context, sc.WS, spec, lp)
				if err != nil {
					return nil, errors.New(errMessageOrText(err))
				}
				nodes := make([]any, len(rows))
				for i := range rows {
					nodes[i] = rowMap(&rows[i])
				}
				return map[string]any{"totalCount": total, "nodes": nodes}, nil
			}}
		query[single] = &graphql.Field{Type: obj, Args: graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)}},
			Resolve: func(p graphql.ResolveParams) (any, error) {
				id, err := uuid.Parse(asString(p.Args["id"]))
				if err != nil {
					return nil, errors.New("not found")
				}
				row, _, err := h.getRow(p.Context, h.store.Pool, sc.WS, spec, id, sc.OwnersFor(spec.Key, me))
				if err != nil {
					return nil, errors.New("not found")
				}
				return rowMap(row), nil
			}}
		write := func(action string, fn func(p graphql.ResolveParams, tx pgx.Tx, a actorInfo) (*Row, error)) graphql.FieldResolveFn {
			return func(p graphql.ResolveParams) (any, error) {
				if !sc.Can(spec.Key, action) {
					return nil, errors.New("you don't have permission to do that")
				}
				a := actorInfo{ID: &me, Kind: "api_key", Source: "api"}
				if apiKeyFrom(p.Context) == nil {
					a.Kind = "identity"
				}
				var row *Row
				err := h.store.WithTx(p.Context, func(tx pgx.Tx) error {
					var err error
					row, err = fn(p, tx, a)
					return err
				})
				if err != nil {
					return nil, errors.New(errMessageOrText(err))
				}
				h.bus.Kick()
				if row == nil {
					return true, nil
				}
				return rowMap(row), nil
			}
		}
		idArg := graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)}}
		mutation["create"+pascal(spec.Singular)] = &graphql.Field{Type: obj,
			Args: graphql.FieldConfigArgument{"values": &graphql.ArgumentConfig{Type: graphql.NewNonNull(jsonScalar)}},
			Resolve: write("create", func(p graphql.ResolveParams, tx pgx.Tx, a actorInfo) (*Row, error) {
				vals, _ := p.Args["values"].(map[string]any)
				return h.createRecord(p.Context, tx, sc.WS, spec, a, vals)
			})}
		mutation["update"+pascal(spec.Singular)] = &graphql.Field{Type: obj,
			Args: graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.ID)},
				"values": &graphql.ArgumentConfig{Type: graphql.NewNonNull(jsonScalar)}, "expectedVersion": &graphql.ArgumentConfig{Type: graphql.Int}},
			Resolve: write("update", func(p graphql.ResolveParams, tx pgx.Tx, a actorInfo) (*Row, error) {
				id, err := uuid.Parse(asString(p.Args["id"]))
				if err != nil {
					return nil, shared.NotFound("record_not_found")
				}
				vals, _ := p.Args["values"].(map[string]any)
				var expected *int
				if v, ok := p.Args["expectedVersion"].(int); ok {
					expected = &v
				}
				return h.updateValues(p.Context, tx, sc.WS, spec, id, a, vals, expected, sc.OwnersFor(spec.Key, me))
			})}
		mutation["delete"+pascal(spec.Singular)] = &graphql.Field{Type: graphql.Boolean, Args: idArg,
			Resolve: write("delete", func(p graphql.ResolveParams, tx pgx.Tx, a actorInfo) (*Row, error) {
				id, err := uuid.Parse(asString(p.Args["id"]))
				if err != nil {
					return nil, shared.NotFound("record_not_found")
				}
				return nil, h.deleteRecord(p.Context, tx, sc.WS, spec, id, a, sc.OwnersFor(spec.Key, me))
			})}
		mutation["restore"+pascal(spec.Singular)] = &graphql.Field{Type: obj, Args: idArg,
			Resolve: write("delete", func(p graphql.ResolveParams, tx pgx.Tx, a actorInfo) (*Row, error) {
				id, err := uuid.Parse(asString(p.Args["id"]))
				if err != nil {
					return nil, shared.NotFound("record_not_found")
				}
				return h.restoreRecord(p.Context, tx, sc.WS, spec, id, a, sc.OwnersFor(spec.Key, me))
			})}
	}
	if len(query) == 0 {
		query["ping"] = &graphql.Field{Type: graphql.String, Resolve: func(graphql.ResolveParams) (any, error) { return "pong", nil }}
	}
	cfg := graphql.SchemaConfig{Query: graphql.NewObject(graphql.ObjectConfig{Name: "Query", Fields: query})}
	if len(mutation) > 0 {
		cfg.Mutation = graphql.NewObject(graphql.ObjectConfig{Name: "Mutation", Fields: mutation})
	}
	return graphql.NewSchema(cfg)
}

func identityOf(ctx context.Context) uuid.UUID {
	if s := identity.SessionFrom(ctx); s != nil {
		return s.IdentityID
	}
	return uuid.Nil
}

func (h *Handler) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in struct {
		Query         string         `json:"query"`
		Variables     map[string]any `json:"variables"`
		OperationName string         `json:"operationName"`
	}
	if err := shared.DecodeJSONLimit(w, r, &in, 1<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	schema, err := h.graphQLSchema(r.Context(), sc)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res := graphql.Do(graphql.Params{Schema: schema, RequestString: in.Query, VariableValues: in.Variables, OperationName: in.OperationName, Context: r.Context()})
	shared.WriteJSON(w, http.StatusOK, res)
}

// GET /graphql/schema — the schema as SDL-like JSON (introspection result).
func (h *Handler) handleGraphQLSchema(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	schema, err := h.graphQLSchema(r.Context(), sc)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	type fieldDoc struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	out := map[string][]fieldDoc{}
	for _, root := range []*graphql.Object{schema.QueryType(), schema.MutationType()} {
		if root == nil {
			continue
		}
		for name, f := range root.Fields() {
			out[root.Name()] = append(out[root.Name()], fieldDoc{Name: name, Type: f.Type.String()})
		}
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// ---- OpenAPI ----

func openAPIType(f Field) map[string]any {
	switch f.Type {
	case "number", "currency", "percent":
		return map[string]any{"type": "number"}
	case "rating":
		return map[string]any{"type": "integer", "minimum": 1, "maximum": 5}
	case "boolean":
		return map[string]any{"type": "boolean"}
	case "date":
		return map[string]any{"type": "string", "format": "date"}
	case "datetime":
		return map[string]any{"type": "string", "format": "date-time"}
	case "email":
		return map[string]any{"type": "string", "format": "email"}
	case "url":
		return map[string]any{"type": "string", "format": "uri"}
	case "lookup":
		return map[string]any{"type": "string", "format": "uuid", "description": "Id of a " + lookupLabel(f.Lookup) + " record"}
	case "relations":
		return map[string]any{"type": "array", "items": map[string]any{"type": "string", "format": "uuid"}}
	case "multiselect", "emails", "phones", "links":
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	case "select":
		vals := []string{}
		for _, o := range f.Options {
			vals = append(vals, o.Value)
		}
		return map[string]any{"type": "string", "enum": vals}
	case "address", "fullName", "json":
		return map[string]any{"type": "object"}
	case "files":
		return map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "readOnly": true}
	}
	return map[string]any{"type": "string"}
}

func (h *Handler) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	base := strings.TrimRight(h.cfg.BaseURL, "/") + "/api/crm/v1/w/" + sc.Code
	paths := map[string]any{}
	schemas := map[string]any{
		"Error": map[string]any{"type": "object", "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"},
			"fieldErrors": map[string]any{"type": "object"}, "requestId": map[string]any{"type": "string"}}},
		"FilterNode": map[string]any{"type": "object", "description": "A condition {field, op, value} or a group {op: and|or, filters: [...]}",
			"properties": map[string]any{"op": map[string]any{"type": "string"}, "field": map[string]any{"type": "string"}, "value": map[string]any{},
				"filters": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/FilterNode"}}}},
	}
	errResp := map[string]any{"description": "Error", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}}}}
	for _, spec := range apiObjects(sc) {
		fields, err := allFields(r.Context(), h.store.Pool, sc.WS, spec)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		name := pascal(spec.Singular)
		props := map[string]any{}
		required := []string{}
		for _, f := range fields {
			t := openAPIType(f)
			t["title"] = f.Label
			if f.ReadOnly {
				t["readOnly"] = true
			}
			props[f.Key] = t
			if f.Required && !f.ReadOnly {
				required = append(required, f.Key)
			}
		}
		recordSchema := map[string]any{"type": "object", "properties": map[string]any{
			"id": map[string]any{"type": "string", "format": "uuid"}, "code": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"},
			"version": map[string]any{"type": "integer"}, "values": map[string]any{"type": "object", "properties": props},
			"lookups": map[string]any{"type": "object"}, "createdAt": map[string]any{"type": "string", "format": "date-time"},
			"updatedAt": map[string]any{"type": "string", "format": "date-time"}}}
		schemas[name] = recordSchema
		schemas[name+"Values"] = map[string]any{"type": "object", "properties": props, "required": required}
		ref := func(n string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + n} }
		ok := func(schema map[string]any) map[string]any {
			return map[string]any{"description": "OK", "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
		}
		listParamsDoc := []any{
			map[string]any{"name": "q", "in": "query", "schema": map[string]any{"type": "string"}, "description": "Search"},
			map[string]any{"name": "filter", "in": "query", "schema": map[string]any{"type": "string"}, "description": "FilterNode as JSON"},
			map[string]any{"name": "sorts", "in": "query", "schema": map[string]any{"type": "string"}, "description": `JSON, e.g. [{"field":"createdAt","dir":"desc"}]`},
			map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "maximum": 200}},
			map[string]any{"name": "offset", "in": "query", "schema": map[string]any{"type": "integer"}},
		}
		idParam := map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string", "format": "uuid"}}
		tag := []string{spec.Plural}
		paths["/crm/"+spec.Key] = map[string]any{
			"get": map[string]any{"tags": tag, "summary": "List " + strings.ToLower(spec.Plural), "parameters": listParamsDoc,
				"responses": map[string]any{"200": ok(map[string]any{"type": "object", "properties": map[string]any{"data": map[string]any{"type": "array", "items": ref(name)}, "total": map[string]any{"type": "integer"}}}), "default": errResp}},
			"post": map[string]any{"tags": tag, "summary": "Create a " + strings.ToLower(spec.Singular),
				"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"values": ref(name + "Values")}}}}},
				"responses":   map[string]any{"201": ok(ref(name)), "default": errResp}},
		}
		paths["/crm/"+spec.Key+"/{id}"] = map[string]any{
			"get": map[string]any{"tags": tag, "summary": "Get a " + strings.ToLower(spec.Singular) + " with related records", "parameters": []any{idParam},
				"responses": map[string]any{"200": ok(map[string]any{"type": "object", "properties": map[string]any{"record": ref(name)}}), "default": errResp}},
			"patch": map[string]any{"tags": tag, "summary": "Change a " + strings.ToLower(spec.Singular), "parameters": []any{idParam},
				"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object",
					"required": []string{"values", "expectedVersion"}, "properties": map[string]any{"values": ref(name + "Values"), "expectedVersion": map[string]any{"type": "integer"}}}}}},
				"responses": map[string]any{"200": ok(ref(name)), "default": errResp}},
			"delete": map[string]any{"tags": tag, "summary": "Move to the recycle bin", "parameters": []any{idParam},
				"responses": map[string]any{"204": map[string]any{"description": "Deleted"}, "default": errResp}},
		}
		paths["/crm/"+spec.Key+"/{id}/restore"] = map[string]any{"post": map[string]any{"tags": tag, "summary": "Restore from the recycle bin", "parameters": []any{idParam},
			"responses": map[string]any{"200": ok(ref(name)), "default": errResp}}}
		paths["/crm/"+spec.Key+"/bulk"] = map[string]any{"post": map[string]any{"tags": tag, "summary": "Update, delete, restore or destroy many records",
			"requestBody": map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []string{"update", "delete", "restore", "destroy"}}, "values": ref(name + "Values"),
				"query": map[string]any{"type": "object", "properties": map[string]any{"ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "filter": ref("FilterNode")}}}}}}},
			"responses": map[string]any{"200": ok(map[string]any{"type": "object"}), "default": errResp}}}
	}
	paths["/search"] = map[string]any{"get": map[string]any{"tags": []string{"Search"}, "summary": "Search every object", "parameters": []any{
		map[string]any{"name": "q", "in": "query", "required": true, "schema": map[string]any{"type": "string"}}}, "responses": map[string]any{"200": map[string]any{"description": "OK"}}}}
	paths["/graphql"] = map[string]any{"post": map[string]any{"tags": []string{"GraphQL"}, "summary": "GraphQL endpoint (same objects and permissions)",
		"requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string"}, "variables": map[string]any{"type": "object"}}}}}}, "responses": map[string]any{"200": map[string]any{"description": "OK"}}}}
	doc := map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": sc.Name + " CRM API", "version": "1", "description": "Records of this workspace. Authenticate with an API key: Authorization: Bearer crm_…  (100 requests a minute)."},
		"servers": []any{map[string]any{"url": base}},
		"paths":   paths,
		"components": map[string]any{"schemas": schemas,
			"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"}}},
		"security": []any{map[string]any{"bearerAuth": []string{}}},
	}
	shared.WriteJSON(w, http.StatusOK, doc)
}
