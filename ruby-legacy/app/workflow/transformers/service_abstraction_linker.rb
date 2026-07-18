# frozen_string_literal: true

require_relative 'base'
require_relative '../../services/endpoint_mapper'

module Workflow
  module Transformers
    # Injects the Service Abstraction Pattern (ExternalName services) by parsing string literals,
    # mutating application configuration dynamically to .cluster.local, and provisioning supporting k8s external services.
    class ServiceAbstractionLinker < Base
      def initialize(project_name:, tld:)
        @project_name = project_name
        @tld = tld
      end

      def call(workspace)
        workspace.manifests.to_a.each do |path, doc|
          next unless doc.is_a?(Hash) && doc[:kind] == 'Kustomization' && path.start_with?('overlay/')
          
          env = extract_env(path)
          discovered_services = process_configmap_generator(doc[:configMapGenerator], env, workspace.app_name)

          if discovered_services && discovered_services.any?
            generate_external_services(discovered_services, env, workspace)
            
            doc[:resources] ||= []
            doc[:resources] << 'external-services.yaml' unless doc[:resources].include?('external-services.yaml')
          end
        end

        workspace
      end

      private

      def process_configmap_generator(generators, env, app_name)
        return [] unless generators
        
        discovered_services = []

        generators.each do |cm|
          next unless cm[:literals]

          cm[:literals].map! do |lit|
            services, mapped_lit = map_literal(lit.dup, env, app_name)
            discovered_services.concat(services)
            mapped_lit
          end
        end
        discovered_services.uniq
      end

      def map_literal(lit, env, app_name)
        services = []
        if lit.include?('=')
          key, value = lit.split('=', 2)
          services += map_suffix_patterns(value, env, app_name)
          lit = "#{key}=#{value}"
        end

        map_known_services!(lit, env, app_name, services)
        [services, lit]
      end

      def map_suffix_patterns(value_ref, env, app_name)
        services = []
        Services::EndpointMapper::SUFFIX_MAPPINGS.each do |suffix, resource|
          next unless value_ref.include?(suffix.delete_prefix('.'))

          value_ref.scan(/[a-zA-Z0-9._-]+#{Regexp.escape(suffix)}/).each do |matched_host|
            prefixed_resource = "#{app_name}-#{resource}"
            cluster_dns = "#{prefixed_resource}.#{@project_name}-#{env}.svc.cluster.local"
            value_ref.gsub!(matched_host, cluster_dns)
            
            ext_dns = "#{resource}.#{@project_name}.#{env}.#{@tld}"
            services << { resource: prefixed_resource, ext_dns: ext_dns }
          end
        end
        services
      end

      def map_known_services!(lit, env, app_name, services)
        Services::EndpointMapper::KNOWN_SERVICES.each do |resource|
          ext_dns = "#{resource}.#{@project_name}.#{env}.#{@tld}"
          prefixed_resource = "#{app_name}-#{resource}"
          cluster_dns = "#{prefixed_resource}.#{@project_name}-#{env}.svc.cluster.local"

          if lit.include?(ext_dns)
            lit.gsub!(ext_dns, cluster_dns)
            services << { resource: prefixed_resource, ext_dns: ext_dns }
          end
        end
      end

      def generate_external_services(services, env, workspace)
        external_services_docs = services.map do |srv|
          {
            apiVersion: 'v1',
            kind: 'Service',
            metadata: {
              name: srv[:resource],
              namespace: "#{@project_name}-#{env}"
            },
            spec: {
              type: 'ExternalName',
              externalName: srv[:ext_dns]
            }
          }
        end

        workspace.manifests["overlay/#{env}/external-services.yaml"] = external_services_docs
      end

      def extract_env(path)
        parts = path.split('/')
        env_index = parts.index('overlay') + 1
        parts[env_index]
      end
    end
  end
end
