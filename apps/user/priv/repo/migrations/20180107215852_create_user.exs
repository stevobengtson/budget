defmodule User.Repo.Migrations.CreateUser do
  use Ecto.Migration

  def change do
    create table(:users, primary_key: false) do
      add(:id, :uuid, primary_key: true)
      add(:email, :string, unique: true, null: false)
      add(:password_hash, :string, null: false)

      timestamps()
    end

    create(unique_index(:users, [:email], name: :unique_emails))
  end
end
