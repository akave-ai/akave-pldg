package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akave-ai/akavelog/internal/model"
	"github.com/akave-ai/akavelog/internal/query"
	"github.com/akave-ai/akavelog/internal/response"
	"github.com/labstack/echo/v4"
)

// QueryHandler handles POST /query and GET /query/stream.
type QueryHandler struct {
	engine *query.Engine
}

// NewQueryHandler creates a handler backed by the given query engine.
func NewQueryHandler(engine *query.Engine) *QueryHandler {
	return &QueryHandler{engine: engine}
}

// Handle executes a log query and returns all matching entries as JSON.
//
// POST /query
// Content-Type: application/json
// Body: model.QueryRequest (all fields optional)
// Response: model.QueryResponse
func (h *QueryHandler) Handle(c echo.Context) error {
	var req model.QueryRequest
	if err := c.Bind(&req); err != nil {
		return response.BadRequest(c, "invalid request body", err.Error())
	}
	if !req.TsStart.IsZero() && !req.TsEnd.IsZero() && req.TsStart.After(req.TsEnd) {
		return response.BadRequest(c, "invalid time range", "time_start must be before time_end")
	}

	resp, err := h.engine.Query(c.Request().Context(), req)
	if err != nil {
		return response.InternalError(c, "query failed", err.Error())
	}

	return c.JSON(http.StatusOK, resp)
}

// HandleSSE executes a log query and streams matching entries as Server-Sent Events.
// Each SSE "log" event carries one JSON-encoded QueryResultEntry.
// A final "done" event carries {"count":N,"truncated":bool}.
//
// GET /query/stream?tenant=&service=&levels=error,warn&keyword=&from=RFC3339&to=RFC3339&limit=100
func (h *QueryHandler) HandleSSE(c echo.Context) error {
	req, err := parseSSEParams(c)
	if err != nil {
		return response.BadRequest(c, "invalid query params", err.Error())
	}

	w := c.Response().Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	c.Response().WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	resp, err := h.engine.Query(c.Request().Context(), req)
	if err != nil {
		writeSSEEvent(w, "error", `{"error":"`+jsonEscape(err.Error())+`"}`)
		if canFlush {
			flusher.Flush()
		}
		return nil
	}

	for i := range resp.Results {
		b, err := json.Marshal(resp.Results[i])
		if err != nil {
			continue
		}
		writeSSEEvent(w, "log", string(b))
		if canFlush {
			flusher.Flush()
		}
	}

	writeSSEEvent(w, "done",
		fmt.Sprintf(`{"count":%d,"truncated":%s}`, resp.Count, boolStr(resp.Truncated)),
	)
	if canFlush {
		flusher.Flush()
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeSSEEvent(w http.ResponseWriter, event, data string) {
	_, _ = w.Write([]byte("event: " + event + "\ndata: " + data + "\n\n"))
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// jsonEscape escapes backslashes and double-quotes for embedding in a JSON string literal.
func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func parseSSEParams(c echo.Context) (model.QueryRequest, error) {
	var req model.QueryRequest
	req.Tenant = c.QueryParam("tenant")
	req.Service = c.QueryParam("service")
	req.Keyword = c.QueryParam("keyword")

	if lvls := c.QueryParam("levels"); lvls != "" {
		for _, l := range strings.Split(lvls, ",") {
			if t := strings.TrimSpace(l); t != "" {
				req.Levels = append(req.Levels, t)
			}
		}
	}
	if s := c.QueryParam("from"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return req, fmt.Errorf("invalid from: %w", err)
		}
		req.TsStart = t
	}
	if s := c.QueryParam("to"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return req, fmt.Errorf("invalid to: %w", err)
		}
		req.TsEnd = t
	}
	if s := c.QueryParam("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return req, fmt.Errorf("invalid limit: must be a positive integer")
		}
		req.Limit = n
	}
	return req, nil
}
