package op_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/serviceclients/op"
	"github.com/abradner/workflow/internal/serviceclients/op/optest"
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

// The tests above drive a permissive stub: it accepts any argv and returns
// whatever the test asks for. That is enough to check output handling, but it
// cannot tell a working invocation from a broken one. The tests below drive the
// contract fake instead, which refuses what the real CLI refuses.

// TestCreateItem_SatisfiesTheRealCLIContract pins the invocation end to end:
// the template is read, its category is honoured, and supplied field IDs
// survive. If someone "fixes" CreateItem by adding --category, this fails.
func TestCreateItem_SatisfiesTheRealCLIContract(t *testing.T) {
	runner := &optest.Runner{}
	client := op.NewWithRunner(runner)

	template := map[string]any{
		"title":    "k8s-wtf-dev4",
		"category": "SECURE_NOTE",
		"sections": []any{map[string]any{"id": "cfg", "label": "cfg"}},
		"fields": []any{map[string]any{
			"id": "field-abc", "label": "username", "value": "wtf_dev4", "type": "CONCEALED",
		}},
	}

	out, err := client.CreateItem(context.Background(), template)
	require.NoError(t, err)
	// The real CLI prints a human-readable block here, not a bare ID -- so the
	// item is inspected via the fake's own record rather than by trusting
	// CreateItem's return value to be an identifier.
	assert.Contains(t, out, "ID:")

	created := runner.Last()
	require.NotNil(t, created, "no item was created")
	assert.Equal(t, "k8s-wtf-dev4", created.Title)
	assert.Equal(t, "SECURE_NOTE", created.Category, "category comes from the template")
	require.Len(t, created.Fields, 1)

	field, ok := created.Fields[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "field-abc", field["id"], "supplied field ID must survive verbatim")
}

// Passing --category alongside a template that already declares one is an
// error, not a belt-and-braces improvement. Encoded so nobody re-adds it.
func TestCreateItem_CategoryFlagAlongsideTemplateIsRejected(t *testing.T) {
	runner := &optest.Runner{}

	_, stderr, err := runner.Run(context.Background(), "op",
		[]string{"item", "create", "--category", "Secure Note", "-"},
		[]byte(`{"title":"x","category":"SECURE_NOTE"}`))

	require.Error(t, err)
	assert.Contains(t, stderr, "only specify the category in one location")
	assert.Empty(t, runner.Items)
}

// A template carrying no category and no --category flag is rejected too.
func TestCreateItem_NoCategoryAnywhereIsRejected(t *testing.T) {
	runner := &optest.Runner{}

	_, stderr, err := runner.Run(context.Background(), "op",
		[]string{"item", "create", "-"}, []byte(`{"title":"x"}`))

	require.Error(t, err)
	assert.Contains(t, stderr, "--category")
}

// Omitting --vault is silently accepted and lands the item in the account's
// personal vault. Not an error, and rarely what anyone intends -- see the
// OP_VAULT_NAME work.
func TestCreateItem_WithoutVaultLandsInTheDefaultVault(t *testing.T) {
	runner := &optest.Runner{}
	client := op.NewWithRunner(runner)

	_, err := client.CreateItem(context.Background(),
		map[string]any{"title": "k8s-wtf-dev4", "category": "SECURE_NOTE"})
	require.NoError(t, err)

	created := runner.Last()
	require.NotNil(t, created)
	assert.Equal(t, optest.DefaultVault, created.Vault,
		"no --vault is passed today, so items land in the personal vault")
}

// A typo'd flag must fail, not be silently recorded and ignored. Without this
// the fake would happily accept an invocation the real CLI rejects -- the exact
// hole this package exists to close.
func TestContractFake_RejectsUnknownFlag(t *testing.T) {
	runner := &optest.Runner{}

	_, stderr, err := runner.Run(context.Background(), "op",
		[]string{"item", "create", "--catgory", "Secure Note", "-"},
		[]byte(`{"title":"x"}`))

	require.Error(t, err)
	assert.Contains(t, stderr, "unknown flag: --catgory")
	assert.Empty(t, runner.Items)
}

// A value-taking flag with nothing after it is a missing-argument error, not a
// boolean true.
func TestContractFake_RejectsValuelessFlag(t *testing.T) {
	runner := &optest.Runner{}

	_, stderr, err := runner.Run(context.Background(), "op",
		[]string{"item", "create", "--vault"}, []byte(`{"title":"x","category":"SECURE_NOTE"}`))

	require.Error(t, err)
	assert.Contains(t, stderr, "flag needs an argument: --vault")
	assert.Empty(t, runner.Items)
}

// --dry-run previews without creating. A production regression that started
// passing it would otherwise pass a test asserting an item was created, while
// the real CLI wrote nothing.
func TestContractFake_DryRunCreatesNothing(t *testing.T) {
	runner := &optest.Runner{}

	out, stderr, err := runner.Run(context.Background(), "op",
		[]string{"item", "create", "--dry-run", "-"},
		[]byte(`{"title":"k8s-wtf-dev4","category":"SECURE_NOTE"}`))

	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, out, "k8s-wtf-dev4", "the preview still describes the item")
	assert.Empty(t, runner.Items, "--dry-run must not record an item")
}

// --template is not modelled, so it must be rejected rather than accepted and
// silently ignored by create().
func TestContractFake_RejectsUnmodelledTemplateFlag(t *testing.T) {
	runner := &optest.Runner{}

	_, stderr, err := runner.Run(context.Background(), "op",
		[]string{"item", "create", "--template=item.json"}, nil)

	require.Error(t, err)
	assert.Contains(t, stderr, "unknown flag: --template")
}

// `--vault=` is a missing argument, not an empty value: accepting it would read
// downstream as "not provided" and fall back to a default the real CLI would
// never have reached.
func TestContractFake_RejectsEmptyInlineFlagValue(t *testing.T) {
	runner := &optest.Runner{}

	_, stderr, err := runner.Run(context.Background(), "op",
		[]string{"item", "create", "--vault=", "-"},
		[]byte(`{"title":"x","category":"SECURE_NOTE"}`))

	require.Error(t, err)
	assert.Contains(t, stderr, "flag needs an argument: --vault")
	assert.Empty(t, runner.Items)
}
