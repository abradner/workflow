package awssecrets_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/services/awssecrets"
)

// fakeAWSClient serves ListSecrets results one page at a time (via
// NextToken), so tests can prove ExtractSecrets walks every page instead of
// only the first.
type fakeAWSClient struct {
	pages      [][]types.SecretListEntry
	listInputs []*secretsmanager.ListSecretsInput
}

func (f *fakeAWSClient) ListSecrets(_ context.Context, params *secretsmanager.ListSecretsInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	f.listInputs = append(f.listInputs, params)

	pageIndex := 0
	if params.NextToken != nil {
		pageIndex, _ = strconv.Atoi(*params.NextToken)
	}

	out := &secretsmanager.ListSecretsOutput{SecretList: f.pages[pageIndex]}
	if next := pageIndex + 1; next < len(f.pages) {
		token := strconv.Itoa(next)
		out.NextToken = &token
	}
	return out, nil
}

func (f *fakeAWSClient) GetSecretValue(_ context.Context, params *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	switch aws.ToString(params.SecretId) {
	case "dev3/wtf/config":
		return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(`{"username":"db_user"}`)}, nil
	case "dev3/wtf/cert":
		return &secretsmanager.GetSecretValueOutput{SecretBinary: []byte("op_1123")}, nil
	}
	panic("unexpected secret id: " + aws.ToString(params.SecretId))
}

func TestExtractSecrets(t *testing.T) {
	fake := &fakeAWSClient{pages: [][]types.SecretListEntry{
		{{Name: aws.String("dev3/wtf/config")}, {Name: aws.String("dev3/wtf/cert")}},
	}}
	svc := awssecrets.NewWithClient(fake)

	secrets, err := svc.ExtractSecrets(context.Background(), "dev3")
	require.NoError(t, err)
	require.Len(t, secrets, 2)

	require.Len(t, fake.listInputs, 1)
	require.Len(t, fake.listInputs[0].Filters, 1)
	assert.Equal(t, types.FilterNameStringTypeName, fake.listInputs[0].Filters[0].Key)
	assert.Equal(t, []string{"dev3", "dev/dev3"}, fake.listInputs[0].Filters[0].Values)

	assert.Equal(t, "dev3/wtf/config", secrets[0].Name)
	require.NotNil(t, secrets[0].String)
	assert.Equal(t, `{"username":"db_user"}`, *secrets[0].String)
	assert.Nil(t, secrets[0].Binary)

	assert.Equal(t, "dev3/wtf/cert", secrets[1].Name)
	assert.Nil(t, secrets[1].String)
	require.NotNil(t, secrets[1].Binary)
	// The SDK decodes SecretBinary to raw bytes; ExtractSecrets re-encodes
	// to base64 so downstream code (1Password field values) sees the same
	// string form the original CLI-based tool produced.
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("op_1123")), *secrets[1].Binary)
}

func TestExtractSecrets_WalksEveryPage(t *testing.T) {
	fake := &fakeAWSClient{pages: [][]types.SecretListEntry{
		{{Name: aws.String("dev3/wtf/config")}},
		{{Name: aws.String("dev3/wtf/cert")}},
	}}
	svc := awssecrets.NewWithClient(fake)

	secrets, err := svc.ExtractSecrets(context.Background(), "dev3")
	require.NoError(t, err)

	// Two ListSecrets pages, one entry each - both must show up, not just
	// the first page's.
	require.Len(t, fake.listInputs, 2)
	require.Len(t, secrets, 2)
	assert.Equal(t, "dev3/wtf/config", secrets[0].Name)
	assert.Equal(t, "dev3/wtf/cert", secrets[1].Name)
}

// exactClient serves GetSecretValue by name, so ExtractExact's error handling
// can be exercised branch by branch. ListSecrets is never called.
type exactClient struct {
	values map[string]string
	errs   map[string]error
	got    []string
}

func (c *exactClient) ListSecrets(context.Context, *secretsmanager.ListSecretsInput, ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	panic("ExtractExact must not call ListSecrets")
}

func (c *exactClient) GetSecretValue(_ context.Context, params *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	name := aws.ToString(params.SecretId)
	c.got = append(c.got, name)
	if err, ok := c.errs[name]; ok {
		return nil, err
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(c.values[name])}, nil
}

func TestExtractExact_FetchesByNameWithoutListing(t *testing.T) {
	client := &exactClient{values: map[string]string{
		"dev/neons-dev-elasticache/pmn-dev3-ro": `{"username":"pmn-dev3-ro"}`,
		"dev/neons-dev-elasticache/pmn-dev3-rw": `{"username":"pmn-dev3-rw"}`,
	}}
	svc := awssecrets.NewWithClient(client)

	got, err := svc.ExtractExact(context.Background(), []string{
		"dev/neons-dev-elasticache/pmn-dev3-ro",
		"dev/neons-dev-elasticache/pmn-dev3-rw",
	})
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, "dev/neons-dev-elasticache/pmn-dev3-ro", got[0].Name)
	assert.JSONEq(t, `{"username":"pmn-dev3-ro"}`, *got[0].String)
	assert.Len(t, client.got, 2, "one GetSecretValue per name, no listing")
}

// A genuinely absent secret is skipped: a fresh environment may not have every
// peripheral secret yet, and one missing entry should not abort a migration.
func TestExtractExact_SkipsSecretsThatDoNotExist(t *testing.T) {
	client := &exactClient{
		values: map[string]string{"present": "value"},
		errs:   map[string]error{"absent": &types.ResourceNotFoundException{}},
	}
	svc := awssecrets.NewWithClient(client)

	got, err := svc.ExtractExact(context.Background(), []string{"absent", "present"})
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, "present", got[0].Name)
}

// Everything that is not a not-found is fatal. The Ruby original's blanket
// rescue made an expired session indistinguishable from an absent secret, so a
// completely broken run reported success having extracted nothing.
func TestExtractExact_FailsLoudlyOnNonNotFoundErrors(t *testing.T) {
	for name, awsErr := range map[string]error{
		"access denied": &types.InvalidRequestException{Message: aws.String("access denied")},
		"throttled":     errors.New("Throttling: rate exceeded"),
	} {
		t.Run(name, func(t *testing.T) {
			client := &exactClient{errs: map[string]error{"boom": awsErr}}
			svc := awssecrets.NewWithClient(client)

			_, err := svc.ExtractExact(context.Background(), []string{"boom"})
			require.Error(t, err, "must not be swallowed as a skipped secret")
			assert.Contains(t, err.Error(), "boom")
		})
	}
}
