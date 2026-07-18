# frozen_string_literal: true

require 'net/http'
require 'json'
require 'uri'

module ServiceClients
  class Keycloak
    attr_reader :base_url

    def initialize(base_url:, logger: nil)
      @base_url = base_url.chomp('/')
      @logger = logger
      @token = nil
    end

    def authenticate(username, password)
      uri = URI("#{@base_url}/realms/master/protocol/openid-connect/token")
      
      request = Net::HTTP::Post.new(uri)
      request.set_form_data(
        'client_id' => 'admin-cli',
        'username' => username,
        'password' => password,
        'grant_type' => 'password'
      )
      
      response = execute_request(uri, request, authenticated: false)
      @token = JSON.parse(response.body)['access_token']
    end
    def ready?
      uri = URI("#{@base_url}/realms/master/.well-known/openid-configuration")
      @logger&.debug("Checking Keycloak health at #{uri}")
      
      http = Net::HTTP.new(uri.host, uri.port)
      http.use_ssl = (uri.scheme == 'https')
      http.open_timeout = 5
      http.read_timeout = 5
      
      request = Net::HTTP::Get.new(uri)
      response = http.request(request)
      
      @logger&.debug("Health check response: #{response.code} #{response.message}")
      response.is_a?(Net::HTTPSuccess)
    rescue StandardError => e
      @logger&.debug("Health check failed: #{e.class} - #{e.message}")
      false
    end

    def create_realm(realm_name)
      payload = { realm: realm_name, enabled: true }
      post("/admin/realms", payload)
    end

    def import_client(realm_name, client_payload)
      post("/admin/realms/#{realm_name}/clients", client_payload)
    end

    def create_group(realm_name, group_name)
      post("/admin/realms/#{realm_name}/groups", { name: group_name })
    end

    def create_user(realm_name, user_payload)
      # Returns location header or we have to query it.
      # Net::HTTP responds with Net::HTTPCreated if successful
      response = post("/admin/realms/#{realm_name}/users", user_payload, return_response: true)
      
      # Extract ID from Location header if created
      if response.is_a?(Net::HTTPCreated) && response['location']
        response['location'].split('/').last
      else
        nil
      end
    end

    def get_users(realm_name, username: nil)
      path = "/admin/realms/#{realm_name}/users"
      path += "?username=#{URI.encode_www_form_component(username)}" if username
      get(path)
    end

    def get_groups(realm_name, search: nil)
      path = "/admin/realms/#{realm_name}/groups"
      path += "?search=#{URI.encode_www_form_component(search)}" if search
      get(path)
    end

    def add_user_to_group(realm_name, user_id, group_id)
      put("/admin/realms/#{realm_name}/users/#{user_id}/groups/#{group_id}", nil)
    end

    def fetch_saml_descriptor(realm_name)
      uri = URI("#{@base_url}/realms/#{realm_name}/protocol/saml/descriptor")
      request = Net::HTTP::Get.new(uri)
      
      response = execute_request(uri, request, authenticated: false)
      response.body
    end

    def fetch_realm_public_key(realm_name)
      uri = URI("#{@base_url}/realms/#{realm_name}")
      request = Net::HTTP::Get.new(uri)
      
      response = execute_request(uri, request, authenticated: false)
      JSON.parse(response.body)['public_key']
    end

    private

    def get(path)
      uri = URI("#{@base_url}#{path}")
      request = Net::HTTP::Get.new(uri)
      request['Authorization'] = "Bearer #{@token}"
      request['Content-Type'] = 'application/json'
      
      response = execute_request(uri, request)
      JSON.parse(response.body)
    end
    
    def post(path, payload, return_response: false)
      uri = URI("#{@base_url}#{path}")
      request = Net::HTTP::Post.new(uri)
      request['Authorization'] = "Bearer #{@token}"
      request['Content-Type'] = 'application/json'
      request.body = payload.to_json if payload
      
      response = execute_request(uri, request)
      return response if return_response
      
      response.body.empty? ? nil : JSON.parse(response.body)
    rescue JSON::ParserError
      response.body
    end

    def put(path, payload)
      uri = URI("#{@base_url}#{path}")
      request = Net::HTTP::Put.new(uri)
      request['Authorization'] = "Bearer #{@token}"
      request['Content-Type'] = 'application/json'
      request.body = payload.to_json if payload
      
      execute_request(uri, request)
    end

    def execute_request(uri, request, authenticated: true)
      raise "Not authenticated" if authenticated && @token.nil?
      
      @logger&.debug("Keycloak API Request: #{request.method} #{uri}")
      
      http = Net::HTTP.new(uri.host, uri.port)
      http.use_ssl = (uri.scheme == 'https')
      http.open_timeout = 10
      
      response = http.request(request)
      @logger&.debug("Keycloak API Response: #{response.code} #{response.message}")
      
      # 409 Conflict is often expected (e.g. realm exists, user exists)
      unless response.is_a?(Net::HTTPSuccess) || response.code == '409' || response.code == '201'
        @logger&.error("Keycloak API Error Body: #{response.body}")
        raise "Keycloak API Error: #{response.code} #{response.message} - #{response.body}"
      end
      
      response
    end
  end
end
