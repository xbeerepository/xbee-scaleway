package main

import (
	scaleway "github.com/iodasolutions/scaleway"
	"github.com/iodasolutions/xbee-common/provider"
)

func main() {
	var p scaleway.Provider
	var a scaleway.Admin
	provider.Execute(p, a)
}
