// Package awssecrets extracts secrets from AWS Secrets Manager for a given
// environment, using the official AWS SDK rather than shelling out to the
// aws CLI (the approach the original Ruby tool used).
package awssecrets

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/abradner/workflow/internal/domain"
)

// Client is the subset of the Secrets Manager API this service depends on.
type Client interface {
	ListSecrets(ctx context.Context, params *secretsmanager.ListSecretsInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error)
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// Service extracts secrets from AWS Secrets Manager.
type Service struct {
	client Client
}

// New builds a Service using the ambient AWS credential chain (env vars,
// shared config/credentials files, SSO, instance role, etc).
func New(ctx context.Context) (*Service, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return &Service{client: secretsmanager.NewFromConfig(cfg)}, nil
}

// NewWithClient builds a Service around an explicit client - used in tests.
func NewWithClient(client Client) *Service {
	return &Service{client: client}
}

// ExtractSecrets lists every secret whose name matches env or "dev/env"
// (mirroring the original tool's `--filter Key=name,Values=env,dev/env` CLI
// invocation), then fetches each one's value.
//
// ListSecrets is paginated by the API itself (unlike the `aws` CLI command
// this replaces, which paginates automatically) - a paginator walks every
// page so an environment with more matching secrets than fit on one page
// doesn't silently lose the rest.
func (s *Service) ExtractSecrets(ctx context.Context, env string) ([]domain.ExtractedSecret, error) {
	paginator := secretsmanager.NewListSecretsPaginator(s.client, &secretsmanager.ListSecretsInput{
		Filters: []types.Filter{
			{Key: types.FilterNameStringTypeName, Values: []string{env, "dev/" + env}},
		},
	})

	var metas []types.SecretListEntry
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing AWS secrets: %w", err)
		}
		metas = append(metas, page.SecretList...)
	}

	secrets := make([]domain.ExtractedSecret, 0, len(metas))
	for _, meta := range metas {
		name := aws.ToString(meta.Name)

		valueOut, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(name)})
		if err != nil {
			return nil, fmt.Errorf("fetching AWS secret %s: %w", name, err)
		}

		secret := domain.ExtractedSecret{Name: name, String: valueOut.SecretString}
		if valueOut.SecretBinary != nil {
			// The SDK hands back decoded bytes; re-encode to base64 so
			// downstream code (1Password field values) sees the same string
			// form the original CLI-based tool produced.
			encoded := base64.StdEncoding.EncodeToString(valueOut.SecretBinary)
			secret.Binary = &encoded
		}

		secrets = append(secrets, secret)
	}

	return secrets, nil
}
