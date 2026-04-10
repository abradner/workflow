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
        workspace.manifests.each do |path, docs|
          # Standalone Kustomization hashes
          if docs.is_a?(Hash) && (path.include?('kustomization.yaml') || path.include?('kustomization.yml')) && path.start_with?('overlay/')
            modernize_kustomization(docs, workspace)
            next
          end

          # Document Streams (.yaml/.yml streams normalized)
          docs = mutate_yaml(docs) do |doc|
            next doc unless doc.is_a?(Hash)

            modernize_base_external_secrets!(doc, path)
            strip_topology!(doc)
            modernize_base_ingress!(doc, path)
            modernize_overlay_ingress_patches!(doc, path, workspace)
            doc
          end

          # Secret Overlay JSON patches migration
          if path.include?('secrets.yaml') && path.start_with?('overlay/')
            docs = modernize_overlay_secrets_patches(docs, path, workspace)
          end

          workspace.manifests[path] = docs
        end

        workspace
      end

      private

      def modernize_kustomization(doc, workspace)
        return unless doc[:patches]

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
        return unless workspace.manifests['base/secrets.yaml']
        
        secret_name = workspace.manifests['base/secrets.yaml'].is_a?(Array) ? workspace.manifests['base/secrets.yaml'].first.dig(:metadata, :name) : workspace.manifests['base/secrets.yaml'].dig(:metadata, :name)
        es_version = @external_secrets_api_version.split('/').last
        doc[:patches] << {
          path: 'secrets.yaml',
          target: { group: 'external-secrets.io', version: es_version, kind: 'ExternalSecret', name: secret_name }
        }
      end

      def modernize_base_external_secrets!(doc, path)
        if doc[:kind] == 'ExternalSecret' && path.start_with?('base/')
          doc[:apiVersion] = @external_secrets_api_version
          doc[:spec] ||= {}
          doc[:spec][:secretStoreRef] = { name: 'onepassword-backend', kind: 'ClusterSecretStore' }
          doc[:spec][:refreshInterval] = '1h'
        end
      end

      def strip_topology!(doc)
        if doc[:kind] == 'Deployment'
          doc.dig(:spec, :template, :spec)&.delete(:topologySpreadConstraints)
        end
      end

      def modernize_base_ingress!(doc, path)
        if doc[:kind] == 'Ingress' && path.start_with?('base/')
          doc[:apiVersion] = 'gateway.networking.k8s.io/v1'
          doc[:kind] = 'HTTPRoute'
          
          if doc[:spec] && doc[:spec][:rules] && doc[:spec][:rules][0]
            rule = doc[:spec][:rules][0]
            
            host_value = rule[:host]
            service_name = rule.dig(:http, :paths, 0, :backend, :service, :name)
            service_port = rule.dig(:http, :paths, 0, :backend, :service, :port, :number)
            
            doc[:spec] = {
              parentRefs: [{ name: 'homelab-gateway', namespace: 'default' }],
              hostnames: [host_value].compact,
              rules: [{ backendRefs: [{ name: service_name, port: service_port }.compact] }]
            }
          end
        end
      end

      def modernize_overlay_ingress_patches!(doc, path, workspace)
        if path.include?('ingress.yaml') && path.start_with?('overlay/')
          env = extract_env(path)
          fqdn = "#{workspace.app_name}.#{@project_name}.#{env}.#{@tld}"

          if doc[:op] == 'replace' && doc[:path]&.include?('host')
            doc[:path] = '/spec/hostnames/0'
            doc[:value] = fqdn
          end
        end
      end

      def modernize_overlay_secrets_patches(docs_array, path, workspace)
        env = extract_env(path)
        base_secret_doc = workspace.manifests['base/secrets.yaml']
        base_secret_doc = base_secret_doc.first if base_secret_doc.is_a?(Array)
        return docs_array unless base_secret_doc

        docs_array.is_a?(Array) ? docs_array.flat_map do |patch|
          # Only intercept AWS legacy replacements
          if patch.is_a?(Hash) && patch[:op] == 'replace' && patch[:path].to_s.include?('remoteRef/key')
            aws_val = patch[:value]
            parts = aws_val.split('/')
            parts.shift if parts.length > 1
            section_id = parts.join('-')

            path_parts = patch[:path].split('/')
            index = path_parts[3].to_i

            base_property = base_secret_doc.dig(:spec, :data, index, :remoteRef, :property) || 'password'
            new_key = "k8s-#{@project_name}-#{env}/#{section_id}/#{base_property}"

            [
              { op: 'replace', path: patch[:path], value: new_key },
              { op: 'remove', path: "/spec/data/#{index}/remoteRef/property" }
            ]
          else
            patch
          end
        end : docs_array
      end

      def extract_env(path)
        parts = path.split('/')
        env_index = parts.index('overlay') + 1
        parts[env_index]
      end
    end
  end
end
