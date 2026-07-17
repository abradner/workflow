# frozen_string_literal: true

require_relative '../service_clients/op'
require 'json'

module Services
  # High level builder for 1Password items
  class OnePasswordService
    def initialize(project_name:, client: ServiceClients::Op.new)
      @client = client
      @project_name = project_name
    end

    def ingest_vault_item(env, extracted_secrets)
      item_title = "k8s-#{@project_name}-#{env}"

      sections = []
      fields = []

      extracted_secrets.each do |secret|
        # Sanitize AWS name e.g. dev1/wtf/config -> wtf-config
        section_id = sanitize_section_id(secret[:name])
        section_label = section_id

        sections << {
          id: section_id,
          label: section_label
        }

        if secret[:string]
          begin
            json_payload = JSON.parse(secret[:string])
            json_payload.each do |k, v|
              fields << {
                section: { id: section_id },
                label: k,
                value: v.to_s,
                type: 'CONCEALED'
              }
            end
          rescue JSON::ParserError
            fields << {
              section: { id: section_id },
              label: 'password',
              value: secret[:string],
              type: 'CONCEALED'
            }
          end
        elsif secret[:binary]
          fields << {
            section: { id: section_id },
            label: 'password',
            value: secret[:binary],
            type: 'CONCEALED'
          }
        end
      end

      op_template = {
        title: item_title,
        category: 'SECURE_NOTE',
        sections: sections,
        fields: fields
      }

      @client.create_item(op_template)
    end

    private

    def sanitize_section_id(aws_name)
      # Removes leading environment prefix and converts remaining slashes to hyphens
      parts = aws_name.split('/')
      parts.shift if parts.length > 1 # drop env e.g. dev3
      parts.join('-')
    end
  end
end
