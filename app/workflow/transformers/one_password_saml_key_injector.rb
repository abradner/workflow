# frozen_string_literal: true

require 'json'

module Workflow
  module Transformers
    # Maps extracted AWS secrets to target environments and optionally injects live Keys.
    class OnePasswordSamlKeyInjector
      def initialize(source_env:, target_env:, kc_public_key: nil, logger: nil)
        @source_env = source_env
        @target_env = target_env
        @kc_public_key = kc_public_key
        @logger = logger
      end

      def call(extracted_secrets)
        extracted_secrets.map do |secret|
          mapped_string = secret[:string]&.gsub(@source_env, @target_env)

          if @kc_public_key && mapped_string
            begin
              payload = JSON.parse(mapped_string)
              if payload.is_a?(Hash) && payload.key?('mp.jwt.verify.publickey')
                payload['mp.jwt.verify.publickey'] = @kc_public_key
                mapped_string = payload.to_json
                @logger&.info "Injected fresh Keycloak public key into #{secret[:name]}"
              end
            rescue JSON::ParserError
              # safely skip string values that aren't JSON
            end
          end

          {
            name: secret[:name].gsub(@source_env, @target_env),
            string: mapped_string,
            binary: secret[:binary]
          }
        end
      end
    end
  end
end
