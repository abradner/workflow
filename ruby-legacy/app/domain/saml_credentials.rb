# frozen_string_literal: true

module Domain
  # Strongly-typed container for extracted Keycloak SAML configuration.
  SamlCredentials = Data.define(:public_key, :sso_xml) do
    # Formats the raw Base64 public key block into the explicit PEM structure
    # expected by Quarkus SmallRye JWT configuration.
    def pem_public_key
      return nil if public_key.nil? || public_key.empty?

      lines = public_key.chars.each_slice(64).map(&:join)
      ["-----BEGIN PUBLIC KEY-----", *lines, "-----END PUBLIC KEY-----"].join("\n")
    end
  end
end
