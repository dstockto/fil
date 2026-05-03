package server

import (
	"context"
	"fmt"
	"time"

	"github.com/dstockto/fil/plan"
)

// notifierAdapter bridges *server.Notifier to plan.Notifier. server.Notifier
// has a richer surface (TestAll, IsQuietHours, Speak etc.) than plan needs;
// this exposes only Notify and folds in quiet-hours suppression.
type notifierAdapter struct {
	n *Notifier
}

func (a *notifierAdapter) Notify(_ context.Context, title, body string) {
	if a.n == nil {
		return
	}
	if a.n.IsQuietHours(time.Now()) {
		return
	}
	if errs := a.n.Send(title, body); len(errs) > 0 {
		for _, err := range errs {
			fmt.Printf("[notify] %v\n", err)
		}
	}
}

// NewNotifierAdapter wraps a *Notifier so it satisfies plan.Notifier. Pass
// nil when notifications aren't configured — Notify becomes a no-op.
func NewNotifierAdapter(n *Notifier) plan.Notifier {
	if n == nil {
		return plan.NoopNotifier{}
	}
	return &notifierAdapter{n: n}
}
