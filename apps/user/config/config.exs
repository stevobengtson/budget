# This file is responsible for configuring your application
# and its dependencies with the aid of the Mix.Config module.
use Mix.Config

config :user, ecto_repos: [User.Repo]

config :user, User.Repo,
  adapter: Ecto.Adapters.Postgres,
  database: "user",
  username: System.get_env("DB_USER_USERNAME") || "${DB_USER_USERNAME}",
  password: System.get_env("DB_USER_PASSWORD") || "${DB_USER_PASSWORD}",
  hostname: System.get_env("DB_USER_HOST") || "${DB_USER_HOST}"

config :bcrypt_elixir, :log_rounds, 4

# Configures Elixir's Logger
config :logger, :console,
  format: "$time $metadata[$level] $message\n",
  metadata: [:request_id]

# This configuration is loaded before any dependency and is restricted
# to this project. If another project depends on this project, this
# file won't be loaded nor affect the parent project. For this reason,
# if you want to provide default values for your application for
# 3rd-party users, it should be done in your "mix.exs" file.

# You can configure your application as:
#
#     config :user, key: :value
#
# and access this configuration in your application as:
#
#     Application.get_env(:user, :key)
#
# You can also configure a 3rd-party app:
#
#     config :logger, level: :info
#

# It is also possible to import configuration files, relative to this
# directory. For example, you can emulate configuration per environment
# by uncommenting the line below and defining dev.exs, test.exs and such.
# Configuration from the imported file will override the ones defined
# here (which is why it is important to import them last).
#
import_config "#{Mix.env}.exs"
