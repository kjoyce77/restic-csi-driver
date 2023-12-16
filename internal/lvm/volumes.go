// TODO: refresh volume state before idempotent operations

package lvm

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

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
	VGName          string   `json:"vg_name"`
	LVName          string   `json:"lv_name"`
	LVAttr          string   `json:"lv_attr"`
	LVSize          ByteSize `json:"lv_size"`
	Mounted         bool
	Target          string
}

// CreateVolume creates a new volume in the thin pool with the specified size.
func CreateThinVolume(volumeName string, thinPoolLongName string, size ByteSize) (*Volume, error) {
	cmd := execCommand("/usr/sbin/lvcreate", "-V", size.AsString(), "-T", thinPoolLongName, "-n", volumeName)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to create volume: %v, output: %s", err, string(output))
	}
	cmd = execCommand("/usr/sbin/mkfs.xfs", thinPoolLongName)
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to create filesystem: %v, output: %s", err, string(output))
	}
	return &Volume{
		VGName: strings.Split(thinPoolLongName, "/")[1],
		LVName: volumeName,
		LVSize: size,
	}, nil
}

// DeviceName returns the device name of the volume, ie '/dev/vg0/test-volume'.
func (volume *Volume) DeviceName() string {
	return fmt.Sprintf("/dev/%s/%s", volume.VGName, volume.LVName)
}

// CreateVolumeSnapshot creates a new snapshot volume with the specified size.
func (volume *Volume) CreateSnapshot(snapshotName string, size ByteSize) (*Volume, error) {
	cmd := execCommand("/usr/sbin/lvcreate", "--snapshot", "--name", snapshotName, "-L", size.AsString(), volume.DeviceName())
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to create volume snapshot: %v, output: %s", err, string(output))
	}
	return &Volume{
		VGName: volume.VGName,
		LVName: snapshotName,
		LVSize: size,
	}, nil
}

func (volume *Volume) Extend(size ByteSize) error {
	cmd := execCommand("/usr/sbin/lvextend", "--size", size.AsString(), "--resizefs", volume.DeviceName())
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to extend volume: %v, output: %s", err, string(output))
	}
	return nil
}

// RemoveVolume removes a volume from the thin pool.
func (volume *Volume) Remove(volumeName string) error {
	cmd := execCommand("/usr/sbin/lvremove", "-f", volume.DeviceName())
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to remove volume: %v, output: %s", err, string(output))
	}
	return nil
}
func (volume *Volume) EnsureVolumeIsMounted(mountPath string) error {
	if volume.Mounted {
		return nil
	}
	return volume.mountVolume(mountPath)
}

func (volume *Volume) UpdateMountStatus() error {
	output, err := execCommand("/usr/bin/findmnt", "-n", "-o", "TARGET", "--source", volume.DeviceName()).Output()
	if exitError, ok := err.(*exec.ExitError); ok {
		if exitError.ExitCode() == 1 {
			// Exit code 1 means the volume is not mounted
			volume.Mounted = false
			volume.Target = ""
		} else {
			// Handle other non-zero exit codes if necessary
			fmt.Printf("Error executing findmnt for volume %s: %s\n", volume.LVName, err)
			return err
		}
	} else if err != nil {
		// Handle other errors
		fmt.Printf("Error executing findmnt for volume %s: %s\n", volume.LVName, err)
		return err
	} else {
		// No error, command executed successfully
		volume.Mounted = true
		trimmedTarget := strings.TrimSpace(string(output))
		volume.Target = trimmedTarget
	}
	return nil
}

func (volume *Volume) mountVolume(mountPoint string) error {
	// Create the mount point directory if it doesn't exist
	if err := MkdirAll(mountPoint, 0755); err != nil {
		return fmt.Errorf("error creating mount point directory: %w", err)
	}

	// Execute the mount command
	cmd := execCommand("/usr/bin/mount", volume.DeviceName(), mountPoint)
	if output, err := cmd.Output(); err != nil {
		return fmt.Errorf("mount error: %s, output: %s", err, output)
	}

	volume.Mounted = true
	volume.Target = mountPoint
	return nil
}

func (volume *Volume) EnsureVolumeIsUnmounted() error {
	if !volume.Mounted {
		return nil
	}
	return volume.unmountVolume()
}

func (volume *Volume) unmountVolume() error {
	// Execute the umount command
	cmd := execCommand("/usr/bin/umount", volume.DeviceName())
	if output, err := cmd.Output(); err != nil {
		return fmt.Errorf("umount error: %s, output: %s", err, output)
	}

	volume.Mounted = false
	volume.Target = ""
	return nil
}
