# frozen_string_literal: true

require_relative '../../services/aws_secrets_service'

module Workflow
  module Hydrate
    # Queries the AWS Services matching environment prefixes and explicit secret references
    class AwsSecrets
      def self.call(context)
        return if context.aws_secrets_extracted?

        service = Services::AwsSecretsService.new(logger: context.logger)

        # 1. Fetch all implicitly queried secrets by Environment Prefix
        env_secrets = service.extract_environment(context.config.source_env)

        # 2. Add in any strictly listed secrets mapping (bypassing search/list limits)
        strict_identifiers = context.config.additional_aws_secrets

        strict_secrets = service.extract_exact(strict_identifiers)

        # 3. Deduplicate (exact extracts take precedence if collided)
        map = {}
        env_secrets.each { |s| map[s[:name]] = s }
        strict_secrets.each { |s| map[s[:name]] = s }

        context.extracted_aws_secrets = map.values
      end
    end
  end
end
