package version

// ServerVersion is the controller/server release version.
//
// Source builds default to "dev". Release builds should stamp this with the
// GitHub release tag version, for example:
//
//	go build -ldflags "-X insylus/internal/version.ServerVersion=0.1.14"
var ServerVersion = "dev"

const AgentVersion = "0.1.14"
