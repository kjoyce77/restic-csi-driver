package lvm

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// execCommand allows mocking of the exec.Command function.
var execCommand = exec.Command
var MkdirAll = os.MkdirAll

// ThinPoolIface ...
type ThinPoolInterface interface {
	// EnsureVolumeIsPresent ensures that a volume is present in the thin pool.
	EnsureVolumeIsPresent(volumeName, size string) error
	// ensure_absent ensures that a volume is absent in the thin pool.
	EnsureVolumeIsAbsent(volumeName string) error
	// GetVolume gets a volume from the thin pool.
	GetVolume(volumeName string) (*Volume, error)
}

// ThinPool represents a thin pool with its volumes.
type ThinPool struct {
	sync.Mutex
	LongName string
	Name     string
	VGName   string
	Volumes  []Volume
}

// NewThinPool creates a new ThinPool instance with the os path to the thin pool.
// For example: "/dev/mapper/vg0-thinpool"
func NewThinPool(longName string) (*ThinPool, error) {
	// Check if the thin pool exists. If not, return an error.
	success := isThinPool(longName)
	if !success {
		return nil, errors.New("thin pool does not exist")
	}
	// Split the string by "/"
	parts := strings.Split(longName, "/")

	// Assuming the structure is always "/dev/VGName/Name"
	// and checking if the slice has at least 3 elements
	var thinPool ThinPool
	if len(parts) >= 3 {
		thinPool = ThinPool{LongName: longName,
			Name:   parts[3],
			VGName: parts[2],
		}
	} else {
		return nil, errors.New("invalid thin pool path")
	}

	thinPool.refreshVolumes()
	return &thinPool, nil
}

// EnsurePresent ensures that a volume is present in the thin pool.
func (tp *ThinPool) EnsureVolumeIsPresent(volumeName string, size ByteSize) error {
	tp.Lock()
	defer tp.Unlock()

	// Check if the volume already exists.
	volume := tp.GetVolume(volumeName)
	if volume == nil {
		// Create the volume
		_, err := CreateThinVolume(volumeName, tp.LongName, size)
		if err == nil {
			tp.refreshVolumes()
		}
		return err
	}
	// If the size is smaller than the configured size, do nothnig since there is no practical way to shrink it.
	// If the size is bigger than the configured size, extend the volume.
	if size != 0 && volume.LVSize < size {
		err := volume.Extend(size)
		if err == nil {
            tp.refreshVolumes()
        }
		return err
	}
	// If we haven't returned yet the volume exists and requires no changes.
	return nil
}

// ensure_absent ensures that a volume is absent in the thin pool.
func (tp *ThinPool) EnsureVolumeIsAbsent(volumeName string) error {
	tp.Lock()
	defer tp.Unlock()

	// Check if the volume exists.
	volume := tp.GetVolume(volumeName)
	if volume == nil {
		return nil // Volume already absent, cool beans.
	}

	// Remove the volume.
	result := volume.Remove(volumeName)
	if result == nil {
		tp.refreshVolumes()
	}
	return result
}

// GetVolume checks if a volume exists in the thin pool.
func (tp *ThinPool) GetVolume(volumeName string) *Volume {
	tp.refreshVolumes()
	for _, v := range tp.Volumes {
		if v.LVName == volumeName {
			return &v
		}
	}
	return nil
}

// refreshVolumes refreshes the list of volumes from the thin pool.
func (tp *ThinPool) refreshVolumes() error {
	output, err := execCommand("/usr/sbin/lvs", "--units", "B", "--select", "pool_lv="+tp.Name+"&&vg_name="+tp.VGName, "--reportformat", "json").Output()
	if err != nil {
		// Handle error.
		return err
	}

	var result struct {
		Report []struct {
			LV []Volume `json:"lv"`
		} `json:"report"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		// Handle JSON parsing error.
		log.Fatalf("Error parsing JSON from /usr/sbin/lvs command: %s", err)
		return err
	}

	tp.Volumes = result.Report[0].LV
	for i := range tp.Volumes {
		tp.Volumes[i].UpdateMountStatus()
	}

	return nil
}

// isThinPool checks if the specified pool name is a valid thin pool.
func isThinPool(poolName string) bool {
	// Execute the /usr/sbin/lvs command to check that the volume exsits and get its attrs.
	cmd := execCommand("/usr/sbin/lvs", poolName, "--noheadings", "-o", "lv_attr")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// Check if the specified pool is a thin pool.
	return strings.HasPrefix(strings.TrimSpace(string(output)), "t")
}

// root@bouba:/# findmnt -n -o TARGET --source /dev/vg0/test-volume
// root@bouba:/# echo $?
// 1

// root@bouba:/# mount /dev/vg0/test-volume /mnt/test
// root@bouba:/# findmnt -n -o TARGET --source /dev/vg0/test-volume
// /mnt/test
// root@bouba:/# echo $?
// 0
// root@bouba:
