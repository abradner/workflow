# frozen_string_literal: true

require_relative '../models/app_manifest_workspace'

module Workflow
  module Hydrate
    # Traverses the filesystem to load a target namespace and perfectly capture all definitions
    # inside base and the source overly targeting into a parsed Workspace struct.
    class WorkspaceExtractor
      def initialize(config:, fs: Services::FilesystemService.new)
        @config = config
        @fs = fs
      end

      # Extracts all relevant data into a single side-effect free structure representing
      # the start of the synchronization pipeline.
      def extract(app_name)
        workspace = Models::AppManifestWorkspace.new(
          app_name: app_name,
          source_env: @config.source_env,
          target_envs: @config.environments
        )

        load_path(workspace, File.join(@config.source_dir, app_name, 'base'), 'base')
        load_path(workspace, File.join(@config.source_dir, app_name, 'overlay', @config.source_env), "overlay/#{@config.source_env}")

        workspace
      end

      private

      def load_path(workspace, fs_path, virtual_prefix, base_fs_path = fs_path)
        return unless @fs.directory_exists?(fs_path)

        @fs.path_entries(fs_path).each do |src_file|
          if @fs.directory_exists?(src_file)
            load_path(workspace, src_file, virtual_prefix, base_fs_path)
            next
          end

          relative_filename = src_file.sub("#{base_fs_path}/", '')
          virtual_path = "#{virtual_prefix}/#{relative_filename}"

          workspace.manifests[virtual_path] =
            @fs.yaml?(relative_filename) ? @fs.read_yaml(src_file) : @fs.read_file(src_file)
        end
      end
    end
  end
end
