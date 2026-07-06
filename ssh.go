package main

// SSH represents SSH creds
type SSH struct {
	User         string `toml:"user"`
	Password     string `toml:"password"`
	Port         int    `toml:"port"`
	Identifyfile string `toml:"identityfile"`
}
