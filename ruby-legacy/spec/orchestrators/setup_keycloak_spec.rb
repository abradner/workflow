# frozen_string_literal: true

require 'spec_helper'
require_relative '../../app/workflow/orchestrators/setup_keycloak'
require_relative '../../app/workflow/execution_context'

RSpec.describe Workflow::Orchestrators::SetupKeycloak do
  let(:config) do
    cfg = double('Config', environments: ['dev4'], project_name: 'pmn', tld: 'f-ck.xyz', dest_dir: '/dest')
    allow(cfg).to receive(:is_a?).and_return(true)
    cfg
  end
  let(:logger) { instance_double('Logger').as_null_object }
  let(:context) { Workflow::ExecutionContext.new(config: config, logger: logger) }
  let(:setup_service) { instance_double('Services::KeycloakSetupService') }

  subject(:orchestrator) { described_class.new(config: config) }

  before do
    allow(Services::KeycloakSetupService).to receive(:new).and_return(setup_service)
  end

  describe '#act_phase' do
    it 'validates environment dependencies and does not trigger side-effects' do
      allow(ENV).to receive(:[]).with('KEYCLOAK_ADMIN').and_return('admin')
      allow(ENV).to receive(:[]).with('KEYCLOAK_ADMIN_PASSWORD').and_return('pass')
      
      expect(setup_service).not_to receive(:setup)
      orchestrator.act_phase(context)
    end
  end

  describe '#commit_phase' do
    it 'executes the setup service and extracts the SSO descriptor to disk' do
      expect(setup_service).to receive(:setup).and_return({ xml: '<sso_xml/>', b64: 'PHNz' })
      
      fs_double = instance_double('Services::FilesystemService')
      allow(Services::FilesystemService).to receive(:new).and_return(fs_double)
      allow(fs_double).to receive(:directory_exists?).and_return(true)
      
      expect(fs_double).to receive(:write_file)
        .with(%r{/dest/pmn-keycloak/overlay/dev4/sso\.xml$}, '<sso_xml/>')
      expect(fs_double).to receive(:write_file)
        .with(%r{/dest/pmn-keycloak/overlay/dev4/sso\.xml\.b64$}, 'PHNz')
      
      orchestrator.commit_phase(context)
    end
  end
end
