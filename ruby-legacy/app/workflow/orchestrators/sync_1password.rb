# frozen_string_literal: true

require_relative '../orchestrator'
require_relative '../../services/aws_secrets_service'
require_relative '../../services/one_password_service'
require_relative '../transformers/one_password_saml_key_injector'

module Workflow
  module Orchestrators
    class Sync1Password < Orchestrator
      needs :saml_credentials_extracted

      def initialize(config:)
        super
        @project_name = config.project_name
        @aws_service = Services::AwsSecretsService.new
        @op_service = Services::OnePasswordService.new(project_name: @project_name)
      end

      # Not reliant on app discovery
      def act_phase(context)
        source_env = @config.source_env
        envs = @config.environments

        context.logger.info "Will extract AWS secrets for environment #{source_env}"

        extracted_secrets = @aws_service.extract_secrets(source_env)

        context.logger.info "Extracted #{extracted_secrets.count} secrets from AWS."
        context.logger.info "Will map 1Password Items for environments: #{envs.join(', ')}"
        
        @mapped_vault_items = {}
        
        envs.each do |env|
          kc_public_key = context.saml_credentials_by_env[env]&.pem_public_key
          
          mapper = Transformers::OnePasswordSamlKeyInjector.new(
            source_env: source_env,
            target_env: env,
            kc_public_key: kc_public_key,
            logger: context.logger
          )
          
          @mapped_vault_items[env] = mapper.call(extracted_secrets)
        end
      end

      def commit_phase(context)
        # Push to 1Password for each target env
        @config.environments.each do |env|
          context.logger.info "Pushing 1Password Vault Item: k8s-#{@project_name}-#{env} ..."

          # We pass env explicitly so 1PassService creates one single Item per target env securely
          @op_service.ingest_vault_item(env, @mapped_vault_items[env])

          context.logger.info "Created k8s-#{@project_name}-#{env} successfully!"
        end
      end
    end
  end
end
