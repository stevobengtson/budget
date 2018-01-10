defmodule User do
  @moduledoc """
  User interface for creation, authentication, etc.
  """

  import Ecto.Query, only: [from: 2]
  alias User.{Repo, User}

  @doc """
  Find a user by email address.
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
  """
  def create(email, password) do
    changeset = User.registration_changeset(%User{}, %{email: email, password: password})
    Repo.insert!(changeset)
  end
end
