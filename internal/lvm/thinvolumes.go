package lvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// execCommand allows mocking of the exec.Command function.
var execCommand = exec.Command

// ThinPoolIface ...
type ThinPoolInterface interface {
	// EnsureVolumeIsPresent ensures that a volume is present in the thin pool.
	EnsureVolumeIsPresent(volumeName, size string) error
	// ensure_absent ensures that a volume is absent in the thin pool.
	EnsureVolumeIsAbsent(volumeName string) error
}

// ThinPool represents a thin pool with its volumes.
type ThinPool struct {
	sync.Mutex
	LongName string
	Name     string
	VGName	 string
	Volumes  []Volume
}

// ByteSize is a custom type to hold the size in bytes as int64
type ByteSize int64

// UnmarshalJSON is a custom unmarshaler for ByteSize
func (bs *ByteSize) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	s = strings.TrimSuffix(s, "B")
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*bs = ByteSize(val)
	return nil
}

func (bs *ByteSize) AsString() string {
	return strconv.FormatInt(int64(*bs), 10) + "B"
}

// Volume represents a logical volume.
type Volume struct {
	LVName          string   `json:"lv_name"`
	LVAttr          string   `json:"lv_attr"`
	LVSize          ByteSize `json:"lv_size"`
	DataPercent     string   `json:"data_percent"`
	MetadataPercent string   `json:"metadata_percent"`
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
			Name: parts[3],
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
	volume := tp.findVolume(volumeName)
	if volume == nil {
		// Create the volume
		return tp.createVolume(volumeName, size)
	}
	// If the size is smaller than the configured size, do nothnig since there is no practical way to shrink it.
	if size != 0 && volume.LVSize < size {
		return tp.extendVolume(volumeName, size)
	}
	// If we haven't returned yet the volume exists.
	return nil
}

// ensure_absent ensures that a volume is absent in the thin pool.
func (tp *ThinPool) EnsureVolumeIsAbsent(volumeName string) error {
	tp.Lock()
	defer tp.Unlock()

	// Check if the volume exists.
	volume := tp.findVolume(volumeName)
	if volume == nil {
		return nil // Volume already absent, so it's idempotent.
	}

	// Remove the volume.
	return tp.removeVolume(volumeName)
}

// findVolume checks if a volume exists in the thin pool.
func (tp *ThinPool) findVolume(volumeName string) *Volume {
	tp.refreshVolumes()
	for _, v := range tp.Volumes {
		if v.LVName == volumeName {
			return &v
		}
	}
	return nil
}

// refreshVolumes refreshes the list of volumes from the thin pool.
func (tp *ThinPool) refreshVolumes() {
	output, err := execCommand("lvs", "--units", "B", "--select", "pool_lv="+tp.Name+"&&vg_name="+tp.VGName, "--reportformat", "json").Output()
	if err != nil {
		// Handle error.
		return
	}

	var result struct {
		Report []struct {
			LV []Volume `json:"lv"`
		} `json:"report"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		// Handle JSON parsing error.
		log.Fatalf("Error parsing JSON from lvs command: %s", err)
	}

	tp.Volumes = result.Report[0].LV
}

// CreateVolume creates a new volume in the thin pool with the specified size.
func (tp *ThinPool) createVolume(volumeName string, size ByteSize) error {
	cmd := execCommand("lvcreate", "-V", size.AsString(), "-T", tp.LongName, "-n", volumeName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create volume: %v, output: %s", err, string(output))
	}
	cmd = execCommand("/usr/sbin/mkfs.xfs", tp.LongName)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create filesystem: %v, output: %s", err, string(output))
	}
	tp.refreshVolumes()
	return nil
}

func (tp *ThinPool) extendVolume(volumeName string, size ByteSize) error {
	cmd := execCommand("/usr/sbin/lvextend", "--size", size.AsString(), "--resizefs", tp.LongName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to extend volume: %v, output: %s", err, string(output))
	}
	return nil
}

// RemoveVolume removes a volume from the thin pool.
func (tp *ThinPool) removeVolume(volumeName string) error {
	cmd := execCommand("lvremove", "-f", "/dev/"+tp.VGName+"/"+volumeName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove volume: %v, output: %s", err, string(output))
	}
	tp.refreshVolumes()
	return nil
}

// isThinPool checks if the specified pool name is a valid thin pool.
func isThinPool(poolName string) bool {
	// Execute the lvs command to check that the volume exsits and get its attrs.
	cmd := execCommand("lvs", poolName, "--noheadings", "-o", "lv_attr")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// Check if the specified pool is a thin pool.
	return strings.HasPrefix(strings.TrimSpace(string(output)), "t")
}
