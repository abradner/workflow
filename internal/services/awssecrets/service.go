// Package awssecrets extracts secrets from AWS Secrets Manager for a given
// environment, using the official AWS SDK rather than shelling out to the
// aws CLI (the approach the original Ruby tool used).
package awssecrets

import (
	"context"
	"encoding/base64"
	"errors"
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

// Logger is the minimal logging interface this service accepts, satisfied by
// activity.GetLogger(ctx) and by any stand-in used in tests.
type Logger interface {
	Info(msg string, keyvals ...any)
}

// Service extracts secrets from AWS Secrets Manager.
type Service struct {
	client Client

	// logger is optional; nil disables the skipped-secret warnings. Set with
	// WithLogger rather than assigned, so the only way to attach one produces
	// a copy - see that method for why.
	logger Logger
}

// WithLogger returns a copy of s that logs through l.
//
// A copy, not a setter, and deliberately so. Activities holds one *Service
// shared by every concurrently-executing activity, so assigning a logger field
// on it would be a data race across the per-environment fan-out - two
// environments syncing at once, both writing the same pointer. Copying the
// value gives each invocation its own logger and leaves the shared one
// untouched.
//
// This exists because the alternative silently failed: the service had an
// exported Logger field that production never set, so ExtractExact's
// not-found path swallowed missing secrets without a word while the docs
// promised a warning.
func (s *Service) WithLogger(l Logger) *Service {
	if s == nil {
		return nil
	}
	copied := *s
	copied.logger = l
	return &copied
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

// ExtractExact fetches secrets by exact name, bypassing the name filter
// ExtractSecrets uses.
//
// Some secrets do not follow the environment naming convention and so are
// invisible to that filter - the ElastiCache credentials at
// "dev/neons-dev-elasticache/pmn-<env>-ro" being the live example. Without
// this they are simply absent from the migrated vault item, silently.
//
// A secret that genuinely does not exist is skipped with a warning: a fresh
// environment legitimately may not have every peripheral secret yet, and one
// missing entry should not abort a migration. Every OTHER error - credentials,
// permissions, throttling - is returned. That distinction matters more than it
// looks: the Ruby original wrapped this in a blanket `rescue RuntimeError`, so
// an expired session or a missing IAM permission produced the same silent skip
// as a genuinely absent secret, and a completely broken run reported success
// with zero exact secrets extracted.
func (s *Service) ExtractExact(ctx context.Context, names []string) ([]domain.ExtractedSecret, error) {
	secrets := make([]domain.ExtractedSecret, 0, len(names))

	for _, name := range names {
		valueOut, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(name)})
		if err != nil {
			var notFound *types.ResourceNotFoundException
			if errors.As(err, &notFound) {
				s.logf("AWS secret %s not found, skipping", name)
				continue
			}
			return nil, fmt.Errorf("fetching AWS secret %s: %w", name, err)
		}

		secret := domain.ExtractedSecret{Name: name, String: valueOut.SecretString}
		if valueOut.SecretBinary != nil {
			encoded := base64.StdEncoding.EncodeToString(valueOut.SecretBinary)
			secret.Binary = &encoded
		}
		secrets = append(secrets, secret)
	}

	return secrets, nil
}

// logf reports a skipped secret. Nil-safe so the service stays usable without
// a logger; a skipped secret is worth saying out loud but never fatal.
func (s *Service) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Info(fmt.Sprintf(format, args...))
	}
}
