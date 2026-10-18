package api

import "fmt"

const DefaultNormalizeInputBudget int64 = 256 << 20

// NormalizeOptions bounds concurrent jobs by logical input size, not RSS.
// Zero values select defaults. A job over the budget runs alone.
type NormalizeOptions struct {
	Workers          int
	InputBudgetBytes int64
}

func (o NormalizeOptions) resolved() (NormalizeOptions, error) {
	if o.Workers == 0 {
		o.Workers = normalizeWorkers
	}
	if o.InputBudgetBytes == 0 {
		o.InputBudgetBytes = DefaultNormalizeInputBudget
	}
	if o.Workers < 1 || o.Workers > 64 {
		return o, fmt.Errorf("normalize workers must be 1..64")
	}
	if o.InputBudgetBytes < 1 {
		return o, fmt.Errorf("normalize input budget must be positive")
	}
	return o, nil
}
