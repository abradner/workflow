# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/workflow/transformers/one_password_item_mapper'
require_relative '../../app/domain/one_password/item'

RSpec.describe Workflow::Transformers::OnePasswordItemMapper do
  let(:transformer) { described_class.new }
  let(:domain_item) { Domain::OnePassword::Item.new(title: 'k8s-wtf-dev4') }
  let(:domain_items_map) { { 'dev4' => domain_item } }

  let(:logger) { instance_double('Logger').as_null_object }

  it 'returns gracefully if the environment item is missing' do
    expect { transformer.call(env: 'dev99', extracted_secrets: [], domain_items_map: domain_items_map, logger: logger) }
      .not_to raise_error
  end

  it 'iterates AWS secrets and routes payloads dynamically into the Domain Item upsert' do
    secrets = [
      { name: 'dev3/wtf/config', string: '{"foo":"bar"}' },
      { name: 'dev3/wtf-ext/keystore', binary: 'base64Encoded' },
      { name: 'dev3/wtf-raw', string: 'raw_string' }
    ]

    expect(domain_item).to receive(:upsert_field).with(section_id: 'wtf-config', label: 'foo', value: 'bar', type: 'CONCEALED').once
    expect(domain_item).to receive(:upsert_field).with(section_id: 'wtf-ext-keystore', label: 'password', value: 'base64Encoded', type: 'CONCEALED').once
    expect(domain_item).to receive(:upsert_field).with(section_id: 'wtf-raw', label: 'password', value: 'raw_string', type: 'CONCEALED').once

    transformer.call(env: 'dev4', extracted_secrets: secrets, domain_items_map: domain_items_map, logger: logger)
  end
end
