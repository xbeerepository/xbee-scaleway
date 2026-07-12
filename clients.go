package scaleway

import (
	"github.com/iodasolutions/xbee-common/cmd"
	instance "github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

type scwClients struct {
	instance *instance.API
}

func getClients() (*scwClients, *cmd.XbeeError) {
	client, err := scw.NewClient(scw.WithEnv())
	if err != nil {
		return nil, cmd.Error("cannot create Scaleway client : %v", err)
	}
	return &scwClients{instance: instance.NewAPI(client)}, nil
}
