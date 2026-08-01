# frozen_string_literal: true

require 'spec_helper'
require_relative '../../../app/domain/one_password/item'

RSpec.describe Domain::OnePassword::Item do
  let(:existing_json) do
    {
      'id' => '12345',
      'title' => 'existing-title',
      'sections' => [
        { 'id' => 'sec-1', 'label' => 'sec-1' }
      ],
      'fields' => [
        { 'id' => 'f-1', 'section' => { 'id' => 'sec-1' }, 'label' => 'password', 'value' => 'old_pass', 'type' => 'CONCEALED' },
        { 'id' => 'f-old', 'section' => { 'id' => 'sec-1' }, 'label' => 'stale_token', 'value' => 'dead_beef', 'type' => 'STRING' }
      ]
    }
  end

  describe '#initialize' do
    it 'creates a blank entity perfectly' do
      item = described_class.new(title: 'blank-item')

      expect(item.id).to be_nil
      expect(item.reference_id).to eq('blank-item')
      expect(item.sections).to be_empty
      expect(item.fields).to be_empty
      expect(item.as_json[:category]).to eq('SECURE_NOTE')
    end

    it 'hydrates from existing JSON perfectly' do
      item = described_class.new(title: 'hydrated', existing_item_json: existing_json)

      expect(item.id).to eq('12345')
      expect(item.reference_id).to eq('12345')
      expect(item.sections.size).to eq(1)
      expect(item.fields.size).to eq(2)
    end
  end

  describe '#upsert_field' do
    context 'when field exists' do
      it 'mutates value and tracks interaction natively' do
        item = described_class.new(title: 'hydrated', existing_item_json: existing_json)
        item.upsert_field(section_id: 'sec-1', label: 'password', value: 'new_pass')

        field = item.fields.find { |f| f['id'] == 'f-1' }
        expect(field['value']).to eq('new_pass')
        
        expect(item.touched_field_ids).to include('f-1')
        expect(item.stale_field_ids).to include('f-old')
        expect(item.stale_field_ids).not_to include('f-1')
      end
    end

    context 'when field does not exist' do
      it 'generates a new field and tracks its parent section natively' do
        item = described_class.new(title: 'blank-item')
        item.upsert_field(section_id: 'sec-new', label: 'api_key', value: '123')

        expect(item.sections.first['id']).to eq('sec-new')
        expect(item.fields.first).to include(
          'section' => { 'id' => 'sec-new' },
          'label' => 'api_key',
          'value' => '123',
          'type' => 'CONCEALED'
        )
      end
    end
  end
end
