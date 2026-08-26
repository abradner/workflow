// Package converge is the platform's human-in-the-loop machinery for
// two-pass workflows: a read-only planning pass surfaces Questions, the CLI
// resolves them - interactively, or from answers pre-supplied by flags or a
// saved plan file - and only the converged plan reaches the mutating pass.
//
// Workflows never prompt. This package is CLI-side only, which is what
// keeps stdin (and the non-determinism it carries) out of Temporal workflow
// code and its durable event history. A workflow that needs a decision
// returns a Question in its result; the next workflow run receives the
// Answer as ordinary input.
package converge

import (
	"fmt"
	"strings"
)

// Style says how a Question is answered. The zero value is deliberately
// invalid: an uninitialized Question or answer stub must fail loudly, not
// impersonate a Confirm.
type Style int

const (
	styleUnset Style = iota
	// Confirm is a yes/no question; bare Enter takes Question.Default.
	Confirm
	// Value asks for a text value; bare Enter skips (empty Value).
	Value
)

// Question is one unresolved decision a planning pass surfaced.
type Question struct {
	// Kind is the consumer-defined discriminator (e.g. "confirm-trigger",
	// "manual-tag"); with Subject it identifies the question for matching
	// against pre-supplied answers.
	Kind string
	// Subject is what the question is about (e.g. "dev3/pmn-core").
	Subject string
	// Prompt is the human-facing question text (e.g. "Trigger pipeline
	// for pmn-core?" or "Enter image tag for pmn-core").
	Prompt string
	// Reason, when set, is shown as a warning before the prompt - the
	// planning pass's explanation of why a human is needed.
	Reason string
	// Style selects yes/no vs. free-text answering.
	Style Style
	// Default is the Confirm answer a bare Enter takes.
	Default bool
	// Hint, when set, is shown for Value questions (e.g. "press Enter to
	// skip"). Value questions are always skippable; the hint just says so.
	Hint string
}

// Key identifies a Question for answer matching: Kind plus Subject. The
// separator is NUL so a ":" (or anything printable) in either part cannot
// make two different questions collide.
func (q Question) Key() string { return q.Kind + "\x00" + q.Subject }

// Answer records the resolution of one Question.
type Answer struct {
	Question  Question
	Confirmed bool   // Confirm style
	Value     string // Value style; empty means skipped
}

// Skipped reports whether a Value question was declined.
func (a Answer) Skipped() bool { return a.Question.Style == Value && a.Value == "" }

// Apply matches pre-supplied answers (from flags or a saved plan) against
// questions by Key, returning the answers that matched and the questions
// nothing answered. A supplied answer must set Question.Kind,
// Question.Subject AND Question.Style; the matched answer carries the full
// Question.
//
// Style is load-bearing, not ceremony: an answer whose style differs from
// the question it matches is an error, never a silent zero-value. Without
// that check, Answer{Value: "tag"} matched against a Confirm question
// would read its zero Confirmed as an explicit decline, and
// Answer{Confirmed: true} against a Value question would read its zero
// Value as a skip - a malformed or stale plan changing a deployment
// decision instead of failing. Duplicate supplied answers for one key
// resolve last-wins; that is a property of the supply, so it is the
// supplier's to avoid.
func Apply(questions []Question, supplied []Answer) (answered []Answer, remaining []Question, err error) {
	byKey := make(map[string]Answer, len(supplied))
	for _, a := range supplied {
		if a.Question.Style == styleUnset {
			return nil, nil, fmt.Errorf("converge: supplied answer %q must set Style", a.Question.Key())
		}
		byKey[a.Question.Key()] = a
	}

	for _, q := range questions {
		a, ok := byKey[q.Key()]
		if !ok {
			remaining = append(remaining, q)
			continue
		}
		if a.Question.Style != q.Style {
			return nil, nil, fmt.Errorf(
				"converge: supplied answer %q has style %d but the question has style %d - refusing to reinterpret its fields",
				q.Key(), a.Question.Style, q.Style)
		}
		a.Question = q
		answered = append(answered, a)
	}
	return answered, remaining, nil
}

// UnresolvedError is returned in non-interactive mode when questions remain
// unanswered: the caller should exit non-zero, and the message lists every
// question so the invoker (a human reading CI logs) can supply answers.
type UnresolvedError struct {
	Questions []Question
}

func (e *UnresolvedError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d unresolved question(s) in non-interactive mode:", len(e.Questions))
	for _, q := range e.Questions {
		fmt.Fprintf(&b, "\n  - [%s] %s: %s", q.Kind, q.Subject, q.Prompt)
		if q.Reason != "" {
			fmt.Fprintf(&b, " (%s)", q.Reason)
		}
	}
	return b.String()
}
