package op_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/serviceclients/op"
)

type fakeRunner struct {
	gotName string
	gotArgs []string
	gotIn   []byte

	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, stdin []byte) (string, string, error) {
	f.gotName, f.gotArgs, f.gotIn = name, args, stdin
	return f.stdout, f.stderr, f.err
}

func TestCreateItem_PipesJSONToStdin(t *testing.T) {
	runner := &fakeRunner{stdout: "id=123"}
	client := op.NewWithRunner(runner)

	template := map[string]any{"title": "my-item", "category": "SECURE_NOTE"}
	out, err := client.CreateItem(context.Background(), template)
	require.NoError(t, err)

	assert.Equal(t, "id=123", out)
	assert.Equal(t, "op", runner.gotName)
	assert.Equal(t, []string{"item", "create", "-"}, runner.gotArgs)

	var sent map[string]any
	require.NoError(t, json.Unmarshal(runner.gotIn, &sent))
	assert.Equal(t, template["title"], sent["title"])
}

func TestCreateItem_TrimsTrailingNewline(t *testing.T) {
	runner := &fakeRunner{stdout: "id=123\n"}
	client := op.NewWithRunner(runner)

	out, err := client.CreateItem(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "id=123", out)
}

func TestCreateItem_WrapsFailure(t *testing.T) {
	runner := &fakeRunner{err: errors.New("exit 1"), stderr: "not signed in"}
	client := op.NewWithRunner(runner)

	_, err := client.CreateItem(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not signed in")
}

func TestReadNote_StripsWrappingQuotesAndUnescapesNewlines(t *testing.T) {
	runner := &fakeRunner{stdout: `"line one\nline two"` + "\n"}
	client := op.NewWithRunner(runner)

	content, err := client.ReadNote(context.Background(), "item-id")
	require.NoError(t, err)

	assert.Equal(t, "line one\nline two", content)
	assert.Equal(t, []string{"item", "get", "item-id", "--fields", "notesPlain"}, runner.gotArgs)
}

func TestReadNote_PassesThroughUnquotedContent(t *testing.T) {
	runner := &fakeRunner{stdout: "plain: yaml\n"}
	client := op.NewWithRunner(runner)

	content, err := client.ReadNote(context.Background(), "item-id")
	require.NoError(t, err)
	assert.Equal(t, "plain: yaml", content)
}
