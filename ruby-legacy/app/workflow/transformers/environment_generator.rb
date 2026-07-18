# frozen_string_literal: true

require_relative 'base'

module Workflow
  module Transformers
    # Clones the isolated source_env configurations and bootstraps all target environment configurations
    class EnvironmentGenerator < Base
      def call(workspace)
        source_env = workspace.source_env
        overlay_files = workspace.source_overlay_files

        workspace.target_envs.each do |env|
          # Skip cloning mapping loops locally if target implicitly equals source
          next if env == source_env

          overlay_files.each do |virtual_path, content|
            new_path = virtual_path.sub("overlay/#{source_env}", "overlay/#{env}")
            
            # Deep clone and replace string instances of source_env with env
            workspace.manifests[new_path] = deep_replace(content, source_env, env)
          end
        end

        # Drop the source environment configurations unless explicitly matched in deployment
        unless workspace.target_envs.include?(source_env)
          workspace.manifests.delete_if { |virtual_path, _| virtual_path.start_with?("overlay/#{source_env}/") }
        end

        workspace
      end

      private

      def deep_replace(node, source, target)
        case node
        when Hash
          node.transform_values { |v| deep_replace(v, source, target) }
        when Array
          node.map { |v| deep_replace(v, source, target) }
        when String
          node.gsub(source, target)
        else
          node
        end
      end
    end
  end
end
