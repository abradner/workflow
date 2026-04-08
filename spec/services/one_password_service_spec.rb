# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/services/one_password_service'

RSpec.describe Services::OnePasswordService do
  let(:mock_client) { instance_double(ServiceClients::Op) }
  let(:service) { described_class.new(project_name: 'wtf', client: mock_client) }

  describe '#ingest_vault_item' do
    it 'converts extracted secrets to 1Password structure with Section and Field' do
      extracted_secrets = [
        { name: 'dev3/wtf/config', string: '{"foo":"bar","baz":"qux"}' },
        { name: 'dev3/wtf-ext/keystore', binary: 'base64EncodedString' },
        { name: 'dev3/wtf-raw/secret', string: 'raw_string_password' }
      ]

      expected_payload = {
        title: 'k8s-wtf-dev4',
        category: 'SECURE_NOTE',
        sections: [
          { id: 'wtf-config', label: 'wtf-config' },
          { id: 'wtf-ext-keystore', label: 'wtf-ext-keystore' },
          { id: 'wtf-raw-secret', label: 'wtf-raw-secret' }
        ],
        fields: [
          # Parsed JSON
          { section: { id: 'wtf-config' }, label: 'foo', value: 'bar', type: 'CONCEALED' },
          { section: { id: 'wtf-config' }, label: 'baz', value: 'qux', type: 'CONCEALED' },
          # Binary data maps to password
          { section: { id: 'wtf-ext-keystore' }, label: 'password', value: 'base64EncodedString', type: 'CONCEALED' },
          # Raw non-JSON string maps to password
          { section: { id: 'wtf-raw-secret' }, label: 'password', value: 'raw_string_password', type: 'CONCEALED' }
        ]
      }

      expect(mock_client).to receive(:create_item).with(expected_payload).and_return('ok')

      service.ingest_vault_item('dev4', extracted_secrets)
    end
  end
end
