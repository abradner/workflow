# frozen_string_literal: true

require 'spec_helper'
require 'webmock/rspec'
require_relative '../../app/service_clients/keycloak'

RSpec.describe ServiceClients::Keycloak do
  let(:base_url) { 'https://keycloak.example.com' }
  let(:logger) { instance_double('Logger').as_null_object }
  let(:client) { described_class.new(base_url: base_url, logger: logger) }

  describe '#ready?' do
    it 'returns true when openid-configuration is accessible' do
      stub_request(:get, "#{base_url}/realms/master/.well-known/openid-configuration")
        .to_return(status: 200, body: '{}')

      expect(client.ready?).to be true
    end

    it 'returns false on network timeout' do
      stub_request(:get, "#{base_url}/realms/master/.well-known/openid-configuration")
        .to_timeout

      expect(client.ready?).to be false
    end
  end

  describe '#fetch_realm_public_key' do
    it 'returns the public key field from the realm descriptor' do
      stub_request(:get, "#{base_url}/realms/neons")
        .to_return(status: 200, body: { public_key: 'abcxyz' }.to_json)

      expect(client.fetch_realm_public_key('neons')).to eq('abcxyz')
    end
  end

  describe '#fetch_saml_descriptor' do
    it 'returns the raw xml body' do
      stub_request(:get, "#{base_url}/realms/neons/protocol/saml/descriptor")
        .to_return(status: 200, body: '<xml></xml>')

      expect(client.fetch_saml_descriptor('neons')).to eq('<xml></xml>')
    end
  end
end
