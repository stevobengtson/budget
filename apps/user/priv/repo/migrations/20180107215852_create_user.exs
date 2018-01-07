defmodule User.Repo.Migrations.CreateUser do
  use Ecto.Migration

  def change do
    create table(:users) do
      add(:email, :string, unique: true)
      add(:encrypted_password, :string, null: false)

      timestamps()
    end

    create(unique_index(:users, [:email], name: :unique_emails))
  end
end
