# Budget

**TODO: Add description**

## Setup

  * `docker-compose build web`
  * `docker-compose run --rm web mix deps.get`
  * `docker-compose run --rm web sh -c "mix deps.compile  && mix compile"`
  * `docker-compose run --rm web sh -c "mix ecto.create && mix ecto.migrate"`

or use the script `./build`

Then do `docker-compose up`
