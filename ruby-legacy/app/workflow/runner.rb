# frozen_string_literal: true

require_relative 'execution_context'
require_relative 'orchestrator'
require_relative 'hydrate/discovery'
require_relative 'hydrate/saml_credentials'

module Workflow
  # Runs the workflow by hydrating state to satisfy orchestrator predicates,
  # then calling orchestrator phase methods.
  class Runner
    # Mapping of predicates to hydration actions
    # Order matters: earlier predicates should be satisfied first
    HYDRATION_ACTIONS = {
      discovery_completed: ->(ctx) { Hydrate::Discovery.call(ctx) },
      saml_credentials_extracted: ->(ctx) { Hydrate::SamlCredentials.call(ctx) }
    }.freeze

    # @param context [ExecutionContext] shared workflow state
    # @param orchestrators [Array<Orchestrator>] active orchestrators
    def initialize(context, orchestrators:)
      @context = context
      @orchestrators = orchestrators
      @logger = context.logger
    end

    # Execute the workflow
    # @return [Boolean] true if successful
    def run
      @logger.section 'Starting Workflow'

      # 1. Collect all predicates needed by active orchestrators
      required_predicates = collect_required_predicates
      @logger.debug "Required predicates: #{required_predicates.join(', ')}"

      # 2. Hydrate: run actions to satisfy predicates
      hydrate(required_predicates)

      # 3. Act phase: each orchestrator processes user interaction
      run_phase(:act_phase, 'Act')

      # 4. Commit phase: each orchestrator writes outputs (skip if dry-run)
      if @context.dry_run?
        @logger.info '[DRY RUN] Skipping commit phase'
      else
        run_phase(:commit_phase, 'Commit')
      end

      @logger.info ''
      @logger.info 'Workflow complete'
      true
    end

    private

    def collect_required_predicates
      @orchestrators.flat_map(&:needs).uniq
    end

    def hydrate(predicates)
      predicates.each do |predicate|
        predicate_method = "#{predicate}?"

        if @context.respond_to?(predicate_method) && @context.send(predicate_method)
          @logger.debug "Predicate :#{predicate} already satisfied"
          next
        end

        action = HYDRATION_ACTIONS[predicate]
        raise "Unknown predicate: #{predicate}" unless action

        @logger.subsection "Hydrating: #{predicate}...", ''
        action.call(@context)
      end
    end

    def run_phase(phase_method, phase_name)
      @orchestrators.each do |orchestrator|
        next unless orchestrator.respond_to?(phase_method)

        orchestrator_name = orchestrator.class.name.split('::').last
        @logger.subsection "#{phase_name}: #{orchestrator_name}", ''

        orchestrator.send(phase_method, @context)
      end
    end
  end
end
