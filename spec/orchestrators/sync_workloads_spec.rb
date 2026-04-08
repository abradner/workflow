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
    allow(cfg).to receive_messages(environments: ['dev4'], source_dir: '/src', dest_dir: '/dest', project_name: 'wtf')
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
    it 'delegates generation logic to isolated private processor methods perfectly mapping resources' do
      allow(fs_mock).to receive(:base_filename) { |path| File.basename(path) }
      allow(fs_mock).to receive(:extension) { |path| File.extname(path).downcase }

      allow(fs_mock).to receive(:create_directory)

      # Setup Mock Execution paths avoiding Baseline generation for this test focus
      allow(fs_mock).to receive(:directory_exists?).with('/src/test-app/base').and_return(false)
      allow(fs_mock).to receive(:directory_exists?).with('/src/test-app/overlay/dev3').and_return(true)

      # Expose only Kustomization and Patches natively for generating the target overlay
      allow(fs_mock).to receive(:path_entries).with('/src/test-app/overlay/dev3').and_return([
                                                                                               '/src/test-app/overlay/dev3/kustomization.yaml',
                                                                                               '/src/test-app/overlay/dev3/secrets.yaml'
                                                                                             ])

      # ---------------------------------------------------------
      # 1. Kustomization ConfigMapGenerator validation injection
      # ---------------------------------------------------------
      source_kustomization = {
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

      # ---------------------------------------------------------
      # 2. Secret Patch validation injection
      # ---------------------------------------------------------
      base_secret = { metadata: { name: 'test-secret' },
                      spec: { data: [{ remoteRef: { key: 'placeholder', property: 'secret_val' } }] } }
      raw_patches = [{ op: 'replace', path: '/spec/data/0/remoteRef/key', value: 'dev3/wtf/config' }]

      allow(fs_mock).to receive(:file_exists?).with('/dest/test-app/base/secrets.yaml').and_return(true)
      allow(fs_mock).to receive(:file_exists?).with('/src/test-app/overlay/dev3/secrets.yaml').and_return(true)
      allow(fs_mock).to receive(:read_yaml_stream).with('/dest/test-app/base/secrets.yaml').and_return([base_secret])
      allow(fs_mock).to receive(:read_yaml_stream).with('/src/test-app/overlay/dev3/secrets.yaml').and_return(raw_patches)

      # Execution!
      orchestrator.act_phase(context)

      # Asset Output of Kustomization
      expect(fs_mock).to receive(:write_yaml).with('/dest/test-app/overlay/dev4/kustomization.yaml',
                                                   anything) do |_path, doc|
        expect(doc[:namespace]).to eq('wtf-dev4')
        expect(doc[:configMapGenerator][0][:literals]).to eq([
                                                               'database.url=pg.wtf.dev4.f-ck.xyz',
                                                               'queue.url=kafka.wtf.dev4.f-ck.xyz',
                                                               'my.custom.env=dev4'
                                                             ])
        secret_patch = doc[:patches].find { |p| p[:target] && p[:target][:kind] == 'ExternalSecret' }
        expect(secret_patch[:target][:version]).to eq('v1')
      end

      # Asset Output of Patches Array Structure (overlay secrets)
      expect(fs_mock).to receive(:write_yaml_stream).with('/dest/test-app/overlay/dev4/secrets.yaml',
                                                          anything) do |_path, mutations|
        expect(mutations[0]).to eq({ op: 'replace', path: '/spec/data/0/remoteRef/key',
                                     value: 'k8s-wtf-dev4/wtf-config/secret_val' })
        expect(mutations[1]).to eq({ op: 'remove', path: '/spec/data/0/remoteRef/property' })
      end

      # Validate pipeline works and completes execution without exceptions
      expect { orchestrator.commit_phase(context) }.not_to raise_error
    end
  end
end
