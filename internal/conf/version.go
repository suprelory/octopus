package conf

var (
	Version    = "dev"
	Commit     = "unknown"
	BuildTime  = "unknown"
	Author     = "suprelory"
	Repository = "suprelory/octopus"
)

// RepositoryURL is shared by the startup banner, web UI and binary updater.
func RepositoryURL() string {
	return "https://github.com/" + Repository
}

func ReleasesAPIURL() string {
	return "https://api.github.com/repos/" + Repository + "/releases"
}
