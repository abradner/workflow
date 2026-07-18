# frozen_string_literal: true

require 'yaml'

module Services
  # Pure-logic service for template rendering.
  # Takes template content (string) and a secrets hash, returns rendered content.
  class TemplateRenderingService
    PLACEHOLDER_REGEX = /\{\{\s*([\w.]+)\s*\}\}/

    # Recursively flattens a nested hash into dot-separated keys.
    # @param hash [Hash] nested hash
    # @param parent_key [String] prefix for keys
    # @param sep [String] separator (default '.')
    # @return [Hash] flat hash with dot-separated keys
    def flatten_hash(hash, parent_key = '', sep = '.')
      hash.each_with_object({}) do |(k, v), h|
        new_key = parent_key.empty? ? k.to_s : "#{parent_key}#{sep}#{k}"
        if v.is_a?(Hash)
          h.merge!(flatten_hash(v, new_key, sep))
        else
          h[new_key] = v
        end
      end
    end

    # Returns the set of placeholder keys found in the template content.
    # @param template_content [String]
    # @return [Array<String>] sorted unique placeholder keys
    def extract_placeholders(template_content)
      template_content.scan(PLACEHOLDER_REGEX).flatten.uniq.sort
    end

    # Validates that all placeholders in the template can be resolved.
    # @param template_content [String]
    # @param flat_secrets [Hash] flattened secrets hash
    # @return [Array<String>] list of missing keys (empty if all resolve)
    def missing_keys(template_content, flat_secrets)
      extract_placeholders(template_content) - flat_secrets.keys
    end

    # Renders template content by replacing {{ key }} placeholders with values.
    # @param template_content [String] the template string
    # @param flat_secrets [Hash] flat hash of dotted keys to values
    # @return [String] rendered content
    # @raise [KeyError] if any placeholder cannot be resolved
    def render(template_content, flat_secrets)
      missing = missing_keys(template_content, flat_secrets)
      raise KeyError, "Missing secret keys: #{missing.join(', ')}" unless missing.empty?

      template_content.gsub(PLACEHOLDER_REGEX) do |_match|
        key = Regexp.last_match(1)
        flat_secrets[key].to_s
      end
    end
  end
end
