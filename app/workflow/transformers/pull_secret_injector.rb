# frozen_string_literal: true

require_relative 'base'
require_relative '../../domain/kubernetes/external_secret'
require 'json'

module Workflow
  module Transformers
    # Injects the registry pull secret payload natively into the workspace
    class PullSecretInjector < Base
      def initialize(registry_hostname:, registry_1p_item_id:, external_secrets_api_version:, project_name:)
        @registry_hostname = registry_hostname
        @registry_1p_item_id = registry_1p_item_id
        @external_secrets_api_version = external_secrets_api_version
        @project_name = project_name
      end

      def call(workspace)
        # Synthesize the registry-pull-secret
        dockerconfig = JSON.generate({
          auths: {
            @registry_hostname => {
              username: '{{ .username }}',
              password: '{{ .password }}'
            }
          }
        })

        unique_secret_name = "#{workspace.app_name}-registry"

        secret = Kubernetes::ExternalSecret.new(
          name: unique_secret_name,
          api_version: @external_secrets_api_version,
          store_name: 'onepassword-backend',
          template_type: 'kubernetes.io/dockerconfigjson',
          template_data: { '.dockerconfigjson' => dockerconfig },
          data_refs: [
            { secret_key: 'username', key: @registry_1p_item_id, property: 'username' },
            { secret_key: 'password', key: @registry_1p_item_id, property: 'password' }
          ]
        )

        workspace.manifests['base/registry-pull-secret.yaml'] = secret.to_h.merge(spec: secret.to_h[:spec].merge(refreshInterval: '24h'))

        workspace.manifests.each do |path, docs|
          if docs.is_a?(Hash) && (path.include?('kustomization.yaml') || path.include?('kustomization.yml'))
            # Only add to base kustomizations
            if path.start_with?('base/')
              docs[:resources] ||= []
              docs[:resources] << 'registry-pull-secret.yaml' unless docs[:resources].include?('registry-pull-secret.yaml')
            end
            next
          end

          docs = mutate_yaml(docs) do |doc|
            next doc unless doc.is_a?(Hash)

            # Add image pull secret reference to Service Accounts
            if doc[:kind] == 'ServiceAccount'
              doc[:imagePullSecrets] ||= []
              doc[:imagePullSecrets] << { name: unique_secret_name } unless doc[:imagePullSecrets].any? { |s| s[:name] == unique_secret_name }
            end
            doc
          end

          # Map Registry URIs natively out of overlays
          if path.include?('deployment.yaml') && path.start_with?('overlay/') && docs.is_a?(Array)
            docs.each do |patch|
              if patch.is_a?(Hash) && patch[:op] == 'replace' && patch[:path].to_s.include?('image')
                image_repo_tag = patch[:value].split('/').last
                patch[:value] = "#{@registry_hostname}/#{@project_name}/#{image_repo_tag}"
              end
            end
          end

          workspace.manifests[path] = docs
        end

        workspace
      end
    end
  end
end
