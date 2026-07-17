# frozen_string_literal: true

require 'fileutils'
require 'yaml'

module Services
  # FilesystemService abstracts raw File IO operations into use-case-agnostic behaviors
  # for our orchestrators so they don't get bogged down in File system specifics or YAML streams.
  class FilesystemService
    def list_directories(base_path, pattern)
      raise "Source directory #{base_path} does not exist" unless directory_exists?(base_path)

      Dir.glob(File.join(base_path, pattern)).select do |dir|
        File.directory?(dir)
      end
    end

    def path_entries(path)
      Dir.glob(File.join(path, '*'))
    end

    def base_filename(path)
      File.basename(path)
    end

    def extension(path)
      File.extname(path).downcase
    end

    def directory_exists?(path)
      Dir.exist?(path)
    end

    def create_directory(path)
      FileUtils.mkdir_p(path)
    end

    def read_file(path)
      File.read(path)
    end

    def write_file(path, content)
      File.write(path, content)
    end

    def yaml?(path)
      ['.yaml', '.yml'].include?(extension(path))
    end

    def read_yaml(path)
      YAML.safe_load_file(path, symbolize_names: true)
    end

    def write_yaml(path, doc)
      if doc.is_a?(Array) && doc.any? { |d| d.is_a?(Hash) && d.key?(:kind) }
        # Treat as Kubernetes multi-document stream
        yaml_content = doc.map do |d| 
          d.to_yaml(line_width: -1, stringify_names: true).sub(/\A---\n/, '')
        end.join("---\n")
      else
        # Treat as scalar configuration hash or JSON array patch stream
        yaml_content = doc.to_yaml(line_width: -1, stringify_names: true)
        yaml_content = yaml_content.sub(/\A---\n/, '')
      end
      
      File.write(path, yaml_content)
    end
  end
end
