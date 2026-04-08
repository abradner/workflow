# frozen_string_literal: true

module Workflow
  # Base class for orchestrators. Subclasses declare their predicate needs
  # and implement phase methods (act_phase, commit_phase).
  #
  # @example
  #   class MyOrchestrator < Workflow::Orchestrator
  #     needs :discovery_completed
  #     needs :builds_checked
  #
  #     def act_phase(context)
  #       # interactive prompts, triggers, waiting
  #     end
  #
  #     def commit_phase(context)
  #       # write files
  #     end
  #   end
  class Orchestrator
    attr_reader :config

    def initialize(config: nil)
      raise ArgumentError, ':config must be an instance of ::Config' unless config.is_a?(Config)

      @config = config
    end

    class << self
      # Declare a predicate that must be satisfied before this orchestrator runs
      # @param predicate [Symbol] predicate name (e.g., :discovery_completed)
      def needs(predicate)
        required_predicates << predicate
      end

      # @return [Array<Symbol>] predicates required by this orchestrator
      def required_predicates
        @required_predicates ||= []
      end
    end

    # Instance method to access class-level predicates
    # @return [Array<Symbol>]
    def needs
      self.class.required_predicates
    end

    # Override in subclass for user interaction phase
    # @param context [ExecutionContext]
    def act_phase(context)
      # Default: no action
    end

    # Override in subclass for file writing phase
    # @param context [ExecutionContext]
    def commit_phase(context)
      # Default: no action
    end
  end
end
