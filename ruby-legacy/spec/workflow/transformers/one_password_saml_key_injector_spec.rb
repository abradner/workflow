# frozen_string_literal: true

require 'spec_helper'
require_relative '../../../app/workflow/transformers/one_password_saml_key_injector'

require_relative '../../../app/domain/one_password/item'

RSpec.describe Workflow::Transformers::OnePasswordSamlKeyInjector do
  let(:logger) { instance_double('Logger').as_null_object }

  it 'injects the keycloak public key into valid JSON payloads if present' do
    mapper = described_class.new(kc_public_key: 'fresh_key', logger: logger)
    domain_item = Domain::OnePassword::Item.new(title: 'k8s-dev4')
    domain_item.upsert_field(section_id: 'pmn-ui-api-config', label: 'payload', value: '{"mp.jwt.verify.publickey":"stale"}', type: 'CONCEALED')

    expect(logger).to receive(:info).with(/Injected fresh/)
    
    mapper.call(domain_item)
    
    field = domain_item.fields.first
    payload = JSON.parse(field['value'])
    expect(payload['mp.jwt.verify.publickey']).to eq('fresh_key')
  end
end
