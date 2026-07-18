# frozen_string_literal: true

require 'json'
require 'open3'

module ServiceClients
  # Wrapper around AWS CLI commands
  class Aws
    def list_secrets(env)
      stdout, stderr, status = Open3.capture3("aws secretsmanager list-secrets --filter Key=\"name\",Values=\"#{env}\",\"dev/#{env}\"")
      raise "Failed to list AWS Secrets: #{stderr}" unless status.success?

      JSON.parse(stdout)['SecretList']
    end

    def get_secret_value(secret_id)
      stdout, stderr, status = Open3.capture3("aws secretsmanager get-secret-value --secret-id \"#{secret_id}\"")
      raise "Failed to fetch AWS Secret #{secret_id}: #{stderr}" unless status.success?

      JSON.parse(stdout)
    end
  end
end
