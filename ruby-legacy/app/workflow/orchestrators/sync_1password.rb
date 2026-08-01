# frozen_string_literal: true

require_relative '../orchestrator'
require_relative '../../services/aws_secrets_service'
require_relative '../../services/one_password_commit_service'
require_relative '../transformers/one_password_item_mapper'
require_relative '../transformers/one_password_environment_replacer'
require_relative '../transformers/one_password_saml_key_injector'

module Workflow
  module Orchestrators
    class Sync1Password < Orchestrator
      needs :saml_credentials_extracted
      needs :aws_secrets_extracted
      needs :one_password_items_hydrated

      def initialize(config:)
        super
        @project_name = config.project_name
        @aws_service = Services::AwsSecretsService.new
        @op_commit_service = Services::OnePasswordCommitService.new
      end

      def act_phase(context)
        source_env = @config.source_env
        envs = @config.environments

        extracted_secrets = context.extracted_aws_secrets
        
        context.logger.info "Will map 1Password Items for environments: #{envs.join(', ')}"
        
        # 1. Transform raw extracted secrets directly into the domain object representing the vault state
        mapper = Transformers::OnePasswordItemMapper.new
        
        envs.each do |env|
          mapper.call(
            env: env, 
            extracted_secrets: extracted_secrets, 
            domain_items_map: context.one_password_items,
            logger: context.logger
          )
          
          # 2. Translate environment variables natively inside the mapped Domain fields
          env_replacer = Transformers::OnePasswordEnvironmentReplacer.new(
            source_env: source_env,
            target_env: env,
            logger: context.logger
          )
          
          env_replacer.call(context.one_password_items[env])
          
          # 3. Inject dynamically generated payload overrides where applicable
          kc_public_key = context.saml_credentials_by_env[env]&.pem_public_key
          
          saml_injector = Transformers::OnePasswordSamlKeyInjector.new(
            kc_public_key: kc_public_key,
            logger: context.logger
          )
          
          saml_injector.call(context.one_password_items[env])
        end
      end

      def commit_phase(context)
        # Push cleanly built, natively tracked domain objects to the vault
        @config.environments.each do |env|
          domain_item = context.one_password_items[env]
          next unless domain_item

          @op_commit_service.commit(domain_item)
          context.logger.info "Created k8s-#{@project_name}-#{env} successfully!"
        end
      end
    end
  end
end
