package cli_test

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/cli"
	"github.com/abradner/workflow/temporalutil"
)

func testApp() cli.App {
	return cli.App{
		Name:   "testtool",
		Short:  "a test consumer",
		Engine: temporalutil.Engine{TaskQueue: "test-queue"},
	}
}

func TestNew_ProvidesTheGlobalFlagContract(t *testing.T) {
	root := cli.New(testApp())

	for flag, wantDefault := range map[string]string{
		"dry-run":  "false",
		"verbose":  "false",
		"temporal": "embedded",
	} {
		f := root.PersistentFlags().Lookup(flag)
		require.NotNilf(t, f, "--%s must exist on every consumer's root command", flag)
		assert.Equalf(t, wantDefault, f.DefValue, "--%s default", flag)
	}

	assert.Equal(t, "testtool", root.Use)
}

func TestNew_AlwaysAddsTheWorkerSubcommand(t *testing.T) {
	root := cli.New(testApp())

	names := make([]string, 0, len(root.Commands()))
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "worker")
}

// Factories are called at New time, but flag values are only bound by cobra
// between construction and RunE - so a factory reading Options inside RunE
// sees the invocation's flags, which is the contract cli.go documents.
func TestNew_FactoriesSeeFlagValuesAtRunETime(t *testing.T) {
	var seenDryRun bool

	newProbeCmd := func(opts *cli.Options) *cobra.Command {
		return &cobra.Command{
			Use: "probe",
			RunE: func(_ *cobra.Command, _ []string) error {
				seenDryRun = opts.DryRun
				return nil
			},
		}
	}

	root := cli.New(testApp(), newProbeCmd)
	root.SetArgs([]string{"--dry-run", "probe"})
	require.NoError(t, root.Execute())

	assert.True(t, seenDryRun, "RunE must observe the bound flag, not the construction-time zero value")
}
