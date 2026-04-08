# frozen_string_literal: true

require 'json'

module Kubernetes
  # Structured representation of a Kubernetes ExternalSecret manifest
  ExternalSecret = Data.define(:name, :api_version, :store_name, :template_type, :template_data, :data_refs) do
    def to_h
      {
        apiVersion: api_version,
        kind: 'ExternalSecret',
        metadata: { name: name },
        spec: {
          refreshInterval: '1h',
          secretStoreRef: {
            name: store_name,
            kind: 'ClusterSecretStore'
          },
          target: {
            name: name,
            creationPolicy: 'Owner',
            template: {
              type: template_type,
              data: template_data
            }
          },
          data: data_refs.map do |ref|
            {
              secretKey: ref[:secret_key],
              remoteRef: { key: ref[:key], property: ref[:property] }
            }
          end
        }
      }
    end
  end
end
