defmodule Web.UserController do
  use Web.Web, :controller

  def index(conn, _params) do
    users = User.all()
    render(conn, "index.html", users: users)
  end
end