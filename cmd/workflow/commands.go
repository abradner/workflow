package main

import (
	"github.com/spf13/cobra"

	"github.com/abradner/workflow/internal/config"
	"github.com/abradner/workflow/internal/workflows"
)

func newSyncCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Synchronize Kustomize manifests for all workloads",
		Long: "Discovers apps, extracts base/overlay manifests, runs the modernization/registry/service-abstraction\n" +
			"transformer pipeline for every target environment, and writes the result to the destination directory.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorkflow(cmd.Context(), opts, workflows.SyncWorkloadsWorkflow, func(cfg config.Config) workflows.SyncWorkloadsInput {
				return workflows.SyncWorkloadsInput{Config: cfg, DryRun: opts.DryRun}
			})
		},
	}
}

func newSetupArgoCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "setup-argo",
		Short: "Generate ArgoCD Application manifests for all apps",
		Long:  "Generates one ArgoCD Application manifest per app x environment, mapped to its overlay path.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorkflow(cmd.Context(), opts, workflows.GenerateArgocdWorkflow, func(cfg config.Config) workflows.GenerateArgocdInput {
				return workflows.GenerateArgocdInput{Config: cfg, DryRun: opts.DryRun}
			})
		},
	}
}

func newSync1PasswordCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "sync-1p",
		Short: "Sync AWS Secrets Manager secrets into 1Password",
		Long: "Extracts secrets from AWS Secrets Manager for the source environment, remaps them onto every target\n" +
			"environment (refreshing the Keycloak SAML public key where available), and provisions one 1Password\n" +
			"Secure Note per environment.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorkflow(cmd.Context(), opts, workflows.Sync1PasswordWorkflow, func(cfg config.Config) workflows.Sync1PasswordInput {
				return workflows.Sync1PasswordInput{Config: cfg, DryRun: opts.DryRun}
			})
		},
	}
}

func newRenderTalosCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "render-talos",
		Short: "Hydrate Talos cluster templates from a 1Password Secure Note",
		Long: "Reads a 1Password Secure Note containing secrets.yaml content, flattens it to dot-notation keys,\n" +
			"and substitutes \"{{ dotted.key }}\" placeholders in every *.template.yaml file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorkflow(cmd.Context(), opts, workflows.RenderTalosWorkflow, func(cfg config.Config) workflows.RenderTalosInput {
				return workflows.RenderTalosInput{Config: cfg, DryRun: opts.DryRun}
			})
		},
	}
}

func newSetupKeycloakCmd(opts *globalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "setup-keycloak",
		Short: "Provision the Keycloak realm, clients, groups, and users for every environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorkflow(cmd.Context(), opts, workflows.SetupKeycloakWorkflow, func(cfg config.Config) workflows.SetupKeycloakInput {
				return workflows.SetupKeycloakInput{Config: cfg, DryRun: opts.DryRun}
			})
		},
	}
}
