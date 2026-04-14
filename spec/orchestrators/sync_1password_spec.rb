# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/workflow/orchestrators/sync_1password'
require_relative '../../app/workflow/execution_context'
require_relative '../../app/domain/saml_credentials'

RSpec.describe Workflow::Orchestrators::Sync1Password do
  let(:config) do
    cfg = double('Config', environments: ['dev4'], project_name: 'pmn', source_env: 'dev4')
    allow(cfg).to receive(:is_a?).and_return(true)
    cfg
  end
  let(:logger) { instance_double('Logger').as_null_object }
  let(:context) { Workflow::ExecutionContext.new(config: config, logger: logger) }
  let(:aws_service) { instance_double('Services::AwsSecretsService') }
  let(:op_service) { instance_double('Services::OnePasswordService') }

  subject(:orchestrator) { described_class.new(config: config) }

  before do
    allow(Services::AwsSecretsService).to receive(:new).and_return(aws_service)
    allow(Services::OnePasswordService).to receive(:new).and_return(op_service)
  end

  it 'declares needs saml_credentials_extracted' do
    expect(orchestrator.needs).to include(:saml_credentials_extracted)
  end

  describe '#act_phase' do
    it 'extracts secrets from AWS and maps them to vault payloads' do
      expect(aws_service).to receive(:extract_secrets).with('dev4').and_return([
        {
          name: 'dev4/pmn-ui-api-config',
          string: '{"mp.jwt.verify.publickey": "old_key"}',
          binary: nil
        }
      ])
      
      creds = Domain::SamlCredentials.new(public_key: 'fresh_key', sso_xml: '<xml/>')
      context.saml_credentials_by_env['dev4'] = creds

      orchestrator.act_phase(context)
      
      mapped = orchestrator.instance_variable_get(:@mapped_vault_items)['dev4']
      expect(mapped.first[:string]).to eq({ "mp.jwt.verify.publickey" => creds.pem_public_key }.to_json)
    end
  end

  describe '#commit_phase' do
    it 'ingests the buffered vault mappings' do
      orchestrator.instance_variable_set(:@mapped_vault_items, {
        'dev4' => [{ name: 'test', string: 'test', binary: nil }]
      })

      expect(op_service).to receive(:ingest_vault_item).with('dev4', [{ name: 'test', string: 'test', binary: nil }])

      orchestrator.commit_phase(context)
    end
  end
end
