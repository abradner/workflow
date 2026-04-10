# frozen_string_literal: true

require_relative 'base'

module Workflow
  module Transformers
    # Migrates aging representations of workloads into modern cloud native representations
    class LegacyModernizer < Base
      def initialize(external_secrets_api_version:, project_name:, tld:)
        @external_secrets_api_version = external_secrets_api_version
        @project_name = project_name
        @tld = tld
      end

      def call(workspace)
        es_version = @external_secrets_api_version.split('/').last
        project = @project_name

        workspace.manifests.each do |path, docs|
          # Document Streams (.yaml/.yml streams)
          if docs.is_a?(Array)
            docs.map! do |doc|
              next doc unless doc.is_a?(Hash)

              # Update Base External Secrets
              if doc[:kind] == 'ExternalSecret' && path.start_with?('base/')
                doc[:apiVersion] = @external_secrets_api_version
                doc[:spec] ||= {}
                doc[:spec][:secretStoreRef] = { name: 'onepassword-backend', kind: 'ClusterSecretStore' }
              end

              # Strip topology from Deployments
              if doc[:kind] == 'Deployment'
                doc.dig(:spec, :template, :spec)&.delete(:topologySpreadConstraints)
              end

              # Convert Ingress patches to HTTPRoute patches natively if they exist as array items
              if path.include?('ingress.yaml') && path.start_with?('overlay/')
                env = extract_env(path)
                fqdn = "#{workspace.app_name}.#{project}.#{env}.#{@tld}"

                if doc[:op] == 'replace' && doc[:path]&.include?('host')
                  doc[:path] = '/spec/hostnames/0'
                  doc[:value] = fqdn
                end
              end
              
              doc
            end

            # Secret Overlay JSON patches migration
            if path.include?('secrets.yaml') && path.start_with?('overlay/')
              env = extract_env(path)
              base_secret_doc = workspace.manifests['base/secrets.yaml']&.first

              if base_secret_doc
                transformed_patches = docs.filter_map do |patch|
                  # Only intercept AWS legacy replacements
                  if patch.is_a?(Hash) && patch[:op] == 'replace' && patch[:path].to_s.include?('remoteRef/key')
                    aws_val = patch[:value]
                    parts = aws_val.split('/')
                    parts.shift if parts.length > 1
                    section_id = parts.join('-')
      
                    path_parts = patch[:path].split('/')
                    index = path_parts[3].to_i
      
                    base_property = base_secret_doc.dig(:spec, :data, index, :remoteRef, :property) || 'password'
                    new_key = "k8s-#{project}-#{env}/#{section_id}/#{base_property}"
      
                    [
                      { op: 'replace', path: patch[:path], value: new_key },
                      { op: 'remove', path: "/spec/data/#{index}/remoteRef/property" }
                    ]
                  else
                    patch
                  end
                end.flatten
                workspace.manifests[path] = transformed_patches
              end
            end

          elsif docs.is_a?(Hash)
            # Standalone Kustomization hashes
            doc = docs
            if (path.include?('kustomization.yaml') || path.include?('kustomization.yml')) && path.start_with?('overlay/')
              if doc[:patches]
                # Filter old explicit secrets.yaml
                doc[:patches].reject! { |p| p[:path] == 'secrets.yaml' }

                # Modernize Ingress patch target properties
                doc[:patches].each do |p|
                  if p[:target] && p[:target][:kind] == 'Ingress'
                    p[:target][:group] = 'gateway.networking.k8s.io'
                    p[:target][:version] = 'v1'
                    p[:target][:kind] = 'HTTPRoute'
                  end
                end

                # Re-inject the modern secrets.yaml patch targeting ExternalSecret
                if workspace.manifests['base/secrets.yaml']&.first
                  secret_name = workspace.manifests['base/secrets.yaml'].first.dig(:metadata, :name)
                  doc[:patches] << {
                    path: 'secrets.yaml',
                    target: {
                      group: 'external-secrets.io',
                      version: es_version,
                      kind: 'ExternalSecret',
                      name: secret_name
                    }
                  }
                end
              end
            end
          end
        end

        workspace
      end

      private

      def extract_env(path)
        parts = path.split('/')
        env_index = parts.index('overlay') + 1
        parts[env_index]
      end
    end
  end
end
