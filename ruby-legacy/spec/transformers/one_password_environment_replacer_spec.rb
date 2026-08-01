# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/workflow/transformers/one_password_environment_replacer'
require_relative '../../app/domain/one_password/item'

RSpec.describe Workflow::Transformers::OnePasswordEnvironmentReplacer do
  let(:logger) { instance_double('Logger').as_null_object }

  it 'maps environment names across secret strings within domain fields' do
    replacer = described_class.new(source_env: 'dev4', target_env: 'dev5', logger: logger)
    domain_item = Domain::OnePassword::Item.new(title: 'k8s-dev4')
    domain_item.upsert_field(section_id: 'pmn-dev4-config', label: 'conn', value: 'db.dev4.com', type: 'CONCEALED')

    replacer.call(domain_item)
    
    field = domain_item.fields.first
    expect(field['value']).to eq('db.dev5.com')
    expect(field.dig('section', 'id')).to eq('pmn-dev5-config')

    section = domain_item.sections.first
    expect(section['id']).to eq('pmn-dev5-config')
    expect(section['label']).to eq('pmn-dev5-config')
  end

  it 'ignores fields that do not contain the target mapping safely' do
    replacer = described_class.new(source_env: 'dev4', target_env: 'dev5', logger: logger)
    domain_item = Domain::OnePassword::Item.new(title: 'k8s-dev4')
    domain_item.upsert_field(section_id: 'pmn-config', label: 'conn', value: 'db.global.com', type: 'CONCEALED')

    replacer.call(domain_item)
    
    field = domain_item.fields.first
    expect(field['value']).to eq('db.global.com')
  end

  it 'deduplicates identically named fields within the same section after translation' do
    replacer = described_class.new(source_env: 'dev4', target_env: 'dev5', logger: logger)
    
    existing_json = {
      'id' => '12345',
      'title' => 'existing-title',
      'sections' => [{ 'id' => 'pmn-dev5-config', 'label' => 'pmn-dev5-config' }],
      'fields' => [
        { 'id' => 'f-1', 'section' => { 'id' => 'pmn-dev5-config' }, 'label' => 'conn', 'value' => 'db.dev5.com', 'type' => 'CONCEALED' }
      ]
    }
    
    domain_item = Domain::OnePassword::Item.new(title: 'k8s-dev5', existing_item_json: existing_json)
    
    # Simulates mapper inserting a new field for 'dev4'
    domain_item.upsert_field(section_id: 'pmn-dev4-config', label: 'conn', value: 'db.NEW-dev4.com', type: 'CONCEALED')

    # This translation will turn 'pmn-dev4-config' -> 'pmn-dev5-config', 
    # causing TWO 'conn' labels in 'pmn-dev5-config'.
    replacer.call(domain_item)

    # It should consolidate down to 1 field
    expect(domain_item.fields.size).to eq(1)

    field = domain_item.fields.first
    # The deduplication preserves the original ID and takes the newest value after translation
    expect(field['id']).to eq('f-1')
    expect(field['value']).to eq('db.NEW-dev5.com')
  end

  it 'preserves vault-native field ID regardless of array ordering (order-independent)' do
    replacer = described_class.new(source_env: 'dev4', target_env: 'dev5', logger: logger)
    
    existing_json = {
      'id' => '12345',
      'title' => 'existing-title',
      'sections' => [{ 'id' => 'pmn-dev5-config', 'label' => 'pmn-dev5-config' }],
      'fields' => [
        { 'id' => 'f-vault', 'section' => { 'id' => 'pmn-dev5-config' }, 'label' => 'conn', 'value' => 'db.dev5.com', 'type' => 'CONCEALED' }
      ]
    }
    
    domain_item = Domain::OnePassword::Item.new(title: 'k8s-dev5', existing_item_json: existing_json)
    
    # Deliberately insert the mapper-generated field at the FRONT of the array
    # so the vault-native field is at the back — opposite of normal pipeline ordering
    mapper_field = { 'id' => SecureRandom.hex(16), 'section' => { 'id' => 'pmn-dev4-config' }, 'label' => 'conn', 'value' => 'db.NEW-dev4.com', 'type' => 'CONCEALED' }
    domain_item.fields.unshift(mapper_field)
    
    replacer.call(domain_item)

    expect(domain_item.fields.size).to eq(1)

    field = domain_item.fields.first
    # Even though the mapper-generated field was first, the vault-native ID is preserved
    expect(field['id']).to eq('f-vault')
    expect(field['value']).to eq('db.NEW-dev5.com')
  end
end
