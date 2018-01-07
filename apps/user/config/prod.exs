use Mix.Config

# Do not print debug messages in production
config :logger, level: :info

config :user, User.Repo,
  pool_size: 15

# Finally import the config/prod.secret.exs
# which should be versioned separately.
import_config "prod.secret.exs"
