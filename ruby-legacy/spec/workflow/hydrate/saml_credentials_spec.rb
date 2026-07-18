# frozen_string_literal: true

require 'spec_helper'
require_relative '../../../app/workflow/hydrate/saml_credentials'
require_relative '../../../app/workflow/execution_context'

RSpec.describe Workflow::Hydrate::SamlCredentials do
  let(:config) { double('Config', environments: ['dev4', 'dev5'], project_name: 'pmn', tld: 'f-ck.xyz') }
  let(:logger) { instance_double('Logger').as_null_object }
  let(:context) { Workflow::ExecutionContext.new(config: config, logger: logger) }
  let(:service) { instance_double('Services::DiscoverSamlCredsService') }
  let(:creds_mock) { double('SamlCredentials') }

  before do
    allow(Services::DiscoverSamlCredsService).to receive(:new).and_return(service)
  end

  it 'iterates over target environments and fetches credentials mapping them to the context' do
    expect(service).to receive(:fetch_for).with(realm_name: 'neons', base_url: 'https://pmn-keycloak.pmn.dev4.f-ck.xyz').and_return(creds_mock)
    expect(service).to receive(:fetch_for).with(realm_name: 'neons', base_url: 'https://pmn-keycloak.pmn.dev5.f-ck.xyz').and_return(nil)

    described_class.call(context)

    expect(context.saml_credentials_by_env['dev4']).to eq(creds_mock)
    expect(context.saml_credentials_by_env['dev5']).to be_nil
  end

  it 'skips execution if already extracted' do
    context.saml_credentials_by_env['dev4'] = creds_mock

    expect(service).not_to receive(:fetch_for)
    described_class.call(context)
  end
end
