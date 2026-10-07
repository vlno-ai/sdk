package sessionflow

import "context"

// Observe performs GET-only reconciliation; it never repeats a mutation.
func (p *Protocol) Observe(ctx context.Context) error {
	for {
		if e := p.Refresh(ctx); e != nil {
			return e
		}
		j, e := p.read()
		if e != nil {
			return e
		}
		phase := object(object(j, "observation"), "value")["phase"]
		if phase == "finished" || phase == "cancelled" || phase == "interrupted" || phase == "expired" {
			return nil
		}
		if e = pause(ctx); e != nil {
			return e
		}
	}
}
