defmodule User do
  @moduledoc """
  User interface for creation, authentication, etc.
  """

  import Ecto.Query, only: [from: 2]
  alias User.{Repo, User}

  @doc """
  Find a user by email address.

  ## Examples

      iex> User.findByEmail('test@example.com')
      nil

  """
  def findByEmail(email) do
    query =
      from(
        u in User,
        where: u.email == ^email,
        select: { u.email, u.id }
      )
    Repo.one!(query)
  end

  def all() do
    Repo.all(User)
  end

  @doc """
  Create a new user.

  ## Examples

      iex> User.create('test@example.com', 'testPass1234', 'testPass1234')
      {:ok, User}

  """
  def create(email, password, password_confirmation) do
    changeset = User.changeset(%User{}, %{email: email, password: password, password_confirmation: password_confirmation})
    Repo.insert(changeset)
  end
end
