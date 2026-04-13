package models

import (
	"net/http"
	"strconv"
	"time"
)

// FilterParams holds every query-string filter recognised by the actions endpoints.
type FilterParams struct {
	Method     string
	Caller     string
	Contract   string
	TxParamKey string
	TxParamVal string
	EventName string
	EventDataKey string
	EventDataVal string
	FromBlock  int64
	ToBlock    int64
	FromTime   time.Time
	ToTime     time.Time
	Cursor     string
	Limit      int
}

// ParseFilterParams reads query string values from the request and returns a
// FilterParams with normalised values plus a slice of validation error messages.
func ParseFilterParams(r *http.Request) (FilterParams, []string) {
	q := r.URL.Query()
	var errs []string
	fp := FilterParams{Limit: 50}

	fp.Method = q.Get("method")
	fp.Caller = q.Get("caller")
	fp.Contract = q.Get("contract")
	fp.TxParamKey = q.Get("tx_param_key")
	fp.TxParamVal = q.Get("tx_param_val")
	fp.Cursor = q.Get("cursor")
	fp.EventName = q.Get("event_name")
	fp.EventDataKey = q.Get("event_data_key")
	fp.EventDataVal = q.Get("event_data_val")

	if fp.TxParamKey != "" && fp.TxParamVal == "" {
		errs = append(errs, "tx_param_val is required when tx_param_key is set")
	}

	if fp.EventDataKey != "" && fp.EventDataVal == "" {
		errs = append(errs, "event_data_val is required when event_data_key is set")
	}

	if v := q.Get("from_block"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			errs = append(errs, "from_block must be an integer")
		} else {
			fp.FromBlock = n
		}
	}
	if v := q.Get("to_block"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			errs = append(errs, "to_block must be an integer")
		} else {
			fp.ToBlock = n
		}
	}
	if v := q.Get("from_time"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			errs = append(errs, "from_time must be RFC 3339 (e.g. 2024-01-01T00:00:00Z)")
		} else {
			fp.FromTime = t
		}
	}
	if v := q.Get("to_time"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			errs = append(errs, "to_time must be RFC 3339")
		} else {
			fp.ToTime = t
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			errs = append(errs, "limit must be an integer between 1 and 500")
		} else {
			fp.Limit = n
		}
	}

	return fp, errs
}
