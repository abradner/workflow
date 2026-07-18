# frozen_string_literal: true

require 'spec_helper'
require 'fileutils'
require 'yaml'
require_relative '../../app/workflow/orchestrators/sync_workloads'
require_relative '../../app/workflow/execution_context'
require_relative '../../config/config'
require_relative '../../app/utils/colorized_logger'
require_relative '../../app/services/filesystem_service'

RSpec.describe Workflow::Orchestrators::SyncWorkloads do
  let(:config) do
    cfg = Config.new
    allow(cfg).to receive_messages(
      environments: ['dev4'], 
      source_dir: '/src', 
      dest_dir: '/dest', 
      project_name: 'wtf',
      source_env: 'dev3',
      external_secrets_api_version: 'external-secrets.io/v1',
      registry_hostname: 'cr.infra.fqdn',
      tld: 'f-ck.xyz',
      registry_1p_item_id: '12345'
    )
    cfg
  end
  let(:logger) { Utils::ColorizedLogger.new(StringIO.new) }
  let(:context) do
    ctx = Workflow::ExecutionContext.new(config: config, logger: logger, options: {})
    ctx.apps = ['test-app']
    ctx
  end
  let(:fs_mock) { instance_double(Services::FilesystemService).as_null_object }
  let(:orchestrator) do
    allow(Services::FilesystemService).to receive(:new).and_return(fs_mock)
    described_class.new(config: config)
  end

  describe '#commit_phase (Public Execution Integration)' do
    it 'delegates generation logic to isolated transformers perfectly mapping resources and resolving Service Abstraction' do
      allow(fs_mock).to receive(:base_filename) { |path| File.basename(path) }
      allow(fs_mock).to receive(:extension) { |path| File.extname(path).downcase }

      allow(fs_mock).to receive(:create_directory)

      # Pipeline extraction targets
      allow(fs_mock).to receive(:directory_exists?) do |path|
        ['/src/test-app/base', '/src/test-app/overlay/dev3'].include?(path)
      end

      allow(fs_mock).to receive(:path_entries).with('/src/test-app/base').and_return([
        '/src/test-app/base/secrets.yaml'
      ])

      allow(fs_mock).to receive(:path_entries).with('/src/test-app/overlay/dev3').and_return([
        '/src/test-app/overlay/dev3/kustomization.yaml',
        '/src/test-app/overlay/dev3/secrets.yaml'
      ])

      # Basic Secret structure mappings for Legacy Modernizer
      base_secret = { metadata: { name: 'test-secret' },
                      spec: { data: [{ remoteRef: { key: 'placeholder', property: 'secret_val' } }] } }
      allow(fs_mock).to receive(:read_yaml).with('/src/test-app/base/secrets.yaml').and_return(base_secret)

      raw_patches = [{ op: 'replace', path: '/spec/data/0/remoteRef/key', value: 'dev3/wtf/config' }]
      allow(fs_mock).to receive(:read_yaml).with('/src/test-app/overlay/dev3/secrets.yaml').and_return(raw_patches)


      # The Old Dev3 State ConfigMap containing AWS DNS mappings
      source_kustomization = {
        kind: 'Kustomization',
        namespace: 'wtf-dev3',
        patches: [],
        configMapGenerator: [
          { name: 'test-map',
            literals: [
              'database.url=neons-dev-rds-aurora.cluster-cje48k6m23rh.ap-southeast-2.rds.amazonaws.com',
              'queue.url=lkc-1w5ykj.dom8pmemvwy.ap-southeast-2.aws.confluent.cloud',
              'my.custom.env=dev3'
            ] }
        ]
      }
      allow(fs_mock).to receive(:read_yaml).with('/src/test-app/overlay/dev3/kustomization.yaml').and_return(source_kustomization)

      # Execution run!
      orchestrator.act_phase(context)
      orchestrator.commit_phase(context)

      # EXPECT THE CONFIGMAP GENERATOR TO INJECT CLUSTER LOCAL RESOURCES
      expect(fs_mock).to have_received(:write_yaml).with('/dest/test-app/overlay/dev4/kustomization.yaml', anything) do |_path, doc|
        expect(doc[:namespace]).to eq('wtf-dev4')
        expect(doc[:configMapGenerator][0][:literals]).to eq([
                                                               'database.url=test-app-pg.wtf-dev4.svc.cluster.local',
                                                               'queue.url=test-app-kafka.wtf-dev4.svc.cluster.local',
                                                               'my.custom.env=dev4'
                                                             ])
        
        # Verify it successfully pushed external-services downstream
        expect(doc[:resources]).to include('external-services.yaml')

        secret_patch = doc[:patches].find { |p| p[:target] && p[:target][:kind] == 'ExternalSecret' }
        expect(secret_patch[:target][:version]).to eq('v1')
      end

      # EXPECT EXTERNAL-SERVICES.YAML TO EMERGE
      expect(fs_mock).to have_received(:write_yaml).with('/dest/test-app/overlay/dev4/external-services.yaml', anything) do |_path, docs|
        pg_svc = docs.find { |d| d[:metadata][:name] == 'test-app-pg' }
        kafka_svc = docs.find { |d| d[:metadata][:name] == 'test-app-kafka' }

        expect(pg_svc[:spec][:type]).to eq('ExternalName')
        expect(pg_svc[:spec][:externalName]).to eq('pg.wtf.dev4.f-ck.xyz')

        expect(kafka_svc[:spec][:type]).to eq('ExternalName')
        expect(kafka_svc[:spec][:externalName]).to eq('kafka.wtf.dev4.f-ck.xyz')
      end

      # EXPECT SECRET PATCH TO APPLY PROPERLY
      expect(fs_mock).to have_received(:write_yaml).with('/dest/test-app/overlay/dev4/secrets.yaml', anything) do |_path, mutations|
        expect(mutations[0]).to eq({ op: 'replace', path: '/spec/data/0/remoteRef/key',
                                     value: 'k8s-wtf-dev4/wtf-config/secret_val' })
        expect(mutations[1]).to eq({ op: 'remove', path: '/spec/data/0/remoteRef/property' })
      end
    end
  end
end
