package main

import "fmt"

const (
	Major = 0
	Minor = 0
	Patch = 5
)

var Version = fmt.Sprintf("%d.%d.%d", Major, Minor, Patch)
