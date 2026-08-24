package configload_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/configload"
)

type testConfig struct {
	Name  string   `env:"TEST_CFG_NAME,required"`
	Count int      `env:"TEST_CFG_COUNT" envDefault:"3"`
	Tags  []string `env:"TEST_CFG_TAGS" envSeparator:","`
}

// unsetenv guarantees key is absent for the duration of the test while still
// restoring whatever value the outer environment had. t.Setenv alone cannot
// express "unset": it only sets - but it registers a cleanup that restores
// the ORIGINAL state, so setting-then-unsetting gets both halves.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

// loadTestSetup isolates a Load test from the machine it runs on: every
// variable the struct reads is pinned or unset (a stray TEST_CFG_COUNT in the
// developer's shell must not reach env.Parse), and cwd moves to an empty temp
// dir so a stray .env file - gitignored at any depth, so invisible in review -
// can't be picked up by godotenv.
func loadTestSetup(t *testing.T) {
	t.Helper()
	unsetenv(t, "TEST_CFG_NAME")
	unsetenv(t, "TEST_CFG_COUNT")
	unsetenv(t, "TEST_CFG_TAGS")
	t.Chdir(t.TempDir())
}

func TestLoad_ParsesEnvIntoConsumerStruct(t *testing.T) {
	loadTestSetup(t)
	t.Setenv("TEST_CFG_NAME", "demo")
	t.Setenv("TEST_CFG_TAGS", "a,b")

	cfg, err := configload.Load[testConfig]()
	require.NoError(t, err)

	assert.Equal(t, "demo", cfg.Name)
	assert.Equal(t, 3, cfg.Count, "envDefault applies when the variable is unset")
	assert.Equal(t, []string{"a", "b"}, cfg.Tags)
}

func TestLoad_FailsWhenARequiredVariableIsMissing(t *testing.T) {
	// caarlos0/env's `required` fails only on UNSET - a set-but-empty
	// variable satisfies it - so this test is meaningful only because
	// loadTestSetup guarantees TEST_CFG_NAME is genuinely absent.
	loadTestSetup(t)

	_, err := configload.Load[testConfig]()
	assert.Error(t, err)
}

func TestExpandPath_ResolvesHomeAndRelativeSegments(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	got, err := configload.ExpandPath("~/somewhere")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "somewhere"), got)

	got, err = configload.ExpandPath("~")
	require.NoError(t, err)
	assert.Equal(t, home, got, "bare ~ is the home directory itself")

	got, err = configload.ExpandPath("relative/dir")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(got), "relative paths become absolute")
}

func TestExpandPath_DocumentedEdgeCases(t *testing.T) {
	t.Chdir(t.TempDir())
	// Getwd rather than the TempDir value: on macOS the temp root is a
	// symlink, and Abs resolves against the evaluated working directory.
	wd, err := os.Getwd()
	require.NoError(t, err)

	// ~user syntax is NOT expanded - it resolves as a relative path.
	got, err := configload.ExpandPath("~alice")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wd, "~alice"), got)

	// Empty input resolves to the working directory - which is why callers
	// applying it to `required` env fields still accept an empty value:
	// `required` means set, not non-empty.
	got, err = configload.ExpandPath("")
	require.NoError(t, err)
	assert.Equal(t, wd, got)
}
