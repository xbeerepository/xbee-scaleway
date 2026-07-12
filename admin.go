package scaleway

import (
	"context"
	"sync"

	instance "github.com/scaleway/scaleway-sdk-go/api/instance/v1"
	"github.com/scaleway/scaleway-sdk-go/scw"

	"github.com/iodasolutions/xbee-common/cmd"
	"github.com/iodasolutions/xbee-common/log2"
	"github.com/iodasolutions/xbee-common/util"
)

type Admin struct{}

func (pv Admin) DestroyVolumes(names []string) *cmd.XbeeError {
	log2.Infof("asked to destroy volumes %v ...", names)
	ctx := context.Background()
	regions, err := pv.regionsFromVolumes(ctx)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var allExistingNames []string
	for _, r := range regions {
		existingNames := r.existingVolumeNamesFor(names)
		allExistingNames = append(allExistingNames, existingNames...)
		for _, volName := range existingNames {
			wg.Add(1)
			go func(r *Region2, volName string) {
				defer wg.Done()
				vol := r.ScwVolumes[volName]
				if err := r.clients.instance.DeleteVolume(&instance.DeleteVolumeRequest{Zone: r.Zone, VolumeID: vol.ID}, scw.WithContext(ctx)); err != nil {
					log2.Errorf("could not remove volume %s:\n%v", volName, err)
				} else {
					log2.Infof("successfully destroyed volume %s", volName)
				}
			}(r, volName)
		}
	}
	wg.Wait()
	namesSet := util.SetFromStringSlice(names).Remove(allExistingNames...)
	if namesSet.Size() > 0 {
		for _, aName := range namesSet.Slice() {
			log2.Warnf("volume %s already do not exist", aName)
		}
	}
	return nil
}

func (pv Admin) DestroyImages(hashes []string) *cmd.XbeeError {
	log2.Infof("asked to destroy images %v ...", hashes)
	ctx := context.Background()
	regions, err := zonesForHosts(ctx)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var allExistingHashes []string
	for _, r := range regions {
		for _, hash := range hashes {
			imageID, ok := r.ImageMap[hash]
			if !ok {
				continue
			}
			allExistingHashes = append(allExistingHashes, hash)
			wg.Add(1)
			go func(r *Region2, hash string, imageID string) {
				defer wg.Done()
				if err := r.clients.instance.DeleteImage(&instance.DeleteImageRequest{Zone: r.Zone, ImageID: imageID}, scw.WithContext(ctx)); err != nil {
					log2.Errorf("could not remove image %s (%s):\n%v", hash, imageID, err)
				} else {
					log2.Infof("successfully destroyed image %s (%s)", hash, imageID)
				}
			}(r, hash, imageID)
		}
	}
	wg.Wait()
	hashesSet := util.SetFromStringSlice(hashes).Remove(allExistingHashes...)
	if hashesSet.Size() > 0 {
		for _, hash := range hashesSet.Slice() {
			log2.Warnf("image %s already do not exist", hash)
		}
	}
	return nil
}

func (pv Admin) regionsFromVolumes(ctx context.Context) (map[string]*Region2, *cmd.XbeeError) {
	volumes, err := VolumesFrom()
	if err != nil {
		return nil, err
	}
	var channels []<-chan *response
	for zoneName, volumesForZone := range volumes {
		channels = append(channels, newRegion(ctx, zoneName, nil, volumesForZone))
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
