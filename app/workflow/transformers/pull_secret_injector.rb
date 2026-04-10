# frozen_string_literal: true

require_relative 'base'
require_relative '../../domain/kubernetes/external_secret'
require 'json'

module Workflow
  module Transformers
    # Injects the registry pull secret payload natively into the workspace
    class PullSecretInjector < Base
      def initialize(registry_hostname:, registry_1p_item_id:, external_secrets_api_version:)
        @registry_hostname = registry_hostname
        @registry_1p_item_id = registry_1p_item_id
        @external_secrets_api_version = external_secrets_api_version
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

        secret = Kubernetes::ExternalSecret.new(
          name: 'registry-pull-secret',
          api_version: @external_secrets_api_version,
          store_name: 'onepassword-backend',
          template_type: 'kubernetes.io/dockerconfigjson',
          template_data: { '.dockerconfigjson' => dockerconfig },
          data_refs: [
            { secret_key: 'username', key: @registry_1p_item_id, property: 'username' },
            { secret_key: 'password', key: @registry_1p_item_id, property: 'password' }
          ]
        )

        workspace.manifests['base/registry-pull-secret.yaml'] = [secret.to_h]

        workspace.manifests.each do |path, docs|
          if docs.is_a?(Array)
            docs.map! do |doc|
              next doc unless doc.is_a?(Hash)

              # Add image pull secret reference to Service Accounts
              if doc[:kind] == 'ServiceAccount'
                doc[:imagePullSecrets] ||= []
                doc[:imagePullSecrets] << { name: 'registry-pull-secret' } unless doc[:imagePullSecrets].any? { |s| s[:name] == 'registry-pull-secret' }
              end
              doc
            end
          elsif docs.is_a?(Hash) && (path.include?('kustomization.yaml') || path.include?('kustomization.yml'))
            # Only add to base kustomizations
            if path.start_with?('base/')
              docs[:resources] ||= []
              docs[:resources] << 'registry-pull-secret.yaml' unless docs[:resources].include?('registry-pull-secret.yaml')
            end
          end
        end

        workspace
      end
    end
  end
end
