# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/services/keycloak_setup_service'

RSpec.describe Services::KeycloakSetupService do
  let(:logger) { instance_double('Logger').as_null_object }
  let(:keycloak_client) { instance_double('ServiceClients::Keycloak', base_url: 'http://keycloak.example') }
  let(:service) { described_class.new(base_url: 'http://keycloak.example', logger: logger) }

  before do
    allow(ServiceClients::Keycloak).to receive(:new).and_return(keycloak_client)
  end

  describe '#setup' do
    it 'orchestrates realm initialization and client/user mapping correctly' do
      # 1. Ready phase
      expect(keycloak_client).to receive(:ready?).and_return(true)

      # 2. Authenticate
      expect(keycloak_client).to receive(:authenticate).with('admin', 'pass')

      # 3. Realm Creation
      expect(keycloak_client).to receive(:create_realm).with('neons')

      # 4. Client Imports
      expect(keycloak_client).to receive(:import_client).with('neons', hash_including(protocol: 'openid-connect')).once
      expect(keycloak_client).to receive(:import_client).with('neons', hash_including(protocol: 'saml')).once

      # 5. Group setup
      expect(keycloak_client).to receive(:create_group).exactly(3).times

      # 6. User Setup
      expect(keycloak_client).to receive(:create_user).exactly(3).times.and_return('fake_user_id')
      expect(keycloak_client).to receive(:get_groups).exactly(3).times.and_return([{ 'id' => 'fake_group_id' }])
      expect(keycloak_client).to receive(:add_user_to_group).exactly(3).times

      # 7. SAML Descriptor Export
      expect(keycloak_client).to receive(:fetch_saml_descriptor).with('neons').and_return('<xml/>')

      result = service.setup(admin_username: 'admin', admin_password: 'pass')

      expect(result[:xml]).to eq('<xml/>')
      expect(result[:b64]).to eq('PHhtbC8+') # base64 strict encoded
    end

    it 'aborts execution if the system refuses to become ready' do
      allow(keycloak_client).to receive(:ready?).and_return(false)
      expect(service).to receive(:sleep).exactly(11).times

      expect {
        service.setup(admin_username: 'admin', admin_password: 'pass')
      }.to raise_error(StandardError, /did not become ready in time/)
    end
  end
end
