// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package gpu

import (
	"bufio"
	"bytes"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sylabs/singularity/internal/pkg/sylog"
)

// gpuContainerCli runs `nvidia-container-cli list` and returns list of
// libraries, ipcs and binaries for proper NVIDIA work. This may return duplicates!
func gpuContainerCli() ([]string, error) {
	nvidiaCLIPath, err := exec.LookPath("nvidia-container-cli")
	if err != nil {
		return nil, fmt.Errorf("could not find nvidia-container-cli: %v", err)
	}

	var out bytes.Buffer
	cmd := exec.Command(nvidiaCLIPath, "list", "--binaries", "--libraries")
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("could not execute nvidia-container-cli list: %v", err)
	}

	var libs []string
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.Contains(line, ".so") {
			// this is a library -> add library name
			lib := filepath.Base(line)
			libs = append(libs, lib)

			// also add library without .xxx.xx suffix
			bareLib := strings.SplitAfter(lib, ".so")[0]
			libs = append(libs, bareLib)
		} else {
			// this is a binary -> need full path
			libs = append(libs, line)
		}
	}
	return libs, nil
}

// gpuliblist returns libraries listed in [nv,rocm]liblist.conf which is located in gpuDir.
func gpuliblist(gpuDir string, filename string) ([]string, error) {
	gpuliblistPath := filepath.Join(gpuDir, filename)
	file, err := os.Open(gpuliblistPath)
	if err != nil {
		return nil, fmt.Errorf("could not open %s: %v", gpuliblistPath, err)
	}
	defer file.Close()

	var libs []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && line[0] != '#' {
			libs = append(libs, line)
		}
	}
	return libs, nil
}

// Paths returns list of gpu libraries and binaries that should
// be added to mounted into container if it needs GPUs.
func Paths(gpuDir string, envPath string, gpu GpuCfg) ([]string, []string, error) {
	if envPath != "" {
		oldPath := os.Getenv("PATH")
		os.Setenv("PATH", envPath)
		defer os.Setenv("PATH", oldPath)
	}

	var gpuFiles []string
	gpuFiles, err := gpuContainerCli()
	if err != nil {
		sylog.Verbosef("gpuContainerCli returned: %v", err)
		sylog.Verbosef("Falling back to %s", gpu.File)

		gpuFiles, err = gpuliblist(gpuDir)
		if err != nil {
			return nil, nil, fmt.Errorf("could not read %s: %v", gpu.File, err)
		}
	}

	// walk through the ldconfig output and add entries which contain the filenames
	// returned by gpuContainerCli OR the gpuliblist file contents
	out, err := exec.Command("ldconfig", "-p").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("could not execute ldconfig: %v", err)
	}

	// sample ldconfig -p output:
	// libnvidia-ml.so.1 (libc6,x86-64) => /usr/lib64/nvidia/libnvidia-ml.so.1
	r, err := regexp.Compile(`(?m)^(.*)\s*\(.*\)\s*=>\s*(.*)$`)
	if err != nil {
		return nil, nil, fmt.Errorf("could not compile ldconfig regexp: %v", err)
	}

	// get elf machine to match correct libraries during ldconfig lookup
	self, err := elf.Open("/proc/self/exe")
	if err != nil {
		return nil, nil, fmt.Errorf("could not open /proc/self/exe: %v", err)
	}

	machine := self.Machine
	if err := self.Close(); err != nil {
		sylog.Warningf("Could not close ELF: %v", err)
	}

	// store library name with associated path
	ldCache := make(map[string]string)
	for _, match := range r.FindAllSubmatch(out, -1) {
		if match != nil {
			// libName is the "libnvidia-ml.so.1" (from the above example)
			// libPath is the "/usr/lib64/nvidia/libnvidia-ml.so.1" (from the above example)
			libName := strings.TrimSpace(string(match[1]))
			libPath := strings.TrimSpace(string(match[2]))
			ldCache[libPath] = libName
		}
	}

	// trach binaries/libraries to eliminate duplicates
	bins := make(map[string]struct{})
	libs := make(map[string]struct{})

	var libraries []string
	var binaries []string
	for _, gpuFile := range gpuFiles {
		// if the file contains a ".so", treat it as a library
		if strings.Contains(gpuFile, ".so") {
			for libPath, libName := range ldCache {
				if !strings.HasPrefix(libName, gpuFile) {
					continue
				}
				if _, ok := libs[libName]; !ok {
					elib, err := elf.Open(libPath)
					if err != nil {
						sylog.Debugf("ignore library %s: %s", libName, err)
						continue
					}

					if elib.Machine == machine {
						libs[libName] = struct{}{}
						libraries = append(libraries, libPath)
					}

					if err := elib.Close(); err != nil {
						sylog.Warningf("Could not close ELIB: %v", err)
					}
				}
			}
		} else {
			// treat the file as a binary file - add it to the bind list
			// no need to check the ldconfig output
			binary, err := exec.LookPath(gpuFile)
			if err != nil {
				continue
			}
			if _, ok := bins[binary]; !ok {
				bins[binary] = struct{}{}
				binaries = append(binaries, binary)
			}
		}
	}

	return libraries, binaries, nil
}
