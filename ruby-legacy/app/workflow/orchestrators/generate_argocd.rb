# frozen_string_literal: true

require 'yaml'
require_relative '../orchestrator'
require_relative '../../services/filesystem_service'

module Workflow
  module Orchestrators
    class GenerateArgocd < Orchestrator
      def initialize(config:, fs: Services::FilesystemService.new)
        super(config: config)
        @cluster_apps_dir = @config.cluster_apps_dir
        @repo_url = @config.repo_url
        @project_name = @config.project_name
        @target_envs = @config.environments

        @fs = fs
        @pending_writes = {}
      end

      def needs
        [:discovery_completed]
      end

      def act_phase(context)
        context.logger.info "Will generate ArgoCD Application manifests for #{context.apps.count} apps."

        context.apps.each do |app_name|
          @target_envs.each do |env|
            dest_file = File.join(@cluster_apps_dir, "#{app_name}-#{env}.yaml")
            @pending_writes[dest_file] = generate_application_manifest(app_name, env)
          end
        end
      end

      def commit_phase(context)
        context.logger.info 'Writing ArgoCD App manifests...'

        @fs.create_directory(@cluster_apps_dir) unless @fs.directory_exists?(@cluster_apps_dir)

        @pending_writes.each do |dest_file, payload|
          @fs.write_file(dest_file, payload.to_yaml)
        end
      end

      private

      def generate_application_manifest(app_name, env)
        {
          apiVersion: 'argoproj.io/v1alpha1',
          kind: 'Application',
          metadata: {
            name: "#{app_name}-#{env}",
            namespace: 'argocd',
            finalizers: ['resources-finalizer.argocd.argoproj.io']
          },
          spec: {
            project: 'default',
            source: {
              repoURL: @repo_url,
              targetRevision: 'main',
              path: "#{@project_name}-workloads/#{app_name}/overlay/#{env}"
            },
            destination: {
              server: 'https://kubernetes.default.svc',
              namespace: "#{@project_name}-#{env}"
            },
            syncPolicy: {
              automated: {
                prune: true,
                selfHeal: true
              },
              syncOptions: ['CreateNamespace=true']
            }
          }
        }
      end
    end
  end
end
