# frozen_string_literal: true

require 'securerandom'

module Domain
  module OnePassword
    # Represents a 1Password Item (typically Secure Note or Login).
    # Maintains internal ID mappings for safe, idempotent updates against the 1P CLI.
    class Item
      attr_reader :id, :title, :category, :touched_field_ids, :vault_name
      attr_accessor :sections, :fields

      def initialize(title:, category: 'SECURE_NOTE', vault_name: nil, existing_item_json: nil)
        @title = title
        @category = category
        @vault_name = vault_name
        @touched_field_ids = []

        if existing_item_json
          @id = existing_item_json['id']
          @sections = existing_item_json['sections'] || []
          @fields = existing_item_json['fields'] || []
        else
          @id = nil
          @sections = []
          @fields = []
        end
      end

      # Exposes the safest known identifier for targeting CLI operations
      def reference_id
        @id || @title
      end

      # Adds or updates a field safely preserving its upstream ID.
      def upsert_field(section_id:, label:, value:, type: 'CONCEALED')
        ensure_section!(section_id)

        field = @fields.find { |f| f.dig('section', 'id') == section_id && f['label'] == label }

        if field
          field['value'] = value
          field['type'] = type
          @touched_field_ids << field['id'] if field['id']
        else
          new_field = {
            'id' => SecureRandom.hex(16),
            'section' => { 'id' => section_id },
            'label' => label,
            'value' => value,
            'type' => type
          }
          @fields << new_field
          @touched_field_ids << new_field['id']
        end
      end

      def stale_field_ids
        @fields.filter_map { |f| f['id'] } - @touched_field_ids
      end



      def to_h
        {
          title: @title,
          category: @category,
          sections: @sections,
          fields: @fields
        }
      end

      def as_json(*args)
        to_h
      end

      private

      def ensure_section!(section_id)
        unless @sections.any? { |s| s['id'] == section_id }
          @sections << {
            'id' => section_id,
            'label' => section_id
          }
        end
      end
    end
  end
end
