defmodule User.User do
  @moduledoc """
  User Model module.
  """

  use Ecto.Schema
  import Ecto.Changeset
  import Comeonin.Bcrypt, only: [hashpwsalt: 1]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  @derive {Phoenix.Param, key: :id}

  schema "users" do
    field(:email, :string)
    field(:password_hash, :string)
    field(:password, :string, virtual: true)

    timestamps()
  end

  def changeset(user, params \\ :empty) do
    user
    |> cast(params, ~w(email), required: true)
    |> validate_required(:email)
    |> unique_constraint(:email)
    |> validate_length(:email, min: 5)
    |> validate_format(:email, ~r/@/)
  end

  def registration_changeset(user, params \\ :empty) do
    user
    |> changeset(params)
    |> cast(params, ~w(password), [])
    |> validate_required(:password)
    |> validate_length(:password, min: 6)
    |> put_password_hash
  end

  defp put_password_hash(changeset) do
    case changeset do
      %Ecto.Changeset{valid?: true, changes: %{password: pass}} ->
        put_change(changeset, :password_hash, hashpwsalt(pass))
      _ ->
        changeset
    end
  end
end