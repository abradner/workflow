# frozen_string_literal: true

require 'json'

module Workflow
  module Transformers
    # Maps extracted AWS secrets to target environments and optionally injects live Keys.
    class OnePasswordSamlKeyInjector
      def initialize(kc_public_key: nil, logger: nil)
        @kc_public_key = kc_public_key
        @logger = logger
      end

      # What: Dynamically injects a fresh Keycloak SAML PEM public key into 1Password vault configurations.
      #
      # Why: The standard environment-string-replacer is dumb — it only maps domains. For application SSO 
      # integrations, the upstream `pmn-ui-api` requires the live Keycloak Public Key to verify JWTs. 
      # Since this key is cryptographically generated on-the-fly when the new environment is bootstrapped, 
      # we must sniff it and physically inject it into the Vault payload.
      #
      # How: Iterates all CONCEALED fields looking for payloads that appear to be JSON. 
      # If the string starts with `{`, it parses it, finds the specific `mp.jwt.verify.publickey` target, 
      # forcibly overrides the old value with the live Keycloak PEM, and safely re-serializes the JSON block.
      def call(domain_item)
        return unless domain_item

        domain_item.fields.each do |field|
          mapped_string = field['value']

          # skip definitely-non-json strings
          next unless field['type'] == 'CONCEALED' && mapped_string.is_a?(String) && mapped_string.strip.start_with?('{')

          if @kc_public_key
            begin
              payload = JSON.parse(mapped_string)
              if payload.is_a?(Hash) && payload.key?('mp.jwt.verify.publickey')
                payload['mp.jwt.verify.publickey'] = @kc_public_key
                mapped_string = payload.to_json
                section_id = field.dig('section', 'id')
                @logger&.info "Injected fresh Keycloak public key into #{section_id}/#{field['label']}"
              end
            rescue JSON::ParserError
              # safely skip string values that aren't JSON
            end
          end

          domain_item.upsert_field(
            section_id: field.dig('section', 'id'),
            label: field['label'],
            value: mapped_string,
            type: field['type']
          )
        end
      end
    end
  end
end
