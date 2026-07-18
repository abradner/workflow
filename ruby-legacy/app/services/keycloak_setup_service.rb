# frozen_string_literal: true

require_relative '../service_clients/keycloak'
require 'base64'

module Services
  class KeycloakSetupService
    REALM_NAME = 'neons'
    CLIENT_ID = 'Optus CFS'

    def initialize(base_url:, logger: nil)
      @logger = logger
      @client = ServiceClients::Keycloak.new(base_url: base_url, logger: @logger)
    end

    def setup(admin_username:, admin_password:)
      log "Waiting for Keycloak to be ready at #{@client.base_url}..."
      wait_for_ready

      log "Authenticating as #{admin_username}..."
      @client.authenticate(admin_username, admin_password)

      log "Creating realm '#{REALM_NAME}'..."
      @client.create_realm(REALM_NAME)

      log "Importing client '#{CLIENT_ID}'..."
      import_client

      setup_groups
      setup_users

      log "Exporting SAML descriptor..."
      descriptor = @client.fetch_saml_descriptor(REALM_NAME)
      
      # Return both raw and b64 (w/o newlines)
      {
        xml: descriptor,
        b64: Base64.strict_encode64(descriptor)
      }
    end

    private

    def wait_for_ready(max_attempts: 12, delay: 5)
      attempts = 0
      until @client.ready?
        attempts += 1
        raise "Keycloak did not become ready in time" if attempts >= max_attempts
        log "Keycloak not ready yet... sleeping for #{delay}s"
        sleep delay
      end
    end

    def import_client
      oidc_client_payload = {
        clientId: CLIENT_ID,
        enabled: true,
        protocol: 'openid-connect',
        directAccessGrantsEnabled: true,
        publicClient: false,
        secret: 'local_pmn_client_secret',
        redirectUris: [
          'http://localhost:8080/*', 
          'http://host.docker.internal:8080/*', 
          'https://*' 
        ],
        webOrigins: ['*']
      }
      
      saml_client_payload = {
        clientId: "#{@client.base_url}/realms/#{REALM_NAME}",
        enabled: true,
        protocol: 'saml',
        redirectUris: [
          'http://localhost:8080/*', 
          'http://host.docker.internal:8080/*', 
          'https://*' 
        ],
        attributes: {
          "saml.assertion.signature" => "false",
          "saml.server.signature" => "true",       # Quarkus expects the assertion or response to be signed
          "saml.client.signature" => "false",
          "saml.encrypt" => "false",
          "saml.authnstatement" => "true",
          "saml.force.post.binding" => "true"
        }
      }
      
      @client.import_client(REALM_NAME, oidc_client_payload)
      @client.import_client(REALM_NAME, saml_client_payload)
    end

    def setup_groups
      %w[CN=PMN_Admin_Access CN=PMN_Porting_Team_Access CN=PMN_ReadOnly_Access].each do |group|
        log "Creating group '#{group}'..."
        @client.create_group(REALM_NAME, group)
      end
    end

    def setup_users
      users = [
        { user: 'admin', email: 'admin@optus.com.au', first: 'The', last: 'Admin', group: 'CN=PMN_Admin_Access' },
        { user: 'portingteam', email: 'portingteam@optus.com.au', first: 'porting', last: 'team', group: 'CN=PMN_Porting_Team_Access' },
        { user: 'readonly', email: 'readonly@optus.com.au', first: 'read', last: 'only', group: 'CN=PMN_ReadOnly_Access' }
      ]

      users.each do |u|
        log "Creating user '#{u[:user]}'..."
        payload = {
          username: u[:user],
          email: u[:email],
          firstName: u[:first],
          lastName: u[:last],
          enabled: true,
          credentials: [{ type: 'password', value: u[:user], temporary: false }]
        }

        user_id = @client.create_user(REALM_NAME, payload)
        
        # If user existed, we need to query their ID
        unless user_id
          existing = @client.get_users(REALM_NAME, username: u[:user])
          user_id = existing.first['id'] if existing && !existing.empty?
        end

        if user_id
          assign_user_to_group(user_id, u[:user], u[:group])
        end
      end
    end

    def assign_user_to_group(user_id, username, group_name)
      groups = @client.get_groups(REALM_NAME, search: group_name)
      if groups && !groups.empty?
        group_id = groups.first['id']
        log "Adding user '#{username}' to group '#{group_name}'..."
        @client.add_user_to_group(REALM_NAME, user_id, group_id)
      end
    end

    def log(message)
      @logger&.info(message)
    end
  end
end
