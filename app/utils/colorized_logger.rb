# frozen_string_literal: true

require 'logger'
require 'colorize'

module Utils
  # Colorized logger with Rails-style formatting
  # Supports DEBUG, INFO, WARN, ERROR levels with colored output
  class ColorizedLogger
    attr_reader :level

    LEVELS = {
      debug: Logger::DEBUG,
      info: Logger::INFO,
      warn: Logger::WARN,
      error: Logger::ERROR,
      fatal: Logger::FATAL
    }.freeze

    def initialize(output = $stdout, level: :info)
      @logger = Logger.new(output)
      @logger.level = LEVELS[level] || Logger::INFO
      @logger.formatter = method(:formatter)
      @level = level
    end

    def level=(new_level)
      @level = new_level
      @logger.level = LEVELS[new_level] || Logger::INFO
    end

    # Standard log-level methods

    def debug(message = nil, &)
      @logger.debug(message, &)
    end

    def info(message = nil, &)
      @logger.info(message, &)
    end

    def warn(message = nil, &)
      @logger.warn(message, &)
    end

    def error(message = nil, exception = nil, &)
      msg = format_message(message, exception)
      @logger.error(msg, &)
      return unless exception && level.eql?(:debug)

      @logger.debug(exception.backtrace.join("\n"))
    end

    def fatal(message = nil, exception = nil, &)
      msg = format_message(message, exception)
      @logger.fatal(msg, &)
      if exception || level.eql?(:debug)
        trace = exception ? exception.backtrace : caller
        @logger.debug(trace.join("\n")) if trace
      end
      exit 1
    end

    # Utility Helpers

    def section(message)
      @logger.info ''
      @logger.info '=' * [80, message.length].max
      @logger.info message
      @logger.info '=' * [80, message.length].max
      @logger.info ''
    end

    def subsection(heading, subheading)
      @logger.info ''
      @logger.info heading
      @logger.info '-' * [40, heading.length, subheading.length].max
      @logger.info subheading
      @logger.info ''
    end

    private

    def format_message(message, exception)
      return message unless exception

      "#{message} (#{exception.class}: #{exception.message})"
    end

    def formatter(severity, datetime, _progname, msg)
      timestamp = datetime.strftime('%Y-%m-%d %H:%M:%S')
      formatted_severity = format_severity(severity)
      "#{timestamp} #{formatted_severity} #{msg}\n"
    end

    def format_severity(severity)
      case severity
      when 'DEBUG'
        '[DEBUG]'.light_black
      when 'INFO'
        '[INFO] '.cyan
      when 'WARN'
        '[WARN] '.yellow
      when 'ERROR'
        '[ERROR]'.red
      when 'FATAL'
        '[FATAL]'.light_red
      else
        "[#{severity}]"
      end
    end
  end
end
