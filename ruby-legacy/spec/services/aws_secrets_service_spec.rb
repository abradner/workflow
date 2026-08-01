# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/services/aws_secrets_service'

RSpec.describe Services::AwsSecretsService do
  let(:mock_client) { instance_double(ServiceClients::Aws) }
  let(:service) { described_class.new(client: mock_client) }

  describe '#extract_environment' do
    it 'maps API payload to high-level domain objects' do
      expect(mock_client).to receive(:list_secrets).with('dev3').and_return([
                                                                              { 'Name' => 'dev3/wtf/config' },
                                                                              { 'Name' => 'dev3/wtf-ext-gw/keystore' }
                                                                            ])

      expect(mock_client).to receive(:get_secret_value).with('dev3/wtf/config').and_return({
                                                                                             'Name' => 'dev3/wtf/config',
                                                                                             'SecretString' => '{"foo":"bar"}'
                                                                                           })

      expect(mock_client).to receive(:get_secret_value).with('dev3/wtf-ext-gw/keystore').and_return({
                                                                                                      'Name' => 'dev3/wtf-ext-gw/keystore',
                                                                                                      'SecretBinary' => 'base64EncodedData'
                                                                                                    })

      results = service.extract_environment('dev3')
      expect(results.size).to eq(2)

      expect(results[0][:name]).to eq('dev3/wtf/config')
      expect(results[0][:string]).to eq('{"foo":"bar"}')
      expect(results[0][:binary]).to be_nil

      expect(results[1][:name]).to eq('dev3/wtf-ext-gw/keystore')
      expect(results[1][:string]).to be_nil
      expect(results[1][:binary]).to eq('base64EncodedData')
    end
  end

  describe '#extract_exact' do
    it 'maps API payloads for explicitly provided exact names' do
      expect(mock_client).to receive(:get_secret_value).with('standalone-secret').and_return({
                                                                                               'Name' => 'standalone-secret',
                                                                                               'SecretString' => '{"baz":"qux"}'
                                                                                             })

      expect(mock_client).to receive(:get_secret_value).with('missing-secret').and_raise(RuntimeError, 'Secret not found')

      results = service.extract_exact(['standalone-secret', 'missing-secret'])
      
      expect(results.size).to eq(1)
      expect(results[0][:name]).to eq('standalone-secret')
      expect(results[0][:string]).to eq('{"baz":"qux"}')
      expect(results[0][:binary]).to be_nil
    end
  end
end
