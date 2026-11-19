package main

import "os"

// go build -ldflags "-X main.Version=x.y.z"
var (
	// Name is the name of the compiled software.
	Name string
	// Version is the version of the compiled software.
	Version string
	Build   string
	ID, _   = os.Hostname()
)
