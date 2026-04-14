# frozen_string_literal: true

require_relative '../orchestrator'
require_relative '../../services/keycloak_setup_service'
require_relative '../../services/filesystem_service'

module Workflow
  module Orchestrators
    class SetupKeycloak < Orchestrator
      def initialize(config:)
        super
      end

      def act_phase(context)
        context.logger.info "Planning Keycloak Setup for environments: #{@config.environments.join(', ')}"
        if ENV['KEYCLOAK_ADMIN'].nil? || ENV['KEYCLOAK_ADMIN_PASSWORD'].nil?
          context.logger.info "Warning: KEYCLOAK_ADMIN or KEYCLOAK_ADMIN_PASSWORD env vars are not set. Using defaults 'admin'."
        end
      end

      def commit_phase(context)
        admin_user = ENV.fetch('KEYCLOAK_ADMIN', 'admin')
        admin_pass = ENV.fetch('KEYCLOAK_ADMIN_PASSWORD', 'admin')
        fs = Services::FilesystemService.new

        @config.environments.each do |env|
          target_url = "https://pmn-keycloak.#{@config.project_name}.#{env}.#{@config.tld}"
          context.logger.info "--- Setting up Keycloak for #{env} at #{target_url} ---"

          service = Services::KeycloakSetupService.new(base_url: target_url, logger: context.logger)

          begin
            descriptors = service.setup(admin_username: admin_user, admin_password: admin_pass)
            
            app_dir = File.join(@config.dest_dir, 'pmn-keycloak', 'overlay', env)
            fs.create_directory(app_dir) unless fs.directory_exists?(app_dir)
            
            sso_xml_path = File.join(app_dir, 'sso.xml')
            sso_b64_path = File.join(app_dir, 'sso.xml.b64')

            fs.write_file(sso_xml_path, descriptors[:xml])
            context.logger.info "Wrote SAML Descriptor to #{sso_xml_path}"
            
            fs.write_file(sso_b64_path, descriptors[:b64])
            context.logger.info "Wrote Base64 SAML Descriptor to #{sso_b64_path}"
          rescue => e
            context.logger.error "Failed to setup Keycloak for #{env}: #{e.message}"
          end
        end
        context.logger.info "Completed SetupKeycloak orchestrator block."
      end
    end
  end
end
