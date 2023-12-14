package lvm

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"os"
	"os/exec"
	"strings"
	"testing"
)

var volumeExists bool = true
var volumeFormatted bool = false
// var volumeSize int64 = 1024 * 1024 * 1024

// fakeExecCommand allows mocking of the exec.Command function.
func fakeExecCommand(command string, args ...string) *exec.Cmd {
	if command == "lvremove" {
        if volumeExists {
            volumeExists = false
			volumeFormatted = false
        } else {
            // Trigger an error
            panic("Error: Attempted to remove a non-existing volume.")
        }
    }
	if command == "lvcreate" {
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
	// Run TestHelperProcess with the specified command and arguments after the -- flag.
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "GO_HELPER_PROCESS_VOLUME_PRESENT="+fmt.Sprintf("%v", volumeExists)}
	return cmd
}

func TestNewThinPool(t *testing.T) {
	execCommand = fakeExecCommand

	defer func() { execCommand = exec.Command }()

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
		LVAttr:          "Vwi-a-tz--",
		LVSize:          1024 * 1024 * 1024,
		DataPercent:     "0.00",
		MetadataPercent: ""}

	// Assert that the Volume struct is created correctly.
	assert.Equal(t, thinPool.Volumes[0], test_volume_fixture)
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
	assert.Nil(t, thinPool.EnsureVolumeIsPresent("test-volume", 1024 * 1024 * 1024))
	assert.Len(t, thinPool.Volumes, 1)
	assert.True(t, volumeFormatted)
	// Make it bigger
	assert.Nil(t, thinPool.EnsureVolumeIsPresent("test-volume", 1024 * 1024 * 1024 * 2))
    assert.Len(t, thinPool.Volumes, 1)
    assert.True(t, volumeFormatted)
	assert.Equal(t, thinPool.Volumes[0].LVSize, 1024 * 1024 * 1024 * 2)
}

// TODO: Try to mock removeVolume and createVolume -- need to assert
// called when needed and not called when not needed.

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
	mockCommands := map[string]mockCommandResult{
		sliceToStringKey([]string{"lvs", "/dev/vg0/existing_thin_pool", "--noheadings", "-o", "lv_attr"}): {
			stdout:   "  twi-aotz--\n",
			stderr:   "A warning was given, but it doesn't matter.\n",
			exitCode: 0,
		},
		sliceToStringKey([]string{"lvcreate", "-V", "1073741824B", "-T", "/dev/vg0/existing_thin_pool", "-n", "test-volume"}): {
			stdout: "Volume successfully created.\n",
			stderr: "A warning was given, but it doesn't matter.\n",
			exitCode: 0,
		},
		sliceToStringKey([]string{"lvremove", "-f", "/dev/vg0/test-volume"}): {
			stdout: "Volume successfully removed.\n",
            stderr: "A warning was given, but it doesn't matter.\n",
            exitCode: 0,
		},
		sliceToStringKey([]string{"/usr/sbin/mkfs.xfs", "/dev/vg0/existing_thin_pool"}): {
			stdout: "Filesystem successfully formatted.\n",
            stderr: "A warning was given, but it doesn't matter.\n",
            exitCode: 0,
		},
	}

	if os.Getenv(("GO_HELPER_PROCESS_VOLUME_PRESENT")) == "true" {
		mockCommands[sliceToStringKey([]string{"lvs", "--units", "B", "--select", "pool_lv=existing_thin_pool&&vg_name=vg0", "--reportformat", "json"})] = mockCommandResult{
			stdout: `  
    {
        "report": [
            {
                "lv": [
                    {"lv_name":"test-volume", "vg_name":"vg0", "lv_attr":"Vwi-a-tz--", "lv_size":"1073741824B", "pool_lv":"existing_thin_pool", "origin":"", "data_percent":"0.00", "metadata_percent":"", "move_pv":"", "mirror_log":"", "copy_percent":"", "convert_lv":""}
                ]
            }
        ]
    }
            `,
			stderr:   "A warning was given, but it doesn't matter.\n",
			exitCode: 0,
		}
	} else if os.Getenv(("GO_HELPER_PROCESS_VOLUME_PRESENT")) == "false" {
		mockCommands[sliceToStringKey([]string{"lvs", "--units", "B", "--select", "pool_lv=existing_thin_pool&&vg_name=vg0", "--reportformat", "json"})] = mockCommandResult{
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
			stderr:   "File descriptor 34 (/dev/ptmx) leaked on lvs invocation. Parent PID 168001: -bash\nFile descriptor 40 (/dev/ptmx) leaked on lvs invocation. Parent PID 168001: -bash",
			exitCode: 0,
		}
	}

	defaultCommandResult := mockCommandResult{
		stdout:   "",
		stderr:   "Command not recognized.",
		exitCode: 1, // Non-zero exit code typically indicates an error or unknown command
	}

	commandResult, ok := mockCommands[sliceToStringKey(argv)]
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
