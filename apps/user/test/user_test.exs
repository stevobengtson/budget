defmodule UserTest do
  use ExUnit.Case

  test "Creates a user" do
    user = User.create("test_created@example.com", "testPass1234")
    refute user.password_hash == nil
  end
end
