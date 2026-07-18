# frozen_string_literal: true

require_relative '../service_clients/aws'

module Services
  # High level business logic for extracting AWS secrets
  class AwsSecretsService
    def initialize(client: ServiceClients::Aws.new)
      @client = client
    end

    def extract_secrets(env)
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
  end
end
