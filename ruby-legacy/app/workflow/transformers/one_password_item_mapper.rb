# frozen_string_literal: true

require_relative 'base'

module Workflow
  module Transformers
    # Side-effect free transformer handling translation of AWS Secrets
    # to 1Password Item sections and fields natively mapped to Domain objects.
    class OnePasswordItemMapper < Base
      # What: Transforms flat AWS secrets (JSON strings, raw strings, or binaries) into structured 1Password fields.
      #
      # Why: AWS Secrets Manager stores data amorphously, whereas 1Password expects explicit Sections and Fields.
      # By attempting to JSON parse the payload, we natively break out composite secrets (like a config block) 
      # into individual, distinctly labelled 1Password fields, maintaining high fidelity in the Vault.
      #
      # How: We sweep over all AWS extracts. If a string is successfully JSON parsed, we iterate the Map inserting
      # each key/value pair as unique fields under the sanitized Secret Name (the section). If parsing fails (or it's raw binary), we safely fallback to storing the entire payload under a generic 'password' label.
      def call(env:, extracted_secrets:, domain_items_map:, logger:)
        logger.debug "  => Mapping secrets for environment #{env}"
        item = domain_items_map[env]
        return unless item

        extracted_secrets.each do |secret|
          section_id = sanitize_section_id(secret[:name])

          if secret[:string]
            begin
              json_payload = JSON.parse(secret[:string])
              json_payload.each do |k, v|
                item.upsert_field(section_id: section_id, label: k, value: v.to_s, type: 'CONCEALED')
              end
            rescue JSON::ParserError
              item.upsert_field(section_id: section_id, label: 'password', value: secret[:string], type: 'CONCEALED')
            end
          elsif secret[:binary]
            item.upsert_field(section_id: section_id, label: 'password', value: secret[:binary], type: 'CONCEALED')
          end

          logger.debug "  => Upserted fields for AWS secret '#{secret[:name]}' into section '#{section_id}'"
        end

        domain_items_map
      end

      private

      def sanitize_section_id(aws_name)
        parts = aws_name.split('/')
        parts.shift if parts.length > 1
        parts.join('-')
      end
    end
  end
end
