package scaleway

import (
	"context"
	"sync"

	"github.com/iodasolutions/xbee-common/cmd"
	"github.com/iodasolutions/xbee-common/constants"
	"github.com/iodasolutions/xbee-common/log2"
	"github.com/iodasolutions/xbee-common/provider"
	"github.com/iodasolutions/xbee-common/util"
)

type Provider struct{}

func (pv Provider) Up() ([]*provider.InstanceInfo, *cmd.XbeeError) {
	ctx := context.Background()
	regions, err := zonesForHosts(ctx)
	if err != nil {
		return nil, err
	}
	var channels []<-chan *UpInstanceGeneratorResponse
	for _, r := range regions {
		hosts, volumes := r.Existing()
		if len(hosts) > 0 {
			channels = append(channels, r.Filter(hosts, volumes).StartInstancesGenerator(ctx))
		}
		hosts, volumes = r.NotExisting()
		if len(hosts) > 0 {
			channels = append(channels, r.Filter(hosts, volumes).CreateInstancesGenerator(ctx))
		}
	}
	ch := util.Multiplex(ctx, channels...)
	var inError bool
	for upStatus := range ch {
		if upStatus.InError {
			inError = true
		}
	}
	if inError {
		return nil, cmd.Error("Scaleway up command failed, provider cannot continue")
	}
	return pv.InstanceInfos()
}

func (pv Provider) Delete() *cmd.XbeeError {
	ctx := context.Background()
	regions, err := zonesForHosts(ctx)
	if err != nil {
		return err
	}
	for _, r := range regions {
		r.destroyInstances(ctx)
	}
	return nil
}

func (pv Provider) Down() *cmd.XbeeError {
	ctx := context.Background()
	regions, err := zonesForHosts(ctx)
	if err != nil {
		return err
	}
	for _, r := range regions {
		for name, server := range r.Instances {
			if xbeeState(server.State) == constants.State.Up {
				if err := r.serverAction(ctx, server.ID, "poweroff"); err != nil {
					log2.Errorf("cannot stop instance %s : %v", name, err)
				} else {
					log2.Infof("Stopping instance %s", name)
				}
			}
		}
	}
	log2.Infof("all instances are now down")
	return nil
}

func (pv Provider) InstanceInfos() ([]*provider.InstanceInfo, *cmd.XbeeError) {
	ctx := context.Background()
	regions, err := zonesForHosts(ctx)
	if err != nil {
		return nil, err
	}
	infos := map[string]*provider.InstanceInfo{}
	for _, r := range regions {
		for name, info := range r.instanceInfos(ctx) {
			infos[name] = info
		}
	}
	var result []*provider.InstanceInfo
	for _, info := range infos {
		result = append(result, info)
	}
	return result, nil
}

func (pv Provider) Image() *cmd.XbeeError {
	ctx := context.Background()
	regions, err := zonesForHosts(ctx)
	if err != nil {
		return err
	}
	var channels []<-chan *OperationStatus
	for _, r := range regions {
		channels = append(channels, r.PackInstancesGenerator(ctx))
	}
	ch := util.Multiplex(ctx, channels...)
	var inError bool
	var wg sync.WaitGroup
	for status := range ch {
		if status.InError {
			inError = true
		}
		wg.Add(1)
		go func(status *OperationStatus) {
			defer wg.Done()
			if status.InError {
				log2.Errorf("Creation of image %s failed", status.Host.EffectivePackName())
			} else {
				log2.Infof("Creation of image %s succeeded", status.Host.EffectivePackName())
			}
		}(status)
	}
	wg.Wait()
	if inError {
		return cmd.Error("Scaleway image creation operation failed")
	}
	return nil
}
