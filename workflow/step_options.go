package workflow

import "encoding/json"

// StepOptions controls randomness and audit metadata for one operation.
// Draw overrides a run's default when supplied. Meta is copied into records.
type StepOptions struct {
	Draw Draw
	Meta map[string]json.RawMessage
}

func (o StepOptions) draw(fallback Draw) Draw {
	if o.Draw != nil {
		return o.Draw
	}
	return fallback
}
