package main

import (
	"plus/app"
	"plus/cmd"
)

// version must be set from the contents of VERSION file by go build's
// -X main.version= option in the Makefile.
var version = "unknown"

// gitCommit will be the hash that the binary was built from
// and will be populated by the Makefile
var gitCommit = ""

const (
	usage = `High performance linux artifacts repository server`
)

func main() {
	cmd.Execute(app.Name, usage, version, gitCommit)
}
