package distributor

import (
	"errors"
	"log"
	"net/http"

	"github.com/akave-ai/akavelog/internal/push"
	"github.com/labstack/echo/v4"
)

const (
	// MaxRecvMsgSize is the maximum compressed push body size (bytes).
	MaxRecvMsgSize = 5 * 1024 * 1024 // 5MB
	// MaxDecompressedSize is the maximum decompressed push body size (bytes).
	MaxDecompressedSize = 10 * 1024 * 1024 // 10MB
)

// PushHandler returns an Echo handler for POST /akavelog/api/v1/push.
// Parse body (Content-Encoding: gzip/deflate, Content-Type: application/json),
// validate size, push to ingester, then optionally invoke onLog per entry (for recent logs UI).
// Returns 204 No Content on success. No Prometheus, no gRPC, Akavelog-only.
func (d *Distributor) PushHandler(onLog func(labels map[string]string, tsNs int64, line string)) echo.HandlerFunc {
	return func(c echo.Context) error {
		req, err := push.ParseRequestBody(c.Request(), MaxRecvMsgSize, MaxDecompressedSize)
		if err != nil {
			switch {
			case errors.Is(err, push.ErrRequestBodyTooLarge):
				log.Printf("[akavelog] push request body too large: %v", err)
				return c.String(http.StatusRequestEntityTooLarge, err.Error())
			default:
				log.Printf("[akavelog] push parse error: %v", err)
				return c.String(http.StatusBadRequest, err.Error())
			}
		}

		if req == nil || len(req.Streams) == 0 {
			return c.NoContent(http.StatusNoContent)
		}

		d.Push(c.Request().Context(), req)

		if onLog != nil {
			for _, s := range req.Streams {
				for _, e := range s.Entries {
					onLog(s.Labels, e.TsNs, e.Line)
				}
			}
		}

		return c.NoContent(http.StatusNoContent)
	}
}
