# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/domain/saml_credentials'

RSpec.describe Domain::SamlCredentials do
  describe '#pem_public_key' do
    it 'formats base64 strings into 64-character PEM blocks' do
      # 130 characters long
      raw_key = 'A' * 64 + 'B' * 64 + 'CC'
      creds = described_class.new(public_key: raw_key, sso_xml: '<xml/>')

      expected = [
        '-----BEGIN PUBLIC KEY-----',
        'A' * 64,
        'B' * 64,
        'CC',
        '-----END PUBLIC KEY-----'
      ].join("\n")

      expect(creds.pem_public_key).to eq(expected)
    end

    it 'returns nil if the public key is missing' do
      creds = described_class.new(public_key: nil, sso_xml: '<xml/>')
      expect(creds.pem_public_key).to be_nil
    end
  end
end
