require_relative '../orchestrator'
require_relative '../../services/filesystem_service'
require_relative '../../services/endpoint_mapper'
require_relative '../../domain/kubernetes/external_secret'
require_relative '../../domain/kubernetes/http_route'
require 'json'

module Workflow
  module Orchestrators
    class SyncWorkloads < Orchestrator
      def initialize(config:, fs: Services::FilesystemService.new)
        super(config: config)
        @tld = @config.tld
        @project_name = @config.project_name
        @source_dir = @config.source_dir
        @dest_dir = @config.dest_dir
        @external_secrets_api_version = @config.external_secrets_api_version
        @registry_hostname = @config.registry_hostname
        @registry_1p_item_id = @config.registry_1p_item_id
        @source_env = @config.source_env
        @target_envs = @config.environments

        @fs = fs
        @pending_writes = {}
        @pending_copies = {}
      end

      def needs
        [:discovery_completed]
      end

      def act_phase(context)
        context.logger.info "Will synchronize workloads for #{context.apps.count} applications."

        context.apps.each do |app_name|
          plan_migrate_base(app_name)

          @target_envs.each do |env|
            plan_generate_overlay(app_name, env)
          end
        end
      end

      def commit_phase(_context)
        # Execute pure IO against the filesystem mapping generated during ACT
        @pending_copies.each do |src_file, dest_file|
          @fs.create_directory(File.dirname(dest_file))
          if @fs.directory_exists?(src_file)
            @fs.copy_directory(src_file, dest_file)
          else
            @fs.copy_file(src_file, dest_file)
          end
        end

        @pending_writes.each do |dest_file, payload|
          @fs.create_directory(File.dirname(dest_file))

          if ['.yaml', '.yml'].include?(@fs.extension(dest_file))
            if payload.is_a?(Array)
              @fs.write_yaml_stream(dest_file, payload)
            else
              @fs.write_yaml(dest_file, payload)
            end
          else
            @fs.write_file(dest_file, payload)
          end
        end
      end

      private

      def plan_migrate_base(app_name)
        base_src = File.join(@source_dir, app_name, 'base')
        return unless @fs.directory_exists?(base_src)

        base_dest = File.join(@dest_dir, app_name, 'base')

        @fs.path_entries(base_src).each do |src_file|
          filename = @fs.base_filename(src_file)
          dest_file = File.join(base_dest, filename)

          if ['.yaml', '.yml'].include?(@fs.extension(filename))
            docs = @fs.read_yaml_stream(src_file)
            transformed_docs = docs.compact.map { |d| transform_by_kind(d) }
            @pending_writes[dest_file] = transformed_docs
          else
            @pending_copies[src_file] = dest_file
          end
        end

        # Generate the registry pull secret ExternalSecret
        plan_generate_registry_secret(app_name, base_dest)
      end

      # Routes a doc through the appropriate transform based on its kind.
      # Kustomization docs also get the registry secret resource appended.
      def transform_by_kind(doc)
        case doc[:kind]
        when 'Ingress'
          Kubernetes::HTTPRoute.from_ingress(doc)
        when 'ExternalSecret'
          transform_external_secret_base!(doc)
        when 'Deployment'
          strip_topology_constraints!(doc)
        when 'ServiceAccount'
          add_image_pull_secrets!(doc)
        when 'Kustomization'
          add_registry_secret_resource!(doc)
        else
          doc
        end
      end

      def transform_external_secret_base!(doc)
        doc[:apiVersion] = @external_secrets_api_version
        doc[:spec] ||= {}
        doc[:spec][:secretStoreRef] = {
          name: 'onepassword-backend',
          kind: 'ClusterSecretStore'
        }
        doc
      end

      def strip_topology_constraints!(doc)
        template_spec = doc.dig(:spec, :template, :spec)
        template_spec&.delete(:topologySpreadConstraints)
        doc
      end

      def add_image_pull_secrets!(doc)
        doc[:imagePullSecrets] = [{ name: 'registry-pull-secret' }]
        doc
      end

      def add_registry_secret_resource!(doc)
        doc[:resources] ||= []
        doc[:resources] << 'registry-pull-secret.yaml' unless doc[:resources].include?('registry-pull-secret.yaml')
        doc
      end

      def plan_generate_registry_secret(_app_name, base_dest)
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

        dest_file = File.join(base_dest, 'registry-pull-secret.yaml')
        @pending_writes[dest_file] = [secret.to_h]
      end

      def plan_generate_overlay(app_name, env)
        src_env_path = File.join(@source_dir, app_name, 'overlay', @source_env)
        return unless @fs.directory_exists?(src_env_path)

        overlay_dest = File.join(@dest_dir, app_name, 'overlay', env)

        @fs.path_entries(src_env_path).each do |src_file|
          plan_process_overlay_file(src_file, overlay_dest, app_name, env, src_env_path)
        end
      end

      def plan_process_overlay_file(src_file, overlay_dest, app_name, env, src_env_path)
        filename = @fs.base_filename(src_file)
        ext = @fs.extension(filename)
        dest_file = File.join(overlay_dest, filename)

        return if filename == 'secrets.yaml'

        if %w[kustomization.yaml kustomization.yml].include?(filename)
          plan_process_kustomization(src_file, dest_file, overlay_dest, app_name, env, src_env_path)
        elsif filename == 'ingress.yaml'
          plan_process_ingress(src_file, dest_file, app_name, env)
        elsif @fs.directory_exists?(src_file)
          @pending_copies[src_file] = dest_file
        else
          content = @fs.read_file(src_file)
          content = content.gsub(@source_env, env) if ['.yaml', '.yml', '.md'].include?(ext)
          @pending_writes[dest_file] = content
        end
      end

      def plan_process_kustomization(src_file, dest_file, overlay_dest, app_name, env, src_env_path)
        doc = @fs.read_yaml(src_file)

        doc[:namespace] = doc[:namespace].gsub(@source_env, env) if doc[:namespace]

        if doc[:patches]
          doc[:patches].reject! { |p| p[:path] == 'secrets.yaml' }
          doc[:patches].each do |p|
            next unless p[:target] && p[:target][:kind] == 'Ingress'

            p[:target][:group] = 'gateway.networking.k8s.io'
            p[:target][:version] = 'v1'
            p[:target][:kind] = 'HTTPRoute'
          end

          plan_process_secrets_patch(doc, overlay_dest, app_name, env, src_env_path)
        end

        plan_process_configmap_generator(doc[:configMapGenerator], env) if doc[:configMapGenerator]

        @pending_writes[dest_file] = doc
      end

      def plan_process_secrets_patch(doc, overlay_dest, app_name, env, src_env_path)
        # NOTE: We read the base secret from the pending writes dictionary first, falling back to disk
        base_secret_path = File.join(@dest_dir, app_name, 'base', 'secrets.yaml')
        src_env_secret_patch_path = File.join(src_env_path, 'secrets.yaml')

        unless @fs.file_exists?(src_env_secret_patch_path) && (@fs.file_exists?(base_secret_path) || @pending_writes[base_secret_path])
          return
        end

        # Hydrate from pending writes if modified this run, else fallback
        base_secret_doc = @pending_writes[base_secret_path]&.first || @fs.read_yaml_stream(base_secret_path).first

        secret_name = base_secret_doc.dig(:metadata, :name)
        es_version = @external_secrets_api_version.split('/').last

        doc[:patches] << {
          path: 'secrets.yaml',
          target: {
            group: 'external-secrets.io',
            version: es_version,
            kind: 'ExternalSecret',
            name: secret_name
          }
        }

        original_patches = @fs.read_yaml_stream(src_env_secret_patch_path).flatten

        transformed_patches = original_patches.filter_map do |patch|
          next unless patch.is_a?(Hash) && patch[:op] == 'replace' && patch[:path].to_s.include?('remoteRef/key')

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
        end.flatten

        @pending_writes[File.join(overlay_dest, 'secrets.yaml')] = transformed_patches
      end

      def plan_process_configmap_generator(generators, env)
        generators.each do |cm|
          next unless cm[:literals]

          cm[:literals].map! do |lit|
            if lit.include?('=')
              key, value = lit.split('=', 2)
              Services::EndpointMapper::SUFFIX_MAPPINGS.each do |suffix, resource|
                next unless value.include?(suffix.delete_prefix('.'))

                value.scan(/[a-zA-Z0-9._-]+#{Regexp.escape(suffix)}/).each do |matched_host|
                  fqdn = "#{resource}.#{@project_name}.#{env}.#{@tld}"
                  value = value.gsub(matched_host, fqdn)
                end
              end
              lit = "#{key}=#{value}"
            end
            lit = lit.gsub(@source_env, env)
            lit
          end
        end
      end

      def plan_process_ingress(src_file, dest_file, app_name, env)
        docs = @fs.read_yaml_stream(src_file)
        fqdn = "#{app_name}.#{@project_name}.#{env}.#{@tld}"

        docs.map! do |doc|
          if doc.is_a?(Array)
            doc.each do |op|
              if op[:path]&.include?('host')
                op[:path] = '/spec/hostnames/0'
                op[:value] = fqdn
              end
            end
          elsif doc.is_a?(Hash) && doc[:op] == 'replace'
            if doc[:path]&.include?('host')
              doc[:path] = '/spec/hostnames/0'
              doc[:value] = fqdn
            end
          end
          doc
        end

        @pending_writes[dest_file] = docs
      end
    end
  end
end
