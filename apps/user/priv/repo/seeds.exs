# Script for populating the database. You can run it as:
#
#     mix run priv/repo/seeds.exs
#
# Inside the script, you can read and write to any of your
# repositories directly:
#
#     User.Repo.insert!(%User.User{})
#
# We recommend using the bang functions (`insert!`, `update!`
# and so on) as they will fail if something goes wrong.

User.Repo.insert!(%User.User{email: "user@budget.com", password: "test1234"})
User.Repo.insert!(%User.User{email: "admin@budget.com", password: "test12345"})
