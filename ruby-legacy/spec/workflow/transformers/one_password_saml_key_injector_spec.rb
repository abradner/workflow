# frozen_string_literal: true

require 'spec_helper'
require_relative '../../../app/workflow/transformers/one_password_saml_key_injector'

RSpec.describe Workflow::Transformers::OnePasswordSamlKeyInjector do
  let(:logger) { instance_double('Logger').as_null_object }

  it 'maps environment names across secret strings and properties' do
    mapper = described_class.new(source_env: 'dev4', target_env: 'dev5')
    extracted = [
      { name: 'dev4/pmn-config', string: 'conn=db.dev4.com', binary: nil }
    ]

    result = mapper.call(extracted)
    
    expect(result.first[:name]).to eq('dev5/pmn-config')
    expect(result.first[:string]).to eq('conn=db.dev5.com')
  end

  it 'injects the keycloak public key into valid JSON payloads if present' do
    mapper = described_class.new(source_env: 'dev4', target_env: 'dev5', kc_public_key: 'fresh_key', logger: logger)
    extracted = [
      { name: 'dev4/pmn-ui-api-config', string: '{"mp.jwt.verify.publickey":"stale"}', binary: nil }
    ]

    expect(logger).to receive(:info).with(/Injected fresh/)
    
    result = mapper.call(extracted)
    
    payload = JSON.parse(result.first[:string])
    expect(payload['mp.jwt.verify.publickey']).to eq('fresh_key')
  end
end
