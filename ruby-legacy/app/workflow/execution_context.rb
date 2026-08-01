# frozen_string_literal: true

module Workflow
  # Shared state container passed through all workflow phases.
  # Holds configuration and memoized results.
  class ExecutionContext
    attr_reader :config, :logger, :options
    attr_accessor :apps, :prompt, :saml_credentials_by_env, :one_password_items, :extracted_aws_secrets

    # @param config [Config] application configuration
    # @param logger [ColorizedLogger] logger instance
    # @param options [Hash] runtime options (e.g. dry_run)
    def initialize(config:, logger:, options: {})
      @config = config
      @logger = logger
      @options = options
      @apps = []
      @saml_credentials_by_env = {}
      @one_password_items = {}
      @extracted_aws_secrets = nil
    end

    # ─── Predicate Checks (used by Runner for hydration) ────────

    # @return [Boolean] true if discovery has run
    def discovery_completed?
      !apps.empty?
    end

    # @return [Boolean] true if SAML Credentials have been extracted
    def saml_credentials_extracted?
      !saml_credentials_by_env.empty?
    end

    def one_password_items_hydrated?
      !one_password_items.empty?
    end

    # @return [Boolean] true if AWS secrets have been extracted
    def aws_secrets_extracted?
      !extracted_aws_secrets.nil?
    end

    # ─── Option Accessors ───────────────────────────────────────

    # @return [Boolean] true if --dry-run was specified
    def dry_run?
      options[:dry_run] === true
    end
  end
end
