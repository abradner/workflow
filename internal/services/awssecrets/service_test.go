package awssecrets_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/services/awssecrets"
)

type fakeAWSClient struct {
	listInput *secretsmanager.ListSecretsInput
}

func (f *fakeAWSClient) ListSecrets(_ context.Context, params *secretsmanager.ListSecretsInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	f.listInput = params
	return &secretsmanager.ListSecretsOutput{
		SecretList: []types.SecretListEntry{
			{Name: aws.String("dev3/wtf/config")},
			{Name: aws.String("dev3/wtf/cert")},
		},
	}, nil
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
	fake := &fakeAWSClient{}
	svc := awssecrets.NewWithClient(fake)

	secrets, err := svc.ExtractSecrets(context.Background(), "dev3")
	require.NoError(t, err)
	require.Len(t, secrets, 2)

	require.NotNil(t, fake.listInput)
	require.Len(t, fake.listInput.Filters, 1)
	assert.Equal(t, types.FilterNameStringTypeName, fake.listInput.Filters[0].Key)
	assert.Equal(t, []string{"dev3", "dev/dev3"}, fake.listInput.Filters[0].Values)

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
