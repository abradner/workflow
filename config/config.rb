# frozen_string_literal: true

require 'dotenv/load'

class Config
  attr_reader :source_dir, :dest_dir, :cluster_apps_dir, :environments, :source_env
  attr_reader :app_pattern, :project_name, :tld, :repo_url
  attr_reader :talos_item_id, :talos_template_dir
  attr_reader :registry_hostname, :registry_1p_item_id, :external_secrets_api_version

  def initialize
    # Source is the immutable read-only clone
    @source_dir = File.expand_path(ENV.fetch('SOURCE_DIR'))

    # Dest is the GitOps repository for workloads
    @dest_dir = File.expand_path(ENV.fetch('DEST_DIR'))

    # Cluster Apps Dir is where the ArgoCD App-of-Apps manifests go
    @cluster_apps_dir = File.expand_path(ENV.fetch('CLUSTER_APPS_DIR'))

    # Talos bootstrap configuration
    @talos_item_id = ENV.fetch('OP_TALOS_ITEM_ID', nil)
    @talos_template_dir = File.expand_path(ENV.fetch('TALOS_TEMPLATE_DIR'))

    # Source environment for mapping and extraction
    @source_env = ENV.fetch('SOURCE_ENV')

    @environments = ENV.fetch('TARGET_ENVS').split(',').map(&:strip)

    # Application & Environment Parameters
    @app_pattern = ENV.fetch('APP_PATTERN')
    @project_name = ENV.fetch('PROJECT_NAME')
    @tld = ENV.fetch('TLD')
    @repo_url = ENV.fetch('REPO_URL')

    # Private Container Registry
    @registry_hostname = ENV.fetch('REGISTRY_HOSTNAME')
    @registry_1p_item_id = ENV.fetch('REGISTRY_1P_ITEM_ID')

    # Kubernetes API Versions (parametrised for easy upgrades)
    @external_secrets_api_version = 'external-secrets.io/v1'
  end
end
