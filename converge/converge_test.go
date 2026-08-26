package converge_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/converge"
)

func confirmQ(subject string, def bool) converge.Question {
	return converge.Question{
		Kind: "confirm-trigger", Subject: subject,
		Prompt: "Trigger pipeline for " + subject + "?",
		Style:  converge.Confirm, Default: def,
	}
}

func valueQ(subject, reason string) converge.Question {
	return converge.Question{
		Kind: "manual-tag", Subject: subject,
		Prompt: "Enter image tag for " + subject,
		Reason: reason, Style: converge.Value,
		Hint: "press Enter to skip",
	}
}

// --- Prompter -------------------------------------------------------------

func TestPrompter_ConfirmHonorsInputAndDefaults(t *testing.T) {
	var out strings.Builder
	p := &converge.Prompter{
		In:  strings.NewReader("y\n\nn\n"),
		Out: &out,
	}

	answers, err := p.Resolve([]converge.Question{
		confirmQ("app-one", false),  // "y"    -> confirmed
		confirmQ("app-two", false),  // ""     -> default false
		confirmQ("app-three", true), // "n"   -> declined despite default true
	})
	require.NoError(t, err)
	require.Len(t, answers, 3)

	assert.True(t, answers[0].Confirmed)
	assert.False(t, answers[1].Confirmed, "bare Enter takes the default")
	assert.False(t, answers[2].Confirmed)
	assert.Contains(t, out.String(), "[y/N]")
	assert.Contains(t, out.String(), "[Y/n]", "default-true renders the capital on yes")
}

func TestPrompter_ValueSkipsOnBareEnterAndShowsReason(t *testing.T) {
	var out strings.Builder
	p := &converge.Prompter{
		In:  strings.NewReader("1.0.3-SNAPSHOT-abc\n\n"),
		Out: &out,
	}

	answers, err := p.Resolve([]converge.Question{
		valueQ("app-one", "Pipeline for app-one failed"),
		valueQ("app-two", "No pipeline found"),
	})
	require.NoError(t, err)
	require.Len(t, answers, 2)

	assert.Equal(t, "1.0.3-SNAPSHOT-abc", answers[0].Value)
	assert.False(t, answers[0].Skipped())
	assert.True(t, answers[1].Skipped(), "bare Enter skips a value question")

	assert.Contains(t, out.String(), "⚠ ISSUE: Pipeline for app-one failed")
	assert.Contains(t, out.String(), "press Enter to skip")
}

func TestPrompter_EOFReturnsTheRemainderAsUnresolved(t *testing.T) {
	var out strings.Builder
	p := &converge.Prompter{In: strings.NewReader("y\n"), Out: &out} // input ends after one answer

	qs := []converge.Question{confirmQ("app-one", false), valueQ("app-two", "")}
	answers, err := p.Resolve(qs)

	require.Len(t, answers, 1, "the answered prefix is kept")
	var unresolved *converge.UnresolvedError
	require.ErrorAs(t, err, &unresolved)
	require.Len(t, unresolved.Questions, 1)
	assert.Equal(t, "app-two", unresolved.Questions[0].Subject,
		"EOF mid-session must not fabricate answers for what remains")
}

// --- Apply / non-interactive ----------------------------------------------

func TestApply_MatchesByKindAndSubject(t *testing.T) {
	qs := []converge.Question{confirmQ("app-one", false), valueQ("app-two", "")}

	answered, remaining, err := converge.Apply(qs, []converge.Answer{
		{Question: converge.Question{Kind: "manual-tag", Subject: "app-two", Style: converge.Value}, Value: "tag-123"},
		{Question: converge.Question{Kind: "manual-tag", Subject: "app-nine", Style: converge.Value}, Value: "ignored"}, // no such question
	})
	require.NoError(t, err)

	require.Len(t, answered, 1)
	assert.Equal(t, "tag-123", answered[0].Value)
	assert.Equal(t, "Enter image tag for app-two", answered[0].Question.Prompt,
		"the matched answer carries the full Question, not the supplied stub")

	require.Len(t, remaining, 1)
	assert.Equal(t, "app-one", remaining[0].Subject)
}

func TestApply_RefusesACrossStyleAnswer(t *testing.T) {
	// A Value-style stub matched against a Confirm question must error, not
	// read its zero Confirmed as an explicit decline - a malformed or stale
	// plan must never silently change a deployment decision.
	qs := []converge.Question{confirmQ("app-one", false)}

	_, _, err := converge.Apply(qs, []converge.Answer{
		{Question: converge.Question{Kind: "confirm-trigger", Subject: "app-one", Style: converge.Value}, Value: "tag-123"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to reinterpret")
}

func TestApply_RefusesAStubWithoutAStyle(t *testing.T) {
	// The zero Style is deliberately invalid: without this, an
	// uninitialized stub would impersonate whichever style sits at zero.
	qs := []converge.Question{confirmQ("app-one", false)}

	_, _, err := converge.Apply(qs, []converge.Answer{
		{Question: converge.Question{Kind: "confirm-trigger", Subject: "app-one"}, Confirmed: true},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must set Style")
}

func TestUnresolvedError_ListsEveryQuestion(t *testing.T) {
	err := &converge.UnresolvedError{Questions: []converge.Question{
		confirmQ("app-one", false),
		valueQ("app-two", "No pipeline found"),
	}}

	msg := err.Error()
	assert.Contains(t, msg, "2 unresolved")
	assert.Contains(t, msg, "[confirm-trigger] app-one")
	assert.Contains(t, msg, "[manual-tag] app-two")
	assert.Contains(t, msg, "(No pipeline found)")
}

// --- Plan files -------------------------------------------------------------

type fakePlan struct {
	Envs []string `json:"envs"`
	Apps int      `json:"apps"`
}

func TestPlanFile_RoundTripsWithGeneratedAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	before := time.Now().UTC().Add(-time.Second)

	require.NoError(t, converge.SavePlan(path, fakePlan{Envs: []string{"dev3"}, Apps: 11}))

	got, generatedAt, err := converge.LoadPlan[fakePlan](path)
	require.NoError(t, err)
	assert.Equal(t, fakePlan{Envs: []string{"dev3"}, Apps: 11}, got)
	assert.True(t, generatedAt.After(before), "GeneratedAt is stamped at save time")

	assert.False(t, converge.Stale(generatedAt, time.Hour))
	assert.True(t, converge.Stale(generatedAt.Add(-2*time.Hour), time.Hour))
	assert.False(t, converge.Stale(generatedAt.Add(-2*time.Hour), 0), "zero maxAge means no limit")
}

func TestLoadPlan_RejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	require.NoError(t, converge.SavePlan(path, fakePlan{}))

	_, _, err := converge.LoadPlan[[]string](path) // wrong shape for the saved plan
	assert.Error(t, err)

	_, _, err = converge.LoadPlan[fakePlan](filepath.Join(t.TempDir(), "missing.json"))
	assert.Error(t, err)
}

// --- Followup regression coverage -------------------------------------------

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errPipe }

var errPipe = errors.New("pipe burst")

func TestPrompter_AReadErrorIsNotTreatedAsTheHumanStopping(t *testing.T) {
	var out strings.Builder
	p := &converge.Prompter{In: failingReader{}, Out: &out}

	_, err := p.Resolve([]converge.Question{confirmQ("app-one", false)})

	require.Error(t, err)
	var unresolved *converge.UnresolvedError
	assert.False(t, errors.As(err, &unresolved), "a real read failure must not masquerade as unresolved questions")
	assert.Contains(t, err.Error(), "pipe burst")
}

func TestPrompter_HintShowsWithoutAReason(t *testing.T) {
	var out strings.Builder
	p := &converge.Prompter{In: strings.NewReader("\n"), Out: &out}

	q := valueQ("app-one", "") // no Reason
	_, err := p.Resolve([]converge.Question{q})
	require.NoError(t, err)

	assert.Contains(t, out.String(), "press Enter to skip",
		"the Hint contract is per Value question, not per Reason")
}

func TestPrompter_UnknownStyleFailsFast(t *testing.T) {
	var out strings.Builder
	p := &converge.Prompter{In: strings.NewReader("y\n"), Out: &out}

	_, err := p.Resolve([]converge.Question{{Kind: "k", Subject: "s", Style: converge.Style(99)}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown style",
		"silently skipping would return fewer answers than questions")
}

func TestQuestionKey_ColonsInPartsCannotCollide(t *testing.T) {
	a := converge.Question{Kind: "confirm", Subject: "trigger:app"}
	b := converge.Question{Kind: "confirm:trigger", Subject: "app"}
	assert.NotEqual(t, a.Key(), b.Key())
}
