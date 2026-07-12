package scaleway

import (
	"context"
	"fmt"

	"github.com/iodasolutions/xbee-common/cmd"
	"github.com/iodasolutions/xbee-common/log2"
	"github.com/iodasolutions/xbee-common/util"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

type UpInstanceGeneratorResponse struct {
	InError              bool
	Name                 string
	InitiallyNotExisting bool
	InitiallyDown        bool
	InitiallyUp          bool
}

type OperationStatus struct {
	Host    *ProviderHost
	InError bool
}

type response struct {
	r   *Region2
	err error
}

func sendError(ctx context.Context, ch chan *response, err error) {
	select {
	case <-ctx.Done():
	case ch <- &response{err: err}:
	}
}

func projectIdFor(hosts map[string]*ProviderHost, volumes map[string]*Volume) string {
	for _, h := range hosts {
		if h.Specification.ProjectId != "" {
			return h.Specification.ProjectId
		}
	}
	for _, v := range volumes {
		if v.Specification.ProjectId != "" {
			return v.Specification.ProjectId
		}
	}
	return ""
}

func newRegion(ctx context.Context, zoneName string, hosts map[string]*ProviderHost, volumes map[string]*Volume) <-chan *response {
	ch := make(chan *response)
	go func() {
		defer close(ch)
		clients, cerr := getClients()
		if cerr != nil {
			sendError(ctx, ch, fmt.Errorf("cannot create scaleway clients : %v", cerr))
			return
		}
		projectId := projectIdFor(hosts, volumes)
		if projectId == "" {
			sendError(ctx, ch, fmt.Errorf("no projectId configured for zone %s", zoneName))
			return
		}
		zone, err := scw.ParseZone(zoneName)
		if err != nil {
			sendError(ctx, ch, fmt.Errorf("invalid Scaleway zone %s : %v", zoneName, err))
			return
		}
		r := &Region2{
			Name:      zoneName,
			Zone:      zone,
			ProjectId: projectId,
			clients:   clients,
			Hosts:     hosts,
			Volumes:   volumes,
			ImageMap:  map[string]string{},
		}
		if err := r.fillInstances(ctx); err != nil {
			sendError(ctx, ch, fmt.Errorf("an unexpected error occured when listing instances for zone %s : %v", zoneName, err))
			return
		}
		if err := r.fillVolumes(ctx); err != nil {
			sendError(ctx, ch, fmt.Errorf("an unexpected error occured when listing volumes for zone %s : %v", zoneName, err))
			return
		}
		if err := r.ensureImages(ctx); err != nil {
			sendError(ctx, ch, fmt.Errorf("an unexpected error occured when listing images for zone %s : %v", zoneName, err))
			return
		}
		select {
		case <-ctx.Done():
		case ch <- &response{r: r}:
		}
	}()
	return ch
}

func zonesForHosts(ctx context.Context) (map[string]*Region2, *cmd.XbeeError) {
	hosts, err := HostsByZone()
	if err != nil {
		return nil, err
	}
	volumes, err := VolumesFrom()
	if err != nil {
		return nil, err
	}
	var channels []<-chan *response
	for zoneName, hostsForZone := range hosts {
		channels = append(channels, newRegion(ctx, zoneName, hostsForZone, volumes[zoneName]))
	}
	ch := util.Multiplex(ctx, channels...)
	result := map[string]*Region2{}
	for resp := range ch {
		if resp.err != nil {
			log2.Errorf("%v", resp.err)
		} else {
			result[resp.r.Name] = resp.r
		}
	}
	return result, nil
}
