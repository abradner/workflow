package converge

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// planFile is the on-disk envelope for a saved plan: the consumer's own
// plan type plus when it was generated, so a later invocation can warn
// about (or refuse) stale state instead of silently acting on it.
type planFile[T any] struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Plan        json.RawMessage `json:"plan"`
}

// SavePlan writes plan to path as JSON with a GeneratedAt stamp. The plan
// travels client-side only (CLI invocation to CLI invocation); per the
// platform's boundary rule it must already contain no secret material,
// because anything in it has been a workflow input or result.
func SavePlan[T any](path string, plan T) error {
	raw, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encoding plan: %w", err)
	}

	blob, err := json.MarshalIndent(planFile[T]{GeneratedAt: time.Now().UTC(), Plan: raw}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding plan file: %w", err)
	}

	if err := os.WriteFile(path, append(blob, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing plan file: %w", err)
	}
	return nil
}

// LoadPlan reads a plan saved by SavePlan, returning the plan and when it
// was generated. Freshness is the caller's judgment - see Stale for the
// standard check; the age matters because a plan pins SHAs and build
// statuses that the world moves out from under.
func LoadPlan[T any](path string) (T, time.Time, error) {
	var zero T

	blob, err := os.ReadFile(path)
	if err != nil {
		return zero, time.Time{}, fmt.Errorf("reading plan file: %w", err)
	}

	var f planFile[T]
	if err := json.Unmarshal(blob, &f); err != nil {
		return zero, time.Time{}, fmt.Errorf("decoding plan file: %w", err)
	}

	var plan T
	if err := json.Unmarshal(f.Plan, &plan); err != nil {
		return zero, time.Time{}, fmt.Errorf("decoding plan: %w", err)
	}
	return plan, f.GeneratedAt, nil
}

// Stale reports whether a plan generated at generatedAt is older than
// maxAge. A zero maxAge means no limit.
func Stale(generatedAt time.Time, maxAge time.Duration) bool {
	if maxAge == 0 {
		return false
	}
	return time.Since(generatedAt) > maxAge
}
