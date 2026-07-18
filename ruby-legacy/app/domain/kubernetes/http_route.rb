# frozen_string_literal: true

module Kubernetes
  HTTPRoute = Data.define(:name, :namespace, :hostnames, :backend_refs) do
    def to_h
      {
        apiVersion: 'gateway.networking.k8s.io/v1',
        kind: 'HTTPRoute',
        metadata: { name: name, namespace: namespace },
        spec: {
          parentRefs: [{ name: 'homelab-gateway', namespace: 'default' }],
          hostnames: hostnames.empty? ? ['placeholder.local'] : hostnames,
          rules: [{ backendRefs: backend_refs }]
        }
      }
    end

    def self.from_ingress(doc)
      name = doc.dig(:metadata, :name)
      namespace = doc.dig(:metadata, :namespace) || 'default'

      rules = doc.dig(:spec, :rules) || []
      hostnames = rules.filter_map { |r| r[:host] }

      backend_refs = rules.flat_map do |rule|
        rule.dig(:http, :paths)&.filter_map do |path|
          svc = path.dig(:backend, :service)
          next unless svc

          { name: svc[:name], port: svc.dig(:port, :number) }
        end || []
      end

      new(name: name, namespace: namespace, hostnames: hostnames, backend_refs: backend_refs).to_h
    end
  end
end
