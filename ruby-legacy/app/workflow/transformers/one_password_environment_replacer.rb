# frozen_string_literal: true

module Workflow
  module Transformers
    # Recursively replaces the source environment namespace with the target
    # environment namespace across all text-based fields within a Domain Item.
    class OnePasswordEnvironmentReplacer
      def initialize(source_env:, target_env:, logger:)
        @source_env = source_env
        @target_env = target_env
        @logger = logger
      end

      def call(domain_item)
        return unless domain_item

        domain_item.sections.each do |section|
          section['id'] = section['id'].gsub(@source_env, @target_env) if section['id'].is_a?(String)
          section['label'] = section['label'].gsub(@source_env, @target_env) if section['label'].is_a?(String)
        end

        domain_item.fields.each do |field|
          if field['section'] && field['section']['id'].is_a?(String)
            field['section']['id'] = field['section']['id'].gsub(@source_env, @target_env)
          end

          next unless field['type'] == 'CONCEALED' && field['value'].is_a?(String)

          mapped_string = field['value'].gsub(@source_env, @target_env)
          
          # We update in-place rather than looping upsert_field natively since we mutate the section mapping cleanly above
          unless mapped_string == field['value']
            field['value'] = mapped_string
            domain_item.touched_field_ids << field['id'] if field['id']
          end
        end
        
        deduplicate_fields!(domain_item)
        
        @logger.debug "  => Replaced '#{@source_env}' with '#{@target_env}' in domain item '#{domain_item&.title}'"
      end

      private

      # What: Merges identically named fields within the same section that collide after environment string replacement.
      #
      # Why: When we replace `dev3` with `dev4` across all section IDs and field references, a newly mapped field
      # (e.g. section `pmn-dev3-config`, label `conn`) can land on the same section/label as a field that already
      # exists in the hydrated 1Password item (section `pmn-dev4-config`, label `conn`). Without deduplication,
      # the 1Password CLI receives two fields with the same label in the same section, causing ambiguous field
      # resolution errors and potentially silent data loss during `op item edit`.
      #
      # How: Relies on array ordering. The hydrator inserts authentic 1P fields (carrying real tracked IDs) at
      # the front of `domain_item.fields`. The mapper appends newly generated fields to the back. When we iterate,
      # authentic fields populate the `deduplicated` hash first. When a collision is detected from a later
      # (mapper-generated) field, we overwrite the value/type on the existing entry, preserving the authentic 1P ID
      # so that `op item edit` can resolve the field unambiguously.
      def deduplicate_fields!(domain_item)
        deduplicated = {}
        
        domain_item.fields.each do |f|
          next unless f['section'] && f['label']
          key = "#{f.dig('section', 'id')}-#{f['label']}"
          
          if deduplicated.key?(key)
            existing = deduplicated[key]
            
            # Merge the newer value into the existing field (which was hydrated first natively from 1Password upstream)
            # This perfectly preserves the upstream ID while injecting the translated value.
            existing['value'] = f['value']
            existing['type'] = f['type']
            
            @logger.debug "  => Deduplicating field: merged new value into existing tracked 1P field ID '#{existing['id'] || 'new'}' for #{key}"
            
            domain_item.touched_field_ids << existing['id'] if existing['id']
          else
            deduplicated[key] = f
          end
        end

        domain_item.fields = deduplicated.values
        
        deduplicated_sections = {}
        domain_item.sections.each { |s| deduplicated_sections[s['id']] = s }
        domain_item.sections = deduplicated_sections.values
      end
    end
  end
end
