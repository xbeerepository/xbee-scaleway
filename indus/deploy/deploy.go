package main

import (
	"context"
	"log"

	"github.com/iodasolutions/xbee-common/indus"
)

func main() {
	ctx := context.TODO()
	if err := indus.BuildAndDeploy(ctx, "main", "xbee-scaleway"); err != nil {
		log.Fatal(err)
	}
}
