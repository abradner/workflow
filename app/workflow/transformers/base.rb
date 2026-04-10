# frozen_string_literal: true

module Workflow
  module Transformers
    # Base interface for Transformers in the pure ETL pipeline structure.
    class Base
      # Mutates the workspace in place in an isolated execution step and returns the modified workspace.
      # @param workspace [Models::AppManifestWorkspace] The memory structure tracking files
      # @return [Models::AppManifestWorkspace] The modified workspace object
      def call(workspace)
        raise NotImplementedError, "#{self.class} must implement #call"
      end
    end
  end
end
