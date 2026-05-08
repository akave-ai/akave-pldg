package distributor

import (
	"context"

	"github.com/akave-ai/akavelog/internal/ingester"
	"github.com/akave-ai/akavelog/internal/push"
)

// Distributor receives push requests and forwards them to the ingester (single-node; no ring).
type Distributor struct {
	ingester *ingester.Ingester
}

// New creates a distributor that sends to the given ingester.
func New(ing *ingester.Ingester) *Distributor {
	return &Distributor{ingester: ing}
}

// Push parses and validates the request, then forwards to the ingester.
func (d *Distributor) Push(ctx context.Context, req *push.PushRequest) {
	if d.ingester == nil || req == nil || len(req.Streams) == 0 {
		return
	}
	d.ingester.Push(ctx, req)
}
