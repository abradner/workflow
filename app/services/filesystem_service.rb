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

    def file_exists?(path)
      File.exist?(path)
    end

    def create_directory(path)
      FileUtils.mkdir_p(path)
    end

    def copy_directory(src, dest)
      FileUtils.cp_r(src, dest)
    end

    def copy_file(src, dest)
      FileUtils.cp(src, dest)
    end

    def read_file(path)
      File.read(path)
    end

    def write_file(path, content)
      File.write(path, content)
    end

    def read_yaml_stream(path)
      YAML.safe_load_stream(File.read(path), symbolize_names: true).compact
    end

    def write_yaml_stream(path, docs)
      File.open(path, 'w') do |f|
        docs.each do |doc|
          f.puts '---'
          f.puts doc.to_yaml(line_width: -1).sub('---', '').strip
        end
      end
    end

    def read_yaml(path)
      YAML.safe_load_file(path, symbolize_names: true)
    end

    def write_yaml(path, doc)
      File.write(path, doc.to_yaml(line_width: -1).sub('---', '').strip)
    end
  end
end
