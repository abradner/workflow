#!/usr/bin/env ruby
# frozen_string_literal: true

require 'optparse'
require_relative 'config/config'
require_relative 'app/utils/colorized_logger'
require_relative 'app/workflow/execution_context'
require_relative 'app/workflow/runner'
require_relative 'app/workflow/orchestrators/sync_workloads'
require_relative 'app/workflow/orchestrators/generate_argocd'
require_relative 'app/workflow/orchestrators/sync_1password'
require_relative 'app/workflow/orchestrators/render_talos'

options = {}
OptionParser.new do |opts|
  opts.banner = 'Usage: workflow.rb [command] [options]'

  opts.on('--dry-run', 'Run without making state changes') do |v|
    options[:dry_run] = v
  end

  opts.on('-h', '--help', 'Prints this help') do
    puts opts
    exit
  end
end.parse!(ARGV)

command = ARGV.shift

config = Config.new
logger = Utils::ColorizedLogger.new($stdout)
context = Workflow::ExecutionContext.new(config: config, logger: logger, options: options)

orchestrators = case command
                when 'sync'
                  [Workflow::Orchestrators::SyncWorkloads.new(config: config)]
                when 'setup-argo'
                  [Workflow::Orchestrators::GenerateArgocd.new(config: config)]
                when 'sync-1p'
                  [Workflow::Orchestrators::Sync1Password.new(config: config)]
                when 'render-talos'
                  [Workflow::Orchestrators::RenderTalos.new(config: config)]
                else
                  logger.fatal "Unknown command: #{command}. Use 'sync', 'setup-argo', 'sync-1p', or 'render-talos'."
                  exit 1
                end

runner = Workflow::Runner.new(context, orchestrators: orchestrators)
success = runner.run

exit(success ? 0 : 1)
