package gpu

import (
	"fmt"
	"path/filepath"
)

// Devices return list of allnon-GPU nvidia devices present on host. If withGPU
// is true all GPUs are included in the resulting list as well.
func NvDevices(withGPU bool) ([]string, error) {
	nvidiaGlob := "/dev/nvidia*"
	if !withGPU {
		nvidiaGlob = "/dev/nvidia[^0-9]*"
	}
	devs, err := filepath.Glob(nvidiaGlob)
	if err != nil {
		return nil, fmt.Errorf("could not list nvidia devices: %v", err)
	}
	return devs, nil
}

// Devices return list of allnon-GPU rocm devices present on host. If withGPU
// is true all GPUs are included in the resulting list as well.
func RocmDevices(withGPU bool) ([]string, error) {
	rocmGlob := "/dev/dri/card*"
	if !withGPU {
		rocmGlob = "/dev/dri/card[^0-9]*"
	}
	devs, err := filepath.Glob(rocmGlob)
	if err != nil {
		return nil, fmt.Errorf("could not list rocm devices: %v", err)
	}
	return devs, nil
}
