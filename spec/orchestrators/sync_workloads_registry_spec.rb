# frozen_string_literal: true

require 'spec_helper'
require 'json'
require 'yaml'
require_relative '../../app/workflow/orchestrators/sync_workloads'
require_relative '../../app/workflow/execution_context'
require_relative '../../config/config'
require_relative '../../app/utils/colorized_logger'
require_relative '../../app/services/filesystem_service'

RSpec.describe Workflow::Orchestrators::SyncWorkloads, 'registry and backport transforms' do
  let(:config) do
    cfg = Config.new
    allow(cfg).to receive_messages(environments: ['dev4'], source_dir: '/src', dest_dir: '/dest',
                                   registry_hostname: 'mock.registry.test', registry_1p_item_id: 'mock_item_id')
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

  describe '#commit_phase base migration' do
    before do
      allow(fs_mock).to receive(:base_filename) { |path| File.basename(path) }
      allow(fs_mock).to receive(:extension) { |path| File.extname(path).downcase }

      allow(fs_mock).to receive(:create_directory)

      allow(fs_mock).to receive(:directory_exists?) do |path|
        path == '/src/test-app/base'
      end
    end

    it 'generates registry-pull-secret.yaml with correct ExternalSecret structure' do
      allow(fs_mock).to receive(:path_entries).with('/src/test-app/base').and_return([])

      orchestrator.act_phase(context)

      expect(fs_mock).to receive(:write_yaml).with(
        '/dest/test-app/base/registry-pull-secret.yaml',
        anything
      ) do |_path, doc|
        expect(doc[:apiVersion]).to eq('external-secrets.io/v1')
        expect(doc[:kind]).to eq('ExternalSecret')
        expect(doc[:metadata][:name]).to eq('test-app-registry')

        target = doc[:spec][:target]
        expect(target[:name]).to eq('test-app-registry')
        expect(target[:template][:type]).to eq('kubernetes.io/dockerconfigjson')

        dockerconfig = target[:template][:data]['.dockerconfigjson']
        parsed = JSON.parse(dockerconfig)
        expect(parsed['auths']).to have_key('mock.registry.test')

        expect(doc[:spec][:data].length).to eq(2)
        expect(doc[:spec][:data][0][:remoteRef][:key]).to eq('mock_item_id')
        expect(doc[:spec][:data][0][:remoteRef][:property]).to eq('username')
        expect(doc[:spec][:data][1][:remoteRef][:property]).to eq('password')

        store_ref = doc[:spec][:secretStoreRef]
        expect(store_ref[:name]).to eq('onepassword-backend')
        expect(store_ref[:kind]).to eq('ClusterSecretStore')
      end

      orchestrator.commit_phase(context)
    end

    it 'strips topologySpreadConstraints from Deployment docs' do
      deployment_doc = {
        apiVersion: 'apps/v1',
        kind: 'Deployment',
        metadata: { name: 'test-app' },
        spec: {
          template: {
            spec: {
              containers: [{ name: 'app', image: 'test:latest' }],
              topologySpreadConstraints: [
                { maxSkew: 1, topologyKey: 'topology.kubernetes.io/zone' }
              ]
            }
          }
        }
      }

      allow(fs_mock).to receive(:path_entries).with('/src/test-app/base').and_return([
                                                                                       '/src/test-app/base/deployment.yaml'
                                                                                     ])
      allow(fs_mock).to receive(:read_yaml).with('/src/test-app/base/deployment.yaml').and_return(deployment_doc)

      orchestrator.act_phase(context)

      expect(fs_mock).to receive(:write_yaml).with('/dest/test-app/base/deployment.yaml',
                                                          anything) do |_path, doc|
        template_spec = doc.dig(:spec, :template, :spec)
        expect(template_spec).not_to have_key(:topologySpreadConstraints)
        expect(template_spec[:containers]).not_to be_empty
      end

      orchestrator.commit_phase(context)
    end

    it 'adds imagePullSecrets to ServiceAccount docs' do
      sa_doc = {
        apiVersion: 'v1',
        kind: 'ServiceAccount',
        metadata: { name: 'test-app' }
      }

      allow(fs_mock).to receive(:path_entries).with('/src/test-app/base').and_return([
                                                                                       '/src/test-app/base/serviceaccount.yaml'
                                                                                     ])
      allow(fs_mock).to receive(:read_yaml).with('/src/test-app/base/serviceaccount.yaml').and_return(sa_doc)

      orchestrator.act_phase(context)

      expect(fs_mock).to receive(:write_yaml).with('/dest/test-app/base/serviceaccount.yaml',
                                                          anything) do |_path, doc|
        expect(doc[:imagePullSecrets]).to eq([{ name: 'test-app-registry' }])
      end

      orchestrator.commit_phase(context)
    end

    it 'upgrades ExternalSecret apiVersion to the configured version' do
      es_doc = {
        apiVersion: 'external-secrets.io/v1beta1',
        kind: 'ExternalSecret',
        metadata: { name: 'test-secret' },
        spec: { data: [] }
      }

      allow(fs_mock).to receive(:path_entries).with('/src/test-app/base').and_return([
                                                                                       '/src/test-app/base/secrets.yaml'
                                                                                     ])
      allow(fs_mock).to receive(:read_yaml).with('/src/test-app/base/secrets.yaml').and_return(es_doc)

      orchestrator.act_phase(context)

      expect(fs_mock).to receive(:write_yaml).with('/dest/test-app/base/secrets.yaml', anything) do |_path, doc|
        expect(doc[:apiVersion]).to eq('external-secrets.io/v1')
        expect(doc[:spec][:secretStoreRef][:name]).to eq('onepassword-backend')
      end

      orchestrator.commit_phase(context)
    end

    it 'adds registry-pull-secret.yaml to Kustomization resources' do
      kustomization_doc = {
        apiVersion: 'kustomize.config.k8s.io/v1beta1',
        kind: 'Kustomization',
        resources: ['deployment.yaml', 'service.yaml', 'serviceaccount.yaml']
      }

      allow(fs_mock).to receive(:path_entries).with('/src/test-app/base').and_return([
                                                                                       '/src/test-app/base/kustomization.yaml'
                                                                                     ])
      allow(fs_mock).to receive(:read_yaml).with('/src/test-app/base/kustomization.yaml').and_return(kustomization_doc)

      orchestrator.act_phase(context)

      expect(fs_mock).to receive(:write_yaml).with('/dest/test-app/base/kustomization.yaml',
                                                          anything) do |_path, docs|
        expect(docs[:resources]).to include('registry-pull-secret.yaml')
      end

      orchestrator.commit_phase(context)
    end
  end
end
