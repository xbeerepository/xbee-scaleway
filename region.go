package scaleway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iodasolutions/xbee-common/cmd"
	"github.com/iodasolutions/xbee-common/constants"
	"github.com/iodasolutions/xbee-common/log2"
	"github.com/iodasolutions/xbee-common/provider"
	"github.com/iodasolutions/xbee-common/util"
	instance "github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"
)

type Region2 struct {
	Name      string
	Zone      scw.Zone
	ProjectId string

	clients *scwClients

	Volumes map[string]*Volume
	Hosts   map[string]*ProviderHost

	Instances  map[string]*instance.Server
	ScwVolumes map[string]*instance.Volume
	ImageMap   map[string]string // pack/system hash -> image id
}

func (r *Region2) Filter(hosts map[string]*ProviderHost, volumes map[string]*Volume) *Region2 {
	return &Region2{
		Name:       r.Name,
		Zone:       r.Zone,
		ProjectId:  r.ProjectId,
		clients:    r.clients,
		Volumes:    volumes,
		Hosts:      hosts,
		Instances:  r.Instances,
		ScwVolumes: r.ScwVolumes,
		ImageMap:   r.ImageMap,
	}
}

func (r *Region2) HasInstance(name string) bool {
	_, ok := r.Instances[name]
	return ok
}

func (r *Region2) HasVolume(name string) bool {
	_, ok := r.ScwVolumes[name]
	return ok
}

func (r *Region2) NotExisting() (map[string]*ProviderHost, map[string]*Volume) {
	hosts := map[string]*ProviderHost{}
	volumes := map[string]*Volume{}
	for name, h := range r.Hosts {
		if !r.HasInstance(name) {
			hosts[name] = h
			for _, volName := range h.Volumes {
				volumes[volName] = r.Volumes[volName]
			}
		}
	}
	return hosts, volumes
}

func (r *Region2) Existing() (map[string]*ProviderHost, map[string]*Volume) {
	hosts := map[string]*ProviderHost{}
	volumes := map[string]*Volume{}
	for name, h := range r.Hosts {
		if r.HasInstance(name) {
			hosts[name] = h
			for _, volName := range h.Volumes {
				volumes[volName] = r.Volumes[volName]
			}
		}
	}
	return hosts, volumes
}

func (r *Region2) fillInstances(ctx context.Context) *cmd.XbeeError {
	r.Instances = map[string]*instance.Server{}
	if len(r.Hosts) == 0 {
		return nil
	}
	out, err := r.clients.instance.ListServers(&instance.ListServersRequest{
		Zone:    r.Zone,
		Project: &r.ProjectId,
		Tags:    []string{envTag()},
	}, scw.WithAllPages(), scw.WithContext(ctx))
	if err != nil {
		return cmd.Error("an error occured when listing instances in zone %s : %v", r.Name, err)
	}
	for _, server := range out.Servers {
		for _, tag := range server.Tags {
			if hostName, ok := hostNameFromTag(tag, r.Hosts); ok {
				r.Instances[hostName] = server
				break
			}
		}
	}
	return nil
}

func hostNameFromTag(tag string, hosts map[string]*ProviderHost) (string, bool) {
	for hostName := range hosts {
		if tag == nameTag(hostName) {
			return hostName, true
		}
	}
	return "", false
}

func (r *Region2) fillVolumes(ctx context.Context) *cmd.XbeeError {
	r.ScwVolumes = map[string]*instance.Volume{}
	if len(r.Volumes) == 0 {
		return nil
	}
	out, err := r.clients.instance.ListVolumes(&instance.ListVolumesRequest{
		Zone:    r.Zone,
		Project: &r.ProjectId,
		Tags:    []string{envTag()},
	}, scw.WithAllPages(), scw.WithContext(ctx))
	if err != nil {
		return cmd.Error("an error occured when listing volumes in zone %s : %v", r.Name, err)
	}
	for _, volume := range out.Volumes {
		for _, tag := range volume.Tags {
			if volName, ok := volumeNameFromTag(tag, r.Volumes); ok {
				r.ScwVolumes[volName] = volume
				log2.Infof("Found existing volume %s in zone %s", volName, r.Name)
				break
			}
		}
	}
	return nil
}

func volumeNameFromTag(tag string, volumes map[string]*Volume) (string, bool) {
	for volName := range volumes {
		if tag == nameTag(volName) {
			return volName, true
		}
	}
	return "", false
}

func xbeeState(state instance.ServerState) string {
	switch state {
	case instance.ServerStateRunning:
		return constants.State.Up
	case instance.ServerStateStopped, instance.ServerStateStoppedInPlace:
		return constants.State.Down
	case instance.ServerStateStopping:
		return constants.State.Stopping
	case instance.ServerStateStarting, instance.ServerStateLocked:
		return constants.State.Pending
	default:
		return constants.State.NotExisting
	}
}

func (r *Region2) StartInstancesGenerator(ctx context.Context) <-chan *UpInstanceGeneratorResponse {
	ch := make(chan *UpInstanceGeneratorResponse)
	go func() {
		defer close(ch)
		for name, h := range r.Hosts {
			server := r.Instances[name]
			switch server.State {
			case instance.ServerStateRunning:
				ch <- &UpInstanceGeneratorResponse{Name: name, InitiallyUp: true}
			case instance.ServerStateStopped, instance.ServerStateStoppedInPlace:
				if err := r.serverAction(ctx, server.ID, instance.ServerActionPoweron); err != nil {
					log2.Errorf("cannot start instance %s : %v", name, err)
					ch <- &UpInstanceGeneratorResponse{Name: name, InError: true}
					continue
				}
				ch <- &UpInstanceGeneratorResponse{Name: name, InitiallyDown: true}
			default:
				log2.Infof("instance %s in %s state, can not be started", h.Name, server.State)
				ch <- &UpInstanceGeneratorResponse{Name: name, InError: true}
			}
		}
	}()
	return ch
}

func (r *Region2) CreateInstancesGenerator(ctx context.Context) <-chan *UpInstanceGeneratorResponse {
	ch := make(chan *UpInstanceGeneratorResponse)
	go func() {
		defer close(ch)
		for _, h := range r.Hosts {
			if err := r.createOneInstance(ctx, h); err != nil {
				log2.Errorf("cannot create instance %s : %v", h.Name, err)
				ch <- &UpInstanceGeneratorResponse{Name: h.Name, InError: true}
			} else {
				ch <- &UpInstanceGeneratorResponse{Name: h.Name, InitiallyNotExisting: true}
			}
		}
	}()
	return ch
}

func (r *Region2) createOneInstance(ctx context.Context, h *ProviderHost) error {
	imageID, err := r.imageFor(h)
	if err != nil {
		return err
	}
	volumeType := instance.VolumeVolumeTypeSbsVolume
	if h.Specification.VolumeType != "" {
		volumeType = instance.VolumeVolumeType(h.Specification.VolumeType)
	}
	createReq := &instance.CreateServerRequest{
		Zone:              r.Zone,
		Name:              scwName(h.Name),
		Project:           &r.ProjectId,
		CommercialType:    h.Specification.CommercialType,
		Image:             &imageID,
		DynamicIPRequired: scw.BoolPtr(true),
		Tags:              tagsForResource(h.Name),
	}
	// A boot volume of type sbs_volume can only be created by cloning the
	// image's own snapshot: Scaleway rejects a freshly-sized "raw" SBS boot
	// volume ("cannot create a raw SBS volume, provide a volume id or an
	// image"). So for sbs_volume we omit Volumes entirely and let the API
	// derive the boot volume from the image, like the Scaleway console does.
	// l_ssd boot volumes, by contrast, are always created raw/blank and do
	// need an explicit size.
	if volumeType == instance.VolumeVolumeTypeLSSD {
		size := scw.Size(uint64(h.Specification.Size) * uint64(scw.GB))
		createReq.Volumes = map[string]*instance.VolumeServerTemplate{
			"0": {
				Boot:       scw.BoolPtr(true),
				Name:       scw.StringPtr(scwName(h.Name) + "-root"),
				Size:       &size,
				VolumeType: volumeType,
				Project:    &r.ProjectId,
			},
		}
	}
	serverOut, err := r.clients.instance.CreateServer(createReq, scw.WithContext(ctx))
	if err != nil {
		return err
	}
	if err := r.clients.instance.SetServerUserData(&instance.SetServerUserDataRequest{
		Zone:     r.Zone,
		ServerID: serverOut.Server.ID,
		Key:      "cloud-init",
		Content:  stringsReader("#!/bin/bash\n" + provider.AuthorizedKeyScript(h.User)),
	}, scw.WithContext(ctx)); err != nil {
		return err
	}
	if err := r.serverAction(ctx, serverOut.Server.ID, instance.ServerActionPoweron); err != nil {
		return err
	}
	server, err := r.clients.instance.WaitForServer(&instance.WaitForServerRequest{
		Zone:          r.Zone,
		ServerID:      serverOut.Server.ID,
		Timeout:       durationPtr(10 * time.Minute),
		RetryInterval: durationPtr(10 * time.Second),
	}, scw.WithContext(ctx))
	if err != nil {
		return err
	}
	r.Instances[h.Name] = server
	for _, volName := range h.Volumes {
		if !r.HasVolume(volName) {
			if err := r.createVolume(ctx, volName); err != nil {
				return err
			}
		}
		if err := r.attachVolume(ctx, h.Name, volName); err != nil {
			return err
		}
	}
	return nil
}

func stringsReader(s string) *strings.Reader {
	return strings.NewReader(s)
}

func durationPtr(d time.Duration) *time.Duration {
	return &d
}

func (r *Region2) imageFor(h *ProviderHost) (string, error) {
	if h.PackHash != "" {
		if imageID, ok := r.ImageMap[h.PackHash]; ok {
			return imageID, nil
		}
	}
	if imageID, ok := r.ImageMap[h.SystemHash]; ok {
		return imageID, nil
	}
	if h.Specification.Image == "" {
		return "", fmt.Errorf("no Scaleway image configured for host %s and no xbee-built image found", h.Name)
	}
	return h.Specification.Image, nil
}

func (r *Region2) createVolume(ctx context.Context, volName string) error {
	vol := r.Volumes[volName]
	volumeType := instance.VolumeVolumeTypeSbsVolume
	if vol.Specification.VolumeType != "" {
		volumeType = instance.VolumeVolumeType(vol.Specification.VolumeType)
	}
	size := scw.Size(uint64(vol.Size) * uint64(scw.GB))
	log2.Infof("Creating volume %s in zone %s (size=%dGiB, type=%s)", vol.Name, r.Name, vol.Size, volumeType)
	out, err := r.clients.instance.CreateVolume(&instance.CreateVolumeRequest{
		Zone:       r.Zone,
		Name:       scwName(volName),
		Project:    &r.ProjectId,
		Tags:       tagsForResource(volName),
		VolumeType: volumeType,
		Size:       &size,
	}, scw.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("cannot create volume %s : %v", volName, err)
	}
	created, err := r.clients.instance.WaitForVolume(&instance.WaitForVolumeRequest{
		Zone:          r.Zone,
		VolumeID:      out.Volume.ID,
		Timeout:       durationPtr(10 * time.Minute),
		RetryInterval: durationPtr(10 * time.Second),
	}, scw.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("cannot wait for volume %s : %v", volName, err)
	}
	r.ScwVolumes[volName] = created
	log2.Infof("Volume %s created in zone %s with id %s", vol.Name, r.Name, created.ID)
	return nil
}

func (r *Region2) attachVolume(ctx context.Context, hostName string, volName string) error {
	server := r.Instances[hostName]
	vol := r.ScwVolumes[volName]
	if vol.Server != nil && vol.Server.ID == server.ID {
		return nil
	}
	_, err := r.clients.instance.AttachServerVolume(&instance.AttachServerVolumeRequest{
		Zone:       r.Zone,
		ServerID:   server.ID,
		VolumeID:   vol.ID,
		Boot:       scw.BoolPtr(false),
		VolumeType: instance.AttachServerVolumeRequestVolumeType(vol.VolumeType),
	}, scw.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("cannot attach volume %s to instance %s : %v", volName, hostName, err)
	}
	log2.Infof("volume %s is attached to instance %s", volName, hostName)
	return nil
}

func (r *Region2) serverAction(ctx context.Context, serverID string, action instance.ServerAction) error {
	_, err := r.clients.instance.ServerAction(&instance.ServerActionRequest{
		Zone:     r.Zone,
		ServerID: serverID,
		Action:   action,
	}, scw.WithContext(ctx))
	return err
}

func (r *Region2) instanceInfos(_ context.Context) map[string]*provider.InstanceInfo {
	result := map[string]*provider.InstanceInfo{}
	for name, h := range r.Hosts {
		info := &provider.InstanceInfo{Name: name, State: constants.State.NotExisting, User: h.User}
		if server, ok := r.Instances[name]; ok {
			info.State = xbeeState(server.State)
			for _, ip := range server.PublicIPs {
				if ip != nil && ip.Address != nil {
					info.ExternalIp = ip.Address.String()
					info.Ip = info.ExternalIp
					info.SSHPort = "22"
					break
				}
			}
			_, info.PackIdExist = r.ImageMap[h.EffectiveHash()]
			_, info.SystemIdExist = r.ImageMap[h.SystemHash]
		}
		result[name] = info
	}
	return result
}

func (r *Region2) packIds() (result []string) {
	aSet := util.NewEmptyStringSet()
	for _, h := range r.Hosts {
		aSet.Add(h.EffectiveHash(), h.SystemHash)
	}
	return aSet.Slice()
}

func (r *Region2) ensureImages(ctx context.Context) *cmd.XbeeError {
	wanted := map[string]string{}
	for _, hash := range r.packIds() {
		wanted[idTag(hash)] = hash
	}
	out, err := r.clients.instance.ListImages(&instance.ListImagesRequest{
		Zone:    r.Zone,
		Project: &r.ProjectId,
	}, scw.WithAllPages(), scw.WithContext(ctx))
	if err != nil {
		return cmd.Error("an error occured when listing images for project %s : %v", r.ProjectId, err)
	}
	for _, image := range out.Images {
		for _, tag := range image.Tags {
			if original, ok := wanted[tag]; ok {
				r.ImageMap[original] = image.ID
			}
		}
	}
	return nil
}

func (r *Region2) PackInstancesGenerator(ctx context.Context) <-chan *OperationStatus {
	var channels []<-chan *OperationStatus
	for _, h := range r.Hosts {
		ch := make(chan *OperationStatus)
		channels = append(channels, ch)
		go func(h *ProviderHost) {
			defer close(ch)
			if err := r.packInstance(ctx, h); err != nil {
				log2.Errorf(err.Error())
				ch <- &OperationStatus{Host: h, InError: true}
			} else {
				ch <- &OperationStatus{Host: h, InError: false}
			}
		}(h)
	}
	return util.Multiplex(ctx, channels...)
}

func (r *Region2) packInstance(ctx context.Context, h *ProviderHost) error {
	server, ok := r.Instances[h.Name]
	if !ok {
		return fmt.Errorf("no instance found for host %s in %s", h.Name, r.Name)
	}
	if len(server.Volumes) == 0 {
		return fmt.Errorf("cannot find root volume for host %s", h.Name)
	}
	var rootVolumeID string
	for _, vol := range server.Volumes {
		if vol.Boot {
			rootVolumeID = vol.ID
			break
		}
	}
	if rootVolumeID == "" {
		// SBS boot volumes derived from an image (no explicit Volumes given
		// at CreateServer time) come back with Boot=false from the API even
		// though they are the boot device: Scaleway only seems to set Boot
		// when it was explicitly requested. "0" is the conventional slot for
		// the boot volume in the per-server volume map, so fall back to it.
		if vol, ok := server.Volumes["0"]; ok {
			rootVolumeID = vol.ID
		}
	}
	if rootVolumeID == "" {
		return fmt.Errorf("cannot find boot volume for host %s", h.Name)
	}
	if err := r.serverAction(ctx, server.ID, instance.ServerActionPoweroff); err != nil {
		return fmt.Errorf("cannot poweroff instance %s before packing : %v", h.Name, err)
	}
	// ServerAction only fires the poweroff and returns; the server sits in a
	// transient "stopping" state for a few seconds, during which
	// CreateSnapshot is rejected ("resource ... is in a transient state").
	// Wait for it to actually reach "stopped" before snapshotting.
	if _, err := r.clients.instance.WaitForServer(&instance.WaitForServerRequest{
		Zone:          r.Zone,
		ServerID:      server.ID,
		Timeout:       durationPtr(5 * time.Minute),
		RetryInterval: durationPtr(5 * time.Second),
	}, scw.WithContext(ctx)); err != nil {
		return fmt.Errorf("cannot wait for instance %s to poweroff before packing : %v", h.Name, err)
	}
	snap, err := r.clients.instance.CreateSnapshot(&instance.CreateSnapshotRequest{
		Zone:     r.Zone,
		Name:     imageNameFor(h.EffectiveHash()),
		VolumeID: &rootVolumeID,
		Project:  &r.ProjectId,
		Tags:     &[]string{idTag(h.EffectiveHash())},
	}, scw.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("cannot create snapshot for host %s : %v", h.Name, err)
	}
	snapshot, err := r.clients.instance.WaitForSnapshot(&instance.WaitForSnapshotRequest{
		Zone:          r.Zone,
		SnapshotID:    snap.Snapshot.ID,
		Timeout:       durationPtr(30 * time.Minute),
		RetryInterval: durationPtr(15 * time.Second),
	}, scw.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("cannot wait for snapshot for host %s : %v", h.Name, err)
	}
	img, err := r.clients.instance.CreateImage(&instance.CreateImageRequest{
		Zone:       r.Zone,
		Name:       imageNameFor(h.EffectiveHash()),
		RootVolume: snapshot.ID,
		Arch:       scalewayArchFor(h),
		Project:    &r.ProjectId,
		Tags:       []string{idTag(h.EffectiveHash())},
	}, scw.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("cannot create image for host %s : %v", h.Name, err)
	}
	if _, err := r.clients.instance.WaitForImage(&instance.WaitForImageRequest{
		Zone:          r.Zone,
		ImageID:       img.Image.ID,
		Timeout:       durationPtr(30 * time.Minute),
		RetryInterval: durationPtr(15 * time.Second),
	}, scw.WithContext(ctx)); err != nil {
		return fmt.Errorf("cannot wait for image for host %s : %v", h.Name, err)
	}
	r.ImageMap[h.EffectiveHash()] = img.Image.ID
	return nil
}

func scalewayArchFor(h *ProviderHost) instance.Arch {
	if h.Specification.Arch != "" {
		return instance.Arch(h.Specification.Arch)
	}
	switch h.OsArch {
	case "linux_arm64":
		return instance.ArchArm64
	default:
		return instance.ArchX86_64
	}
}

func (r *Region2) destroyInstances(ctx context.Context) {
	for name, server := range r.Instances {
		if err := r.clients.instance.DeleteServer(&instance.DeleteServerRequest{Zone: r.Zone, ServerID: server.ID}, scw.WithContext(ctx)); err != nil {
			log2.Errorf("cannot delete instance %s : %v", name, err)
		} else {
			log2.Infof("instance %s deleted", name)
		}
	}
}

func (r *Region2) existingVolumeNamesFor(names []string) (result []string) {
	for _, name := range names {
		if r.HasVolume(name) {
			result = append(result, name)
		}
	}
	return
}
