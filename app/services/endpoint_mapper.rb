# frozen_string_literal: true

module Services
  class EndpointMapper
    SUFFIX_MAPPINGS = {
      '.rds.amazonaws.com' => 'pg',
      '.confluent.cloud' => 'kafka'
    }.freeze

    # Match a hostname against suffix-based endpoint mappings.
    # Returns the mapped resource name (e.g. 'pg', 'kafka') or nil if no match.
    def self.match_endpoint(hostname)
      SUFFIX_MAPPINGS.each do |suffix, resource|
        return resource if hostname.end_with?(suffix)
      end
      nil
    end
  end
end
