# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/services/discover_saml_creds_service'

RSpec.describe Services::DiscoverSamlCredsService do
  let(:logger) { instance_double('Logger').as_null_object }
  let(:service) { described_class.new(logger: logger) }
  let(:keycloak_client) { instance_double('ServiceClients::Keycloak') }

  before do
    allow(ServiceClients::Keycloak).to receive(:new).and_return(keycloak_client)
  end

  describe '#fetch_for' do
    it 'returns a populated SamlCredentials object on success' do
      allow(keycloak_client).to receive(:fetch_realm_public_key).with('neons').and_return('PUBKEY')
      allow(keycloak_client).to receive(:fetch_saml_descriptor).with('neons').and_return('<xml/>')

      result = service.fetch_for(realm_name: 'neons', base_url: 'http://test')
      
      expect(result).to be_a(Domain::SamlCredentials)
      expect(result.public_key).to eq('PUBKEY')
      expect(result.sso_xml).to eq('<xml/>')
    end

    it 'gracefully traps network errors and returns nil to prevent blocking workflow runs' do
      allow(keycloak_client).to receive(:fetch_realm_public_key).and_raise(StandardError.new('Conn refused'))

      expect(logger).to receive(:warn).with(/Falling back gracefully/)
      
      result = service.fetch_for(realm_name: 'neons', base_url: 'http://test')
      expect(result).to be_nil
    end
  end
end
