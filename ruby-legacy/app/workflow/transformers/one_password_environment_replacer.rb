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
      # How: On collision, uses `domain_item.hydrated_field_ids` to identify which field originated from the
      # live 1Password vault. The vault-native field's identity (ID) is always preserved; the colliding field's
      # value is merged into it. This is order-independent — it does not matter which field appears first in the
      # array. If neither field is vault-native (both generated locally), the last-seen value wins.
      def deduplicate_fields!(domain_item)
        deduplicated = {}
        
        domain_item.fields.each do |f|
          next unless f['section'] && f['label']
          key = "#{f.dig('section', 'id')}-#{f['label']}"
          
          if deduplicated.key?(key)
            existing = deduplicated[key]
            existing_is_hydrated = existing['id'] && domain_item.hydrated_field_ids.include?(existing['id'])
            current_is_hydrated = f['id'] && domain_item.hydrated_field_ids.include?(f['id'])
            
            if existing_is_hydrated || !current_is_hydrated
              # Keep the existing (vault-native) field's identity, take the colliding field's value
              existing['value'] = f['value']
              existing['type'] = f['type']
              domain_item.touched_field_ids << existing['id'] if existing['id']
              
              @logger.debug "  => Deduplicating field: merged new value into vault-tracked field ID '#{existing['id']}' for #{key}"
            else
              # Current field is vault-native but existing isn't — swap: keep current's identity with existing's value
              f['value'] = existing['value']
              f['type'] = existing['type']
              domain_item.touched_field_ids << f['id']
              deduplicated[key] = f
              
              @logger.debug "  => Deduplicating field: promoted vault-tracked field ID '#{f['id']}' for #{key}"
            end
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
