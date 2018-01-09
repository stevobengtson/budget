FROM elixir:1.5-alpine

# Need nodejs and nodejs for web asset compiling
# Need make and g++ for building bcrypt (comonin package)
# Need inotify-tools to live load the page
RUN apk add --update nodejs nodejs-npm make g++ inotify-tools && \
 mix local.hex --force && \
 mix archive.install --force https://github.com/phoenixframework/archives/raw/master/phoenix_new.ez && \
 mix local.rebar --force 

EXPOSE 4000

WORKDIR /app
