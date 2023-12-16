package lvm

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var volumeExists bool = true
var volumeFormatted bool = false
var volumeSize int64 = 1024 * 1024 * 1024
var volumeMounted bool = false


// fakeExecCommand allows mocking of the exec.Command function.
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	if command == "/usr/sbin/lvremove" {
		if volumeExists {
			volumeExists = false
			volumeFormatted = false
		} else {
			// Trigger an error
			panic("Error: Attempted to remove a non-existing volume.")
		}
	}
	if command == "/usr/sbin/lvcreate" {
		if !volumeExists {
			volumeExists = true
			volumeFormatted = false
		} else {
			// Trigger an error
			panic("Error: Attempted to create an existing volume.")
		}
	}
	if command == "/usr/sbin/mkfs.xfs" {
		if !volumeFormatted {
			volumeFormatted = true
		} else {
			// Trigger an error
			panic("Error: Attempted to format an existing volume.")
		}
		if !volumeExists {
			panic("Error: Attempted to format an non-existing volume.")
		}
	}
	if command == "/usr/sbin/lvextend" {
		if !volumeExists {
			panic("Error: Attempted to extend an non-existing volume.")
		}
		result, err := strconv.ParseInt(strings.TrimSuffix(args[1], "B"), 10, 64)
		if err != nil {
			panic(err)
		} else {
			volumeSize = result
		}
	}
	// Run TestHelperProcess with the specified command and arguments after the -- flag.
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{
		"GO_WANT_HELPER_PROCESS=1",
		"GO_HELPER_PROCESS_VOLUME_PRESENT=" + fmt.Sprintf("%v", volumeExists),
		"GO_HELPER_PROCESS_VOLUME_SIZE=" + strconv.FormatInt(volumeSize, 10) + "B",
		"GO_HELPER_PROCESS_VOLUME_MOUNTED=" + fmt.Sprintf("%v", volumeMounted),
	}

	// The volume state affects the output so change it after the command is 'run'.
	if command == "/usr/bin/mount" {
		volumeMounted = true
	}
	if command == "/usr/bin/umount" {
		volumeMounted = false
	}

	return cmd
}

func fakeMkdirAll(path string, perm os.FileMode) error {
	return nil
}

func TestNewThinPool(t *testing.T) {
	execCommand = fakeExecCommand
	MkdirAll = fakeMkdirAll

	defer func() { execCommand = exec.Command }()
	defer func() { MkdirAll = os.MkdirAll }()

	// Test for creating a new ThinPool struct with an existing thin pool.
	thinPool, err := NewThinPool("/dev/vg0/existing_thin_pool")
	if err != nil {
		t.Fatalf("NewThinPool failed, expected thin pool to exist: %v", err)
	}

	assert.Equal(t, thinPool.LongName, "/dev/vg0/existing_thin_pool")
	assert.Equal(t, thinPool.Name, "existing_thin_pool")

	if len(thinPool.Volumes) != 1 {
		t.Fatalf("NewThinPool failed, expected 1 volume, got %d", len(thinPool.Volumes))
	}

	test_volume_fixture := Volume{
		LVName:          "test-volume",
		VGName:          "vg0",
		LVAttr:          "Vwi-a-tz--",
		LVSize:          1024 * 1024 * 1024,
	}

	// Assert that the Volume struct is created correctly.
	assert.Equal(t, test_volume_fixture, thinPool.Volumes[0])
	assert.Len(t, thinPool.Volumes, 1)

	// Test for creating a new ThinPool struct with a non-existing thin pool.
	_, err = NewThinPool("/dev/vg0/non_existing_thin_pool")
	if err == nil {
		t.Errorf("NewThinPool succeeded, expected failure for non-existent thin pool")

	}
	// Test EnsureVolumeIsPresent / no change
	assert.Nil(t, thinPool.EnsureVolumeIsPresent("test-volume", 1024*1024*1024))

	// Assert that the Volume struct remains the same.
	assert.Equal(t, thinPool.Volumes[0], test_volume_fixture)
	assert.Len(t, thinPool.Volumes, 1)

	// Test EnsureVolumeIsAbsent / removes volume
	assert.Nil(t, thinPool.EnsureVolumeIsAbsent("test-volume"))
	assert.Len(t, thinPool.Volumes, 0)
	// Ensure no effect
	assert.Nil(t, thinPool.EnsureVolumeIsAbsent("test-volume"))
	assert.Len(t, thinPool.Volumes, 0)
	// Add it back
	assert.Nil(t, thinPool.EnsureVolumeIsPresent("test-volume", 1024*1024*1024))
	assert.Len(t, thinPool.Volumes, 1)
	assert.True(t, volumeFormatted)
	// Make it bigger
	assert.Nil(t, thinPool.EnsureVolumeIsPresent("test-volume", 1024*1024*1024*2))
	assert.Len(t, thinPool.Volumes, 1)
	assert.Equal(t, thinPool.Volumes[0].LVSize, ByteSize(1024*1024*1024*2))
	assert.Nil(t, thinPool.EnsureVolumeIsPresent("test-volume", 1024*1024*1024))
	assert.Equal(t, thinPool.Volumes[0].LVSize, ByteSize(1024*1024*1024*2))

	// Mount the volume
	volume := thinPool.GetVolume("test-volume")
	volume.EnsureVolumeIsMounted("/mnt/test")
	assert.Equal(t, "/mnt/test", volume.Target)
	assert.Equal(t, true, volume.Mounted)

	// Check volume retrieval reports correct state
	volume = thinPool.GetVolume("test-volume")
	assert.Equal(t, "/mnt/test", volume.Target)
	assert.Equal(t, true, volume.Mounted)

	// Check idempotency
	assert.Nil(t, volume.EnsureVolumeIsMounted("/mnt/test"))
	assert.Equal(t, "/mnt/test", volume.Target)
	assert.Equal(t, true, volume.Mounted)

	// Unmount the volume
	assert.Nil(t, volume.EnsureVolumeIsUnmounted())
	assert.Equal(t, "", volume.Target)
	assert.Equal(t, false, volume.Mounted)

	// Check volume retrieval does not alter state
	volume = thinPool.GetVolume("test-volume")
	assert.Equal(t, "", volume.Target)
	assert.Equal(t, false, volume.Mounted)

	// Check idempotency
	assert.Nil(t, volume.EnsureVolumeIsUnmounted())
	assert.Equal(t, "", volume.Target)
	assert.Equal(t, false, volume.Mounted)

	// Check create snapshot

	// reset volumeExists to prevent lvcreate failure
	volumeExists = false
	snapshotVolume, err := (volume.CreateSnapshot("test-snapshot", ByteSize(1024 * 1024)))
	assert.Nil(t, err)
	assert.Equal(t, snapshotVolume.LVName, "test-snapshot")
	assert.Equal(t, snapshotVolume.LVSize, ByteSize(1024 * 1024))
	assert.Equal(t, snapshotVolume.VGName, "vg0")
}


// TestHelperProcess simulates the behavior of the command being mocked.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	type mockCommandResult struct {
		stdout   string
		stderr   string
		exitCode int
	}

	argv := os.Args[3:]
	// mockCommands is a map of command names to their expected as an array with stdout and stderr.
	if argv[0] == "/usr/sbin/lvextend" && argv[1] == "--size" && argv[3] == "--resizefs" && argv[4] == "/dev/vg0/test-volume" {
		os.Exit(0)
	}

	// mockSuccessfulCommands is a map of commands to the expected output and exit code.
	// if an error is expected, the defaultCommandResult returns 1
	mockSuccessfulCommands := map[string]mockCommandResult{
		sliceToStringKey([]string{"/usr/sbin/lvs", "/dev/vg0/existing_thin_pool", "--noheadings", "-o", "lv_attr"}): {
			stdout:   "  twi-aotz--\n",
			stderr:   "A warning was given, but it doesn't matter.\n",
			exitCode: 0,
		},
		sliceToStringKey([]string{"/usr/sbin/lvcreate", "-V", "1073741824B", "-T", "/dev/vg0/existing_thin_pool", "-n", "test-volume"}): {
			stdout:   "Volume successfully created.\n",
			stderr:   "A warning was given, but it doesn't matter.\n",
			exitCode: 0,
		},
		sliceToStringKey([]string{"/usr/sbin/lvremove", "-f", "/dev/vg0/test-volume"}): {
			stdout:   "Volume successfully removed.\n",
			stderr:   "A warning was given, but it doesn't matter.\n",
			exitCode: 0,
		},
		sliceToStringKey([]string{"/usr/sbin/mkfs.xfs", "/dev/vg0/existing_thin_pool"}): {
			stdout:   "Filesystem successfully formatted.\n",
			stderr:   "A warning was given, but it doesn't matter.\n",
			exitCode: 0,
		},
		sliceToStringKey([]string{"/usr/sbin/lvcreate", "--snapshot", "--name", "test-snapshot", "-L", "1048576B", "/dev/vg0/test-volume"}): {
			stdout:   "Snapshot successfully created.\n",
            stderr:   "A warning was given, but it doesn't matter.\n",
            exitCode: 0,
        },
	}

	// Return exit codes depending on if the volume is mounted or not.
	if os.Getenv("GO_HELPER_PROCESS_VOLUME_MOUNTED") == "true" {
		mockSuccessfulCommands[sliceToStringKey(
			[]string{
				"/usr/bin/findmnt",
				"-n",
				"-o",
				"TARGET",
				"--source",
				"/dev/vg0/test-volume",
			})] = mockCommandResult{
			stdout:   "\n/mnt/test\n",
			stderr:   "",
			exitCode: 0,
		}
		mockSuccessfulCommands[sliceToStringKey(
			[]string{
				"/usr/bin/umount",
				"/dev/vg0/test-volume",
			})] = mockCommandResult{
			stdout:   "",
			stderr:   "",
			exitCode: 0,
		}
	}
	if os.Getenv("GO_HELPER_PROCESS_VOLUME_MOUNTED") == "false" {
		mockSuccessfulCommands[sliceToStringKey(
			[]string{
				"/usr/bin/mount",
				"/dev/vg0/test-volume",
				"/mnt/test",
			})] = mockCommandResult{
			stdout:   "",
			stderr:   "",
			exitCode: 0,
		}
	}

	// Return output depending on if the volume is in the pool
	if os.Getenv("GO_HELPER_PROCESS_VOLUME_PRESENT") == "true" {
		mockSuccessfulCommands[sliceToStringKey([]string{"/usr/sbin/lvs", "--units", "B", "--select", "pool_lv=existing_thin_pool&&vg_name=vg0", "--reportformat", "json"})] = mockCommandResult{
			stdout: `  
    {
        "report": [
            {
                "lv": [
                    {"lv_name":"test-volume", "vg_name":"vg0", "lv_attr":"Vwi-a-tz--", "lv_size":"` +
				os.Getenv("GO_HELPER_PROCESS_VOLUME_SIZE") +
				`", "pool_lv":"existing_thin_pool", "origin":"", "data_percent":"0.00", "metadata_percent":"", "move_pv":"", "mirror_log":"", "copy_percent":"", "convert_lv":""}
                ]
            }
        ]
    }
            `,
			stderr:   "A warning was given, but it doesn't matter.\n",
			exitCode: 0,
		}
	} else if os.Getenv(("GO_HELPER_PROCESS_VOLUME_PRESENT")) == "false" {
		mockSuccessfulCommands[sliceToStringKey([]string{"/usr/sbin/lvs", "--units", "B", "--select", "pool_lv=existing_thin_pool&&vg_name=vg0", "--reportformat", "json"})] = mockCommandResult{
			stdout: `
	{
		"report": [
			{
				"lv": [
				]
			}
		]
	}
			`,
			stderr:   "File descriptor 34 (/dev/ptmx) leaked on /usr/sbin/lvs invocation. Parent PID 168001: -bash\nFile descriptor 40 (/dev/ptmx) leaked on /usr/sbin/lvs invocation. Parent PID 168001: -bash",
			exitCode: 0,
		}
	}

	defaultCommandResult := mockCommandResult{
		stdout:   "",
		stderr:   "Command not mocked or returns an error.",
		exitCode: 1, // Non-zero exit code typically indicates an error or unknown command
	}

	commandResult, ok := mockSuccessfulCommands[sliceToStringKey(argv)]
	if !ok {
		commandResult = defaultCommandResult
	}
	fmt.Fprint(os.Stderr, commandResult.stderr)
	fmt.Fprint(os.Stdout, commandResult.stdout)
	os.Exit(commandResult.exitCode)
}

// sliceToStringKey converts a slice of strings to a string with the values separated by a pizza slice.
func sliceToStringKey(slice []string) string {
	return strings.Join(slice, "🍕")
}
