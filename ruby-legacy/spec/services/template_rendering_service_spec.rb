# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/services/template_rendering_service'

RSpec.describe Services::TemplateRenderingService do
  subject(:service) { described_class.new }

  describe '#flatten_hash' do
    it 'flattens a nested hash with dot-separated keys' do
      input = {
        'cluster' => { 'id' => 'abc123', 'secret' => 'xyz789' },
        'certs' => { 'os' => { 'crt' => 'CERT_DATA', 'key' => 'KEY_DATA' } }
      }

      result = service.flatten_hash(input)

      expect(result).to eq(
        'cluster.id' => 'abc123',
        'cluster.secret' => 'xyz789',
        'certs.os.crt' => 'CERT_DATA',
        'certs.os.key' => 'KEY_DATA'
      )
    end

    it 'handles a flat hash unchanged' do
      input = { 'simple' => 'value' }
      expect(service.flatten_hash(input)).to eq('simple' => 'value')
    end

    it 'handles an empty hash' do
      expect(service.flatten_hash({})).to eq({})
    end
  end

  describe '#extract_placeholders' do
    it 'finds all unique placeholder keys' do
      template = <<~YAML
        token: {{ trustdinfo.token }}
        ca:
            crt: {{ certs.os.crt }}
            key: {{ certs.os.key }}
        id: {{ cluster.id }}
      YAML

      expect(service.extract_placeholders(template)).to eq(
        %w[certs.os.crt certs.os.key cluster.id trustdinfo.token]
      )
    end

    it 'deduplicates repeated placeholders' do
      template = "crt: {{ certs.os.crt }}\nca: {{ certs.os.crt }}"
      expect(service.extract_placeholders(template)).to eq(%w[certs.os.crt])
    end

    it 'returns empty for content without placeholders' do
      expect(service.extract_placeholders('no placeholders here')).to eq([])
    end

    it 'handles varied whitespace inside braces' do
      template = 'a: {{key.one}} b: {{  key.two  }}'
      expect(service.extract_placeholders(template)).to eq(%w[key.one key.two])
    end
  end

  describe '#missing_keys' do
    let(:secrets) { { 'cluster.id' => 'abc', 'cluster.secret' => 'xyz' } }

    it 'returns empty when all keys are present' do
      template = '{{ cluster.id }} {{ cluster.secret }}'
      expect(service.missing_keys(template, secrets)).to be_empty
    end

    it 'returns missing keys' do
      template = '{{ cluster.id }} {{ certs.os.crt }}'
      expect(service.missing_keys(template, secrets)).to eq(%w[certs.os.crt])
    end
  end

  describe '#render' do
    let(:secrets) do
      {
        'cluster.id' => 'R10sHpCQ==',
        'certs.os.crt' => 'LS0tLS1CRUdJ',
        'trustdinfo.token' => 'ao3hmv.rn45d0'
      }
    end

    it 'replaces all placeholders with secret values' do
      template = <<~YAML
        id: {{ cluster.id }}
        ca:
            crt: {{ certs.os.crt }}
        token: {{ trustdinfo.token }}
      YAML

      result = service.render(template, secrets)

      expect(result).to eq(<<~YAML)
        id: R10sHpCQ==
        ca:
            crt: LS0tLS1CRUdJ
        token: ao3hmv.rn45d0
      YAML
    end

    it 'preserves surrounding content (comments, etc.)' do
      template = 'token: {{ trustdinfo.token }} # The machine token'
      result = service.render(template, secrets)
      expect(result).to eq('token: ao3hmv.rn45d0 # The machine token')
    end

    it 'raises KeyError when a placeholder cannot be resolved' do
      template = '{{ cluster.id }} {{ unknown.key }}'
      expect { service.render(template, secrets) }
        .to raise_error(KeyError, /unknown\.key/)
    end

    it 'handles templates with no placeholders' do
      template = 'static: content'
      expect(service.render(template, secrets)).to eq('static: content')
    end
  end
end
