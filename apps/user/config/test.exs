use Mix.Config

config :user, User.Repo,
  database: "user_test",
  pool: Ecto.Adapters.SQL.Sandbox

# Print only warnings and errors during test
config :logger, level: :warn
