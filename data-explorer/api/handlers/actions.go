package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"data-explorer/api/models"
	"data-explorer/api/pagination"
	"data-explorer/database"
)

// ListActions handles GET /actions with filter, cursor-pagination, and limit params.
func ListActions(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp, errs := models.ParseFilterParams(r)
		if len(errs) > 0 {
			writeError(w, http.StatusBadRequest, "invalid query parameters", errs...)
			return
		}

		// Decode optional cursor.
		var after *database.CursorVal
		if fp.Cursor != "" {
			bn, id, err := pagination.Decode(fp.Cursor)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid cursor")
				return
			}
			after = &database.CursorVal{BlockNum: bn, ID: id}
		}

		filter := database.ActionFilter{
			Method:     fp.Method,
			Caller:     fp.Caller,
			Contract:   fp.Contract,
			TxParamKey: fp.TxParamKey,
			TxParamVal: fp.TxParamVal,
			EventName: fp.EventName,
			EventDataKey: fp.EventDataKey,
			EventDataVal: fp.EventDataVal,
			FromBlock:  fp.FromBlock,
			ToBlock:    fp.ToBlock,
			FromTime:   fp.FromTime,
			ToTime:     fp.ToTime,
		}

		result, err := db.ListActions(r.Context(), filter, after, fp.Limit)
		if err != nil {
			log.Printf("ListActions error: %v", err)
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}

		// Map database rows to the leaner list item shape.
		items := make([]models.ActionListItem, len(result.Rows))
		for i, row := range result.Rows {
			items[i] = models.ActionListItem{
				ID:        row.ID,
				BlockNum:  row.BlockNum,
				BlockTime: row.BlockTime,
				TxHash:    row.TxHash,
				Method:    row.Method,
				Caller:    row.Caller,
				Contract:  row.Contract,
			}
		}

		page := models.ActionsPage{
			Data:  items,
			Count: len(items),
		}
		if result.NextCursor != nil {
			page.NextCursor = pagination.Encode(result.NextCursor.BlockNum, result.NextCursor.ID)
		}

		writeJSON(w, http.StatusOK, page)
	}
}

// GetAction handles GET /actions/{blockNum}/{id} — full detail view.
func GetAction(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		blockNumStr := chi.URLParam(r, "blockNum")
		idStr := chi.URLParam(r, "id")

		blockNum, err := strconv.ParseInt(blockNumStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "blockNum must be an integer")
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "id must be an integer")
			return
		}

		row, err := db.GetActionByID(r.Context(), blockNum, id)
		if err != nil {
			log.Printf("GetAction error: %v", err)
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		if row == nil {
			writeError(w, http.StatusNotFound, "action not found")
			return
		}

		detail := models.ActionDetail{
			ActionListItem: models.ActionListItem{
				ID:        row.ID,
				BlockNum:  row.BlockNum,
				BlockTime: row.BlockTime,
				TxHash:    row.TxHash,
				Method:    row.Method,
				Caller:    row.Caller,
				Contract:  row.Contract,
			},
			TxParams: row.TxParams,
			Events:   row.Events,
			Value:    row.Value,
		}

		writeJSON(w, http.StatusOK, detail)
	}
}
