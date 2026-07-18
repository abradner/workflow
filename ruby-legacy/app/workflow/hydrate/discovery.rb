# frozen_string_literal: true

module Workflow
  module Hydrate
    # Scans the source directory for application folders.
    class Discovery
      def self.call(context)
        require_relative '../../services/filesystem_service'
        fs = Services::FilesystemService.new

        source_dir = context.config.source_dir
        app_pattern = context.config.app_pattern

        apps = fs.list_directories(source_dir, app_pattern).map do |dir|
          fs.base_filename(dir)
        end

        context.logger.info "Discovered #{apps.count} applications."
        context.apps = apps
      end
    end
  end
end
