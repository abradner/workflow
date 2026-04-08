# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/service_clients/aws'

RSpec.describe ServiceClients::Aws do
  let(:client) { described_class.new }

  describe '#list_secrets' do
    it 'shells out to aws secretsmanager and returns parsed JSON array' do
      mock_stdout = { 'SecretList' => [{ 'Name' => 'dev3/secret' }] }.to_json
      mock_status = instance_double(Process::Status, success?: true)

      expect(Open3).to receive(:capture3)
        .with('aws secretsmanager list-secrets --filter Key="name",Values="dev3"')
        .and_return([mock_stdout, '', mock_status])

      expect(client.list_secrets('dev3')).to eq([{ 'Name' => 'dev3/secret' }])
    end

    it 'raises an error when aws cli fails' do
      mock_status = instance_double(Process::Status, success?: false)

      expect(Open3).to receive(:capture3).and_return(['', 'command not found', mock_status])

      expect do
        client.list_secrets('dev3')
      end.to raise_error(RuntimeError, /Failed to list AWS Secrets: command not found/)
    end
  end

  describe '#get_secret_value' do
    it 'shells out to aws and JSON parses the object' do
      mock_stdout = { 'SecretString' => 'password123' }.to_json
      mock_status = instance_double(Process::Status, success?: true)

      expect(Open3).to receive(:capture3)
        .with('aws secretsmanager get-secret-value --secret-id "dev3/foo"')
        .and_return([mock_stdout, '', mock_status])

      expect(client.get_secret_value('dev3/foo')).to eq({ 'SecretString' => 'password123' })
    end
  end
end
