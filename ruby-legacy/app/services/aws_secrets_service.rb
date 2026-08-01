# frozen_string_literal: true

require_relative '../service_clients/aws'

module Services
  # High level business logic for  # Extracts payloads from AWS Secrets Manager
  class AwsSecretsService
    def initialize(client: ServiceClients::Aws.new, logger: nil)
      @client = client
      @logger = logger
    end

    def extract_environment(env)
      secrets = @client.list_secrets(env)
      secrets.map do |secret_meta|
        name = secret_meta['Name']
        payload = @client.get_secret_value(name)

        {
          name: name,
          string: payload['SecretString'],
          binary: payload['SecretBinary']
        }
      end
    end

    # What: Fetches explicitly named AWS secrets directly, bypassing environment/search filters.
    #
    # Why: Certain global infrastructure secrets (like ElastiCache credentials) don't conform to the
    # standard `prefix/` environment naming convention. They must be explicitly resolved.
    #
    # How: Maps over the list of names and calls `get_secret_value` directly. If an exception occurs 
    # (e.g., the secret is entirely missing during a fresh environment provision or local mock), 
    # the method traps the error, logs a warning, and gracefully returns nil. 
    # This specifically prevents missing peripheral secrets from fatally crashing the entire pipeline.
    def extract_exact(secret_names)
      secret_names.map do |name|
        begin
          payload = @client.get_secret_value(name)
          {
            name: name,
            string: payload['SecretString'],
            binary: payload['SecretBinary']
          }
        rescue RuntimeError => e
          # Silently drop failed specific pulls if they aren't stored in AWS (mocking/missing etc)
          @logger&.warn "Failed to exact-fetch #{name}: #{e.message}"
          nil
        end
      end.compact
    end
  end
end
