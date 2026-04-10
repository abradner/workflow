# frozen_string_literal: true

require_relative '../orchestrator'
require_relative '../../services/filesystem_service'
require_relative '../hydrate/workspace_extractor'
require_relative '../transformers/environment_generator'
require_relative '../transformers/legacy_modernizer'
require_relative '../transformers/pull_secret_injector'
require_relative '../transformers/service_abstraction_linker'

module Workflow
  module Orchestrators
    class SyncWorkloads < Orchestrator
      def initialize(config:, fs: Services::FilesystemService.new)
        super(config: config)
        @fs = fs
        @extractor = Hydrate::WorkspaceExtractor.new(config: @config, fs: @fs)
        # Transformer Ordering is explicitly sequential:
        # 1. EnvironmentGenerator MUST run first (duplicates memory configurations)
        # 2. Modernizer / Injectors / Linkers (mutates generated resources independently)
        @transformers = [
          Transformers::EnvironmentGenerator.new,
          Transformers::LegacyModernizer.new(
            external_secrets_api_version: @config.external_secrets_api_version,
            project_name: @config.project_name,
            tld: @config.tld
          ),
          Transformers::PullSecretInjector.new(
            registry_hostname: @config.registry_hostname,
            registry_1p_item_id: @config.registry_1p_item_id,
            external_secrets_api_version: @config.external_secrets_api_version
          ),
          Transformers::ServiceAbstractionLinker.new(
            project_name: @config.project_name,
            tld: @config.tld
          )
        ]
        @planned_workspaces = []
      end

      def needs
        [:discovery_completed]
      end

      def act_phase(context)
        context.logger.info "Will synchronize workloads for #{context.apps.count} applications."

        context.apps.each do |app_name|
          context.logger.info "Extracting workspace for #{app_name}"
          # Pure Extract
          workspace = @extractor.extract(app_name)
          context.logger.info "Extracted #{workspace.manifests.keys.count} initial files: #{workspace.manifests.keys.join(', ')}"

          # Pure Sequenced Transform Pipeline
          @transformers.each do |transformer|
            context.logger.info "Running #{transformer.class.name.split('::').last}..."
            workspace = transformer.call(workspace)
          end

          context.logger.info "Final planned workspace has #{workspace.manifests.keys.count} files."
          @planned_workspaces << workspace
        end
      end

      def commit_phase(context)
        # 100% Side-effect free execution loop. 
        # Writing final state arrays to the configured disk natively.
        context.logger.info "Commit phase starting for #{@planned_workspaces.count} workspaces targeting #{@config.dest_dir}..."

        @planned_workspaces.each do |workspace|
          context.logger.info "Committing #{workspace.app_name} configs..."
          workspace.manifests.each do |virtual_path, content|
            # Reconstruct the absolute path mapping
            dest_file = File.join(@config.dest_dir, workspace.app_name, virtual_path)
            
            context.logger.info " -> Writing #{dest_file}"
            @fs.create_directory(File.dirname(dest_file))

            if ['.yaml', '.yml'].include?(@fs.extension(dest_file))
              if content.is_a?(Array)
                @fs.write_yaml_stream(dest_file, content)
              else
                @fs.write_yaml(dest_file, content)
              end
            else
              @fs.write_file(dest_file, content)
            end
          end
        end
      end
    end
  end
end
