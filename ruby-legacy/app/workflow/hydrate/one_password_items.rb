# frozen_string_literal: true

require_relative '../../service_clients/op'
require_relative '../../domain/one_password/item'

module Workflow
  module Hydrate
    # Queries the 1Password CLI to build Domain::OnePassword::Item structures
    class OnePasswordItems
      def self.call(context)
        return if context.one_password_items_hydrated?

        client = ServiceClients::Op.new

        # Fetch for all environments including target AND source 
        # Source must be fetched because we might map generic legacy AWS tokens
        envs = (context.config.environments + [context.config.source_env]).uniq

        envs.each do |env|
          item_title = "k8s-#{context.config.project_name}-#{env}"
          vault = context.config.op_vault_name

          context.logger.info "Hydrating 1Password vault state for: #{item_title} in #{vault}"
          
          existing_json = client.get_item(item_title, vault: vault)

          item = Domain::OnePassword::Item.new(
            title: item_title,
            category: 'SECURE_NOTE',
            vault_name: vault,
            existing_item_json: existing_json
          )

          context.one_password_items[env] = item
        end
      end
    end
  end
end
