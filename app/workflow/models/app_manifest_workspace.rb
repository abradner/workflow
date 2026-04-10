# frozen_string_literal: true

module Workflow
  module Models
    # Represents an in-memory workspace of parsed documents for an application workload.
    # Defines the Extracted state mutated progressively by Sequence Transformers.
    class AppManifestWorkspace
      attr_reader :app_name, :source_env, :target_envs
      
      # Hash mapping relative paths to either Arrays (YAML Streams), Hashes, or Strings (raw file data)
      # e.g., 'base/kustomization.yaml' => { kind: 'Kustomization' ... }
      # e.g., 'overlay/dev3/ingress.yaml' => [{ kind: 'Ingress' ... }]
      attr_accessor :manifests

      def initialize(app_name:, source_env:, target_envs:)
        @app_name = app_name
        @source_env = source_env
        @target_envs = target_envs
        @manifests = {}
      end

      # Yields only files isolated to the source overlay directory.
      def source_overlay_files
        @manifests.select { |path, _| path.start_with?("overlay/#{@source_env}/") }
      end
      
      def base_files
        @manifests.select { |path, _| path.start_with?("base/") }
      end

      # Yields files within a specific target overlay directory (eg. `dev4`).
      def target_overlay_files(target_env)
        @manifests.select { |path, _| path.start_with?("overlay/#{target_env}/") }
      end
    end
  end
end
