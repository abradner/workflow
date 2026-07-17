FROM public.ecr.aws/docker/library/ruby:4.0

RUN bundle config --global frozen 1

WORKDIR /app

COPY ./Gemfile Gemfile.lock ./
RUN bundle install

# No COPY . . needed - source will be mounted at runtime

CMD ["bundle", "exec", "ruby", "workflow.rb", "--help"]
