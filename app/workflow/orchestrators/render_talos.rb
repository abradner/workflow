# frozen_string_literal: true

require 'yaml'
require_relative '../orchestrator'
require_relative '../../service_clients/op'
require_relative '../../services/template_rendering_service'
require_relative '../../services/filesystem_service'

module Workflow
  module Orchestrators
    # Reads a 1Password Secure Note containing secrets YAML,
    # then hydrates .template.yaml files by substituting {{ dotted.key }} placeholders.
    class RenderTalos < Orchestrator
      TEMPLATE_GLOB = '*.template.yaml'

      def initialize(config:, fs: Services::FilesystemService.new)
        super(config: config)
        @op_client = ServiceClients::Op.new
        @rendering_service = Services::TemplateRenderingService.new
        @fs = fs
        @pending_writes = {}
      end

      def act_phase(context)
        item_id = @config.talos_item_id
        template_dir = @config.talos_template_dir

        # 1. Fetch secrets from 1Password
        context.logger.info "Reading Secure Note #{item_id} from 1Password..."
        raw_yaml = @op_client.read_note(item_id)
        secrets_hash = YAML.safe_load(raw_yaml)
        @flat_secrets = @rendering_service.flatten_hash(secrets_hash)
        context.logger.info "Loaded #{@flat_secrets.keys.length} secret keys."

        # 2. Discover template files
        template_files = Dir.glob(File.join(template_dir, TEMPLATE_GLOB))
        context.logger.info "Found #{template_files.length} template files in #{template_dir}."

        # 3. Validate all placeholders resolve and build mapped payloads
        all_missing = []
        template_files.each do |path|
          content = @fs.read_file(path)
          missing = @rendering_service.missing_keys(content, @flat_secrets)
          if missing.empty?
            output_name = File.basename(path).sub('.template.yaml', '.yaml')
            output_path = File.join(template_dir, output_name)
            @pending_writes[output_path] = @rendering_service.render(content, @flat_secrets)
          else
            context.logger.error "#{File.basename(path)}: missing keys: #{missing.join(', ')}"
            all_missing.concat(missing)
          end
        end

        unless all_missing.empty?
          raise "Cannot hydrate: #{all_missing.uniq.length} unresolved placeholder(s). " \
                'Add them to the Secure Note or fix the templates.'
        end

        context.logger.info 'All placeholders validated ✓'
      end

      def commit_phase(context)
        @pending_writes.each do |output_path, rendered|
          @fs.write_file(output_path, rendered)
          context.logger.info "Wrote #{output_path}"
        end
      end
    end
  end
end
