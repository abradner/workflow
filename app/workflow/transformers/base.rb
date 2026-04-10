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

      def error_prefix
        "#{self.class.name.split('::').last} Error: "
      end

      protected

      # Abstracts the differences between iterating over a direct patch stream Array or a single parsed Hash struct.
      def mutate_yaml(docs, &block)
        return docs unless docs.is_a?(Hash) || docs.is_a?(Array)

        is_single = docs.is_a?(Hash)
        docs_array = is_single ? [docs] : docs

        docs_array.map!(&block)
        is_single ? docs_array.first : docs_array
      end
    end
  end
end
