package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ActionFilter holds all supported filter fields for ListActions.
type ActionFilter struct {
	Method       string
	Caller       string
	Contract     string
	TxParamKey   string
	TxParamVal   string
	EventName    string
	EventDataKey string 
	EventDataVal string 
	FromBlock    int64
	ToBlock      int64
	FromTime     time.Time
	ToTime       time.Time
}

// CursorVal is the keyset pagination cursor — holds the last seen (block_num, id).
type CursorVal struct {
	BlockNum int64
	ID       int64
}

// ListActionsResult is the paginated response from ListActions.
type ListActionsResult struct {
	Rows       []ActionRow
	NextCursor *CursorVal // nil means no further pages
}

// ActionRow is a single decoded action row returned to callers.
type ActionRow struct {
	ID        int64
	BlockNum  int64
	BlockTime time.Time
	TxHash    string
	Method    string
	Caller    string
	Contract  string
	TxParams  json.RawMessage
	Events    json.RawMessage
	Value     string
}

// argList builds a positional $N parameter list for PostgreSQL.
// Call add(v) to append a value — it returns the correct "$N" placeholder.
type argList struct {
	vals []any
}

func (a *argList) add(v any) string {
	a.vals = append(a.vals, v)
	return fmt.Sprintf("$%d", len(a.vals))
}

func (a *argList) values() []any { return a.vals }

// jsonQuote returns a JSON-encoded string literal safe for embedding inside
// a jsonb literal that is passed as a query parameter.
func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

const actionSelectCols = `
	SELECT
		a.id,
		a.block_num,
		b.timestamp                             AS block_time,
		encode(a.tx_hash,   'hex')              AS tx_hash,
		COALESCE(a.method,  '')                 AS method,
		encode(a.from_addr, 'hex')              AS caller,
		encode(a.contract,  'hex')              AS contract,
		COALESCE(a.tx_params, '{}'::jsonb)      AS tx_params,
		COALESCE(a.events,   '[]'::jsonb)       AS events,
		COALESCE(a.value::text, '0')            AS value
	FROM actions a
	INNER JOIN blocks b ON b.num = a.block_num
`

func scanAction(row interface {
	Scan(...any) error
}) (*ActionRow, error) {
	var r ActionRow
	var txp, evs []byte
	if err := row.Scan(
		&r.ID, &r.BlockNum, &r.BlockTime,
		&r.TxHash, &r.Method, &r.Caller, &r.Contract,
		&txp, &evs, &r.Value,
	); err != nil {
		return nil, err
	}
	r.TxHash = "0x" + r.TxHash
	r.Caller = "0x" + r.Caller
	r.Contract = "0x" + r.Contract
	r.TxParams = json.RawMessage(txp)
	r.Events = json.RawMessage(evs)
	return &r, nil
}

func (db *DB) ListActions(ctx context.Context, f ActionFilter, after *CursorVal, limit int) (ListActionsResult, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	var args argList
	var conds []string

	// ── Keyset pagination cursor ─────────────────────────────────────────────
	if after != nil {
		p1 := args.add(after.BlockNum)
		p2 := args.add(after.ID)
		conds = append(conds, fmt.Sprintf("(a.block_num, a.id) > (%s, %s)", p1, p2))
	}

	// ── Scalar filters ───────────────────────────────────────────────────────
	if f.Method != "" {
		conds = append(conds, fmt.Sprintf("a.method = %s", args.add(f.Method)))
	}
	if f.Caller != "" {
		conds = append(conds, fmt.Sprintf("a.from_addr = %s",
			args.add(common.HexToAddress(f.Caller).Bytes())))
	}
	if f.Contract != "" {
		conds = append(conds, fmt.Sprintf("a.contract = %s",
			args.add(common.HexToAddress(f.Contract).Bytes())))
	}

	if f.TxParamKey != "" && f.TxParamVal != "" {
		var jsonVal any
		if err := json.Unmarshal([]byte(f.TxParamVal), &jsonVal); err != nil {
			jsonVal = f.TxParamVal
		}
		valJSON, _ := json.Marshal(jsonVal)

		p1 := args.add(f.TxParamKey)
		p2 := args.add(f.TxParamVal)
		p3 := args.add(f.TxParamKey)
		p4 := args.add(f.TxParamVal)
		ginCond := fmt.Sprintf(
			"(a.tx_params @> jsonb_build_object(%s::text, to_jsonb(%s::text))"+
				" OR a.tx_params @> jsonb_build_object(%s::text, jsonb_build_array(to_jsonb(%s::text))))",
			p1, p2, p3, p4,
		)

		escapedKey := strings.ReplaceAll(f.TxParamKey, `"`, `""`)
		path := fmt.Sprintf(`$.**."%s" ? (@ == $val)`, escapedKey)
		p5 := args.add(path)
		p6 := args.add(string(valJSON))
		jsonPathCond := fmt.Sprintf(
			"jsonb_path_exists(a.tx_params, %s::jsonpath, jsonb_build_object('val', %s::jsonb))",
			p5, p6,
		)

		conds = append(conds, fmt.Sprintf("(%s OR %s)", ginCond, jsonPathCond))
	}

	// ── events JSONB search ──────────────────────────────────────────────────
	if f.EventName != "" {
		literal := fmt.Sprintf(`[{"event_name": %s}]`, jsonQuote(f.EventName))
		conds = append(conds, fmt.Sprintf(
			"a.events @> %s::jsonb", args.add(literal),
		))
	}
	if f.EventDataKey != "" && f.EventDataVal != "" {
		escapedKey := strings.ReplaceAll(f.EventDataKey, `"`, `""`)
		path := fmt.Sprintf(`$[*].data."%s" ? (@ == $val)`, escapedKey)
		p1 := args.add(path)
		p2 := args.add(f.EventDataVal)
		conds = append(conds, fmt.Sprintf(
			"jsonb_path_exists(a.events, %s::jsonpath, jsonb_build_object('val', to_jsonb(%s::text)))",
			p1, p2,
		))
	}

	// ── Block range ──────────────────────────────────────────────────────────
	if f.FromBlock > 0 {
		conds = append(conds, fmt.Sprintf("a.block_num >= %s", args.add(f.FromBlock)))
	}
	if f.ToBlock > 0 {
		conds = append(conds, fmt.Sprintf("a.block_num <= %s", args.add(f.ToBlock)))
	}

	// ── Time range ───────────────────────────────────────────────────────────
	if !f.FromTime.IsZero() {
		conds = append(conds, fmt.Sprintf("b.timestamp >= %s", args.add(f.FromTime)))
	}
	if !f.ToTime.IsZero() {
		conds = append(conds, fmt.Sprintf("b.timestamp <= %s", args.add(f.ToTime)))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	limitP := args.add(limit + 1)
	query := fmt.Sprintf(`%s %s ORDER BY a.block_num ASC, a.id ASC LIMIT %s`,
		actionSelectCols, where, limitP)

	rows, err := db.conn.QueryContext(ctx, query, args.values()...)
	if err != nil {
		return ListActionsResult{}, fmt.Errorf("ListActions query: %w", err)
	}
	defer func() {
	_ = rows.Close()
	}()

	var result []ActionRow
	for rows.Next() {
		r, err := scanAction(rows)
		if err != nil {
			return ListActionsResult{}, fmt.Errorf("ListActions scan: %w", err)
		}
		result = append(result, *r)
	}
	if err := rows.Err(); err != nil {
		return ListActionsResult{}, fmt.Errorf("ListActions rows: %w", err)
	}

	var next *CursorVal
	if len(result) > limit {
		last := result[limit-1]
		next = &CursorVal{BlockNum: last.BlockNum, ID: last.ID}
		result = result[:limit]
	}

	return ListActionsResult{Rows: result, NextCursor: next}, nil
}

// GetActionByID returns one action by its composite PK (block_num, id).
// Returns nil, nil when the row does not exist.
func (db *DB) GetActionByID(ctx context.Context, blockNum, id int64) (*ActionRow, error) {
	query := fmt.Sprintf(`%s WHERE a.block_num = $1 AND a.id = $2`, actionSelectCols)

	r, err := scanAction(db.conn.QueryRowContext(ctx, query, blockNum, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetActionByID: %w", err)
	}
	return r, nil
}