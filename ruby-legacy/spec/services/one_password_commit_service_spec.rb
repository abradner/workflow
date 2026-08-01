# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/services/one_password_commit_service'
require_relative '../../app/domain/one_password/item'

RSpec.describe Services::OnePasswordCommitService do
  let(:op_client) { instance_double(ServiceClients::Op) }
  let(:logger) { instance_double(Utils::ColorizedLogger).as_null_object }
  let(:service) { described_class.new(client: op_client, logger: logger) }

  describe '#commit' do
    it 'dispatches an edit_item request if domain object has an ID' do
      existing_json = { 'id' => 'item-4', 'fields' => [] }
      domain = Domain::OnePassword::Item.new(title: 'my-item', existing_item_json: existing_json, vault_name: 'Tooling')

      expect(op_client).to receive(:edit_item).with('item-4', domain.as_json, vault: 'Tooling').once
      service.commit(domain)
    end

    it 'dispatches a create_item request if domain object lacks an ID' do
      domain = Domain::OnePassword::Item.new(title: 'my-item', vault_name: 'Tooling')

      expect(op_client).to receive(:create_item).with(domain.as_json, vault: 'Tooling').once
      service.commit(domain)
    end
  end
end
