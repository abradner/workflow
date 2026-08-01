# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/workflow/orchestrators/sync_1password'
require_relative '../../app/workflow/execution_context'
require_relative '../../app/domain/one_password/item'
require_relative '../../app/utils/colorized_logger'

RSpec.describe Workflow::Orchestrators::Sync1Password do
  let(:config) do
    cfg = double('Config', environments: ['dev4'], project_name: 'pmn', source_env: 'dev4', op_vault_name: 'Tooling')
    allow(cfg).to receive(:is_a?).and_return(true)
    cfg
  end
  let(:logger_mock) { instance_double(Utils::ColorizedLogger).as_null_object }
  let(:context) { Workflow::ExecutionContext.new(config: config, logger: logger_mock) }

  let(:orchestrator) { described_class.new(config: config) }

  before do
    # Fake SAML extraction dependency mapping requirement
    context.saml_credentials_by_env['dev4'] = nil
    
    # Fake 1Password Hydra phase requirement
    domain_item = Domain::OnePassword::Item.new(title: 'k8s-pmn-dev4')
    context.one_password_items['dev4'] = domain_item
  end

  it 'declares needs saml_credentials_extracted, aws_secrets_extracted and one_password_items_hydrated' do
    expect(orchestrator.needs).to include(:saml_credentials_extracted)
    expect(orchestrator.needs).to include(:one_password_items_hydrated)
    expect(orchestrator.needs).to include(:aws_secrets_extracted)
  end

  describe '#act_phase' do
    it 'orchestrates aws secret extraction through the transformer maps' do
      orchestrator = described_class.new(config: config)

      expected_secrets = [{ name: 'dev4/pmn/config', string: '{"foo":"bar"}' }]
      context.extracted_aws_secrets = expected_secrets

      orchestrator.act_phase(context)

      # Validates side-effect-free Domain mapping mutations via the Transformer
      domain_item = context.one_password_items['dev4']
      expect(domain_item.fields.first).to include('label' => 'foo', 'value' => 'bar')
    end
  end

  describe '#commit_phase' do
    let(:op_commit_service_mock) { instance_double(Services::OnePasswordCommitService) }

    before do
      allow(Services::OnePasswordCommitService).to receive(:new).and_return(op_commit_service_mock)
    end

    it 'ingests the buffered vault mappings via the domain model' do
      orchestrator = described_class.new(config: config)
      
      domain_item = context.one_password_items['dev4']
      expect(op_commit_service_mock).to receive(:commit).with(domain_item).once

      orchestrator.commit_phase(context)
    end
  end
end
