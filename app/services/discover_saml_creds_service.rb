# frozen_string_literal: true

require_relative '../service_clients/keycloak'
require_relative '../domain/saml_credentials'

module Services
  # Connects to a target Keycloak gateway to extract both critical SAML and
  # OIDC domain components needed by downstream pipeline applications.
  class DiscoverSamlCredsService
    def initialize(logger: nil)
      @logger = logger
    end

    # @param realm_name [String] Target realm (e.g. neons)
    # @param base_url [String] Full Keycloak gateway URL
    # @return [Domain::SamlCredentials] payload on success, or nil on connection failure
    def fetch_for(realm_name:, base_url:)
      client = ServiceClients::Keycloak.new(base_url: base_url, logger: @logger)

      begin
        public_key = client.fetch_realm_public_key(realm_name)
        sso_xml = client.fetch_saml_descriptor(realm_name)

        Domain::SamlCredentials.new(public_key: public_key, sso_xml: sso_xml)
      rescue StandardError => e
        @logger&.warn("Failed to fetch SAML credentials from #{base_url} (#{e.message}). Falling back gracefully.")
        nil
      end
    end
  end
end
