# frozen_string_literal: true

require 'spec_helper'
require 'fileutils'
require_relative '../app/workflow/runner'
require_relative '../app/workflow/orchestrators/sync_workloads'
require_relative '../app/workflow/orchestrators/generate_argocd'
require_relative '../app/workflow/orchestrators/sync_1password'
require_relative '../config/config'
require_relative '../app/utils/colorized_logger'
require_relative '../app/services/filesystem_service'

RSpec.describe 'End-to-End Workflows' do
  let(:config) do
    cfg = Config.new
    allow(cfg).to receive_messages(environments: %w[dev4 dev5], project_name: 'wtf')
    cfg
  end
  let(:logger) { instance_double(Utils::ColorizedLogger).as_null_object }
  let(:context) { Workflow::ExecutionContext.new(config: config, logger: logger, options: {}) }

  before do
    fs_mock = instance_double(Services::FilesystemService).as_null_object
    allow(fs_mock).to receive_messages(directory_exists?: true, list_directories: ['/src/wtf-core'],
                                       base_filename: 'wtf-core', path_entries: [])

    allow(Services::FilesystemService).to receive(:new).and_return(fs_mock)

    stub_request(:any, /aws/)
    stub_request(:any, /pmn-keycloak/)
  end

  describe 'SyncWorkloads Orchestrator' do
    it 'discovers apps and migrates overlays without crashing' do
      orchestrator = Workflow::Orchestrators::SyncWorkloads.new(config: config)
      runner = Workflow::Runner.new(context, orchestrators: [orchestrator])

      expect(runner.run).to be(true)
    end
  end

  describe 'GenerateArgocd Orchestrator' do
    it 'creates application manifests securely' do
      orchestrator = Workflow::Orchestrators::GenerateArgocd.new(config: config)
      runner = Workflow::Runner.new(context, orchestrators: [orchestrator])

      expect(runner.run).to be(true)
    end
  end

  describe 'Sync1Password Orchestrator Integration' do
    let(:aws_client_mock) { instance_double(ServiceClients::Aws) }
    let(:op_client_mock) { instance_double(ServiceClients::Op) }
    let(:orchestrator) { Workflow::Orchestrators::Sync1Password.new }
    let(:runner) { Workflow::Runner.new(context, orchestrators: [orchestrator]) }

    let(:list_secrets_response) do
      [
        { 'Name' => 'dev3/wtf/config' },
        { 'Name' => 'dev3/wtf/cert' }
      ]
    end
    let(:get_secret_value_response_1) { { 'SecretString' => '{"username":"db_user"}' } }
    let(:get_secret_value_response_2) { { 'SecretBinary' => 'b3BfMTEyMw==' } }

    let(:expected_payload_1) do
      hash_including(
        title: 'k8s-wtf-dev4',
        category: 'SECURE_NOTE',
        fields: array_including(
          hash_including(label: 'username', section: { id: 'wtf-config' }, type: 'CONCEALED', value: 'db_user'),
          hash_including(label: 'password', section: { id: 'wtf-cert' }, type: 'CONCEALED', value: 'b3BfMTEyMw==')
        ),
        sections: array_including(
          hash_including(id: 'wtf-config'),
          hash_including(id: 'wtf-cert')
        )
      )
    end
    let(:expected_payload_2) do
      hash_including(
        title: 'k8s-wtf-dev5'
      )
    end

    before do
      allow(ServiceClients::Aws).to receive(:new).and_return(aws_client_mock)
      allow(ServiceClients::Op).to receive(:new).and_return(op_client_mock)
    end

    it 'extracts secrets comprehensively (JSON and Binary) and triggers 1Password Vault ingestion' do
      # Mock the initial List API representing a Config and a Binary secret mapping to dev3
      allow(aws_client_mock).to receive(:list_secrets).with('dev3').and_return([
                                                                                 { 'Name' => 'dev3/wtf/config' },
                                                                                 { 'Name' => 'dev3/wtf/cert' }
                                                                               ])

      # Mock the payloads coming out of AWS wrapper
      allow(aws_client_mock).to receive(:get_secret_value).with('dev3/wtf/config').and_return(get_secret_value_response_1)
      allow(aws_client_mock).to receive(:get_secret_value).with('dev3/wtf/cert').and_return(get_secret_value_response_2)

      orchestrator = Workflow::Orchestrators::Sync1Password.new(config: config)
      runner = Workflow::Runner.new(context, orchestrators: [orchestrator])

      expect(op_client_mock).to receive(:create_item).with(expected_payload_1).once
      expect(op_client_mock).to receive(:create_item).with(expected_payload_2).once

      expect(runner.run).to be(true)
    end
  end
end
