# frozen_string_literal: true

require 'spec_helper'
require_relative '../../../app/workflow/hydrate/one_password_items'
require_relative '../../../app/workflow/execution_context'
require_relative '../../../config/config'
require_relative '../../../app/utils/colorized_logger'

RSpec.describe Workflow::Hydrate::OnePasswordItems do
  let(:config_mock) do
    cfg = instance_double(Config)
    allow(cfg).to receive_messages(environments: %w[dev4 dev5], source_env: 'dev4', project_name: 'wtf', op_vault_name: 'Tooling')
    cfg
  end

  let(:logger_mock) { instance_double(Utils::ColorizedLogger).as_null_object }
  let(:context) { Workflow::ExecutionContext.new(config: config_mock, logger: logger_mock) }
  let(:op_client) { instance_double(ServiceClients::Op) }

  before do
    allow(ServiceClients::Op).to receive(:new).and_return(op_client)
  end

  it 'returns early if already hydrated' do
    context.one_password_items['dev4'] = 'already_loaded'
    expect(ServiceClients::Op).not_to receive(:new)
    described_class.call(context)
  end

  it 'iterates unique environments generating Domain item blueprints cleanly' do
    # Only two runs because dev4 overlaps source and target
    expect(op_client).to receive(:get_item).with('k8s-wtf-dev4', vault: 'Tooling').and_return({ 'id' => '123' })
    expect(op_client).to receive(:get_item).with('k8s-wtf-dev5', vault: 'Tooling').and_return(nil)

    described_class.call(context)

    expect(context.one_password_items.keys).to match_array(%w[dev4 dev5])
    
    # dev4 hydrated from mock
    expect(context.one_password_items['dev4']).to be_a(Domain::OnePassword::Item)
    expect(context.one_password_items['dev4'].id).to eq('123')
    
    # dev5 blank entity
    expect(context.one_password_items['dev5']).to be_a(Domain::OnePassword::Item)
    expect(context.one_password_items['dev5'].id).to be_nil
  end
end
