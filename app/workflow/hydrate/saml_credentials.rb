# frozen_string_literal: true

require_relative '../../services/discover_saml_creds_service'

module Workflow
  module Hydrate
    # Populates SAML descriptors and Keycloak Public keys per environment.
    class SamlCredentials
      def self.call(context)
        return if context.saml_credentials_extracted?

        service = Services::DiscoverSamlCredsService.new(logger: context.logger)

        context.config.environments.each do |env|
          # Derive exact gateway for the target
          target_url = "https://pmn-keycloak.#{context.config.project_name}.#{env}.#{context.config.tld}"
          
          # Fetch for the specific multi-tenant realm
          creds = service.fetch_for(realm_name: 'neons', base_url: target_url)
          
          # Inject populated domain model into the buffered context
          context.saml_credentials_by_env[env] = creds if creds
        end
      end
    end
  end
end
