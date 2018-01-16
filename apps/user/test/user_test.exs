defmodule UserTest do
  use ExUnit.Case
  doctest User

  test "Creates a user" do
    case User.create('test_created@example.com', 'testPass1234') do
      {:ok, _}    -> assert true
      {:error, _} -> assert false
    end
  end
end
