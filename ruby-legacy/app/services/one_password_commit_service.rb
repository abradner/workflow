# frozen_string_literal: true

require_relative '../service_clients/op'
require_relative '../utils/colorized_logger'

module Services
  # Distinct committer interface that consumes perfectly formed Domain Models
  # and delegates mutating writes safely to the 1Password CLI wrapper.
  class OnePasswordCommitService
    def initialize(client: ServiceClients::Op.new, logger: Utils::ColorizedLogger.new)
      @client = client
      @logger = logger
    end

    def commit(domain_item)
      # Rely on edit_item strictly replacing identical structures mapped by Domain object ID
      if domain_item.id
        @logger.info "Editing 1Password Vault Item: #{domain_item.title} (ID: #{domain_item.id}) in #{domain_item.vault_name}"
        
        # Log stale fields if tracking dictates they are ignored
        unless domain_item.stale_field_ids.empty?
          @logger.warn "Item has #{domain_item.stale_field_ids.size} stale fields omitted from active AWS ingest pipeline."
        end

        @client.edit_item(domain_item.id, domain_item.as_json, vault: domain_item.vault_name)
      else
        @logger.info "Pushing New 1Password Vault Item: #{domain_item.title} in #{domain_item.vault_name} ..."
        @client.create_item(domain_item.as_json, vault: domain_item.vault_name)
      end
    end
  end
end
