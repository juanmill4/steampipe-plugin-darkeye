connection "darkeye" {
  plugin = "juanmill4/darkeye"

  # Each product has its own optional API key. A table only needs the key of
  # its product: darkeye_haveibeenransom_record uses haveibeenransom_api_key and
  # darkeye_breachhouse_record uses breachhouse_api_key.

  # Have I Been Ransomed? API token. Every plan includes one; copy it from your
  # profile. See https://haveibeenransom.com/howapi
  # Can also be set with the HIBR_API_KEY environment variable.
  #haveibeenransom_api_key = "q8Zr3mT0vXk7LwN2pYc5HdJ9sBf4GuAe6RiK1oVnMx"

  # Breach House API token. The API needs an advanced or enterprise plan; the
  # token is in your customer panel. See https://breach.house/docs
  # Can also be set with the BREACHHOUSE_API_KEY environment variable.
  #breachhouse_api_key = "Wd4nT7yQm2Lk9XzR5vBc8HsJ3pGf6NaE1uKoYiZtMw"

  # Maximum number of Have I Been Ransomed? pages requested per query. Each page
  # is one request against your plan's daily budget. If not set, the plugin
  # follows pagination until the API reports no more results.
  # Must be 1 or greater.
  #max_pages = 5
}
