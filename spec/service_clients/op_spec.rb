# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/service_clients/op'

RSpec.describe ServiceClients::Op do
  let(:client) { described_class.new }

  describe '#create_item' do
    it 'pipes JSON configuration to op item create stdin' do
      template = { title: 'my-item', category: 'SECURE_NOTE' }
      mock_status = instance_double(Process::Status, success?: true)

      expect(Open3).to receive(:capture3)
        .with('op item create -', stdin_data: template.to_json)
        .and_return(['id=123', '', mock_status])

      expect(client.create_item(template)).to eq('id=123')
    end
  end
end
