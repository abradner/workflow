# frozen_string_literal: true

require 'json'
require 'open3'

module ServiceClients
  # Wrapper around 1Password CLI commands
  class Op
    def create_item(item_json)
      stdout, stderr, status = Open3.capture3('op item create -', stdin_data: JSON.generate(item_json))
      raise "Failed to create 1P item: #{stderr}" unless status.success?

      stdout
    end

    # Reads the notesPlain field from a Secure Note item.
    # @param item_id [String] 1Password item ID
    # @return [String] raw note content (YAML string)
    def read_note(item_id)
      stdout, stderr, status = Open3.capture3('op', 'item', 'get', item_id, '--fields', 'notesPlain')
      raise "Failed to read 1P item #{item_id}: #{stderr}" unless status.success?

      content = stdout.strip
      # op CLI sometimes wraps output in double quotes with escaped newlines
      content = content[1..-2].gsub('\n', "\n") if content.start_with?('"') && content.end_with?('"')
      content
    end
  end
end
