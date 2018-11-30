// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE.md file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package rocmutils

import (
	"bufio"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/sylabs/singularity/internal/pkg/sylog"
)

// generate bind list using rocm-container-cli
func rocmContainerCli() ([]string, error) {
	var strArray []string

	// fixme

	return strArray, nil
}

// generate bind list using contents of rocmliblist.conf
func rocmLiblist(abspath string) ([]string, error) {
	var strArray []string

	// grab the entries in rocmliblist.conf file
	file, err := os.Open(abspath + "/rocmliblist.conf")
	if err != nil {
		return nil, fmt.Errorf("%v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "#") && line != "" {
			strArray = append(strArray, line)
		}
	}
	return strArray, nil
}

// GetRocmPath returns a string array consisting of filepaths of ROCm
// related files to be added to the BindPaths
func GetRocmPath(abspath string, envPath string) (libraries []string, binaries []string, err error) {
	var strArray []string

	// replace PATH with custom environment variable
	// and restore it when returning
	if envPath != "" {
		oldPath := os.Getenv("PATH")
		os.Setenv("PATH", envPath)

		defer os.Setenv("PATH", oldPath)
	}

	// use rocm-container-cli if present
	strArray, err = rocmContainerCli()
	if err != nil {
		sylog.Verbosef("rocmContainercli returned: %v", err)
		sylog.Verbosef("Falling back to rocmliblist.conf")

		// rocm-container-cli not present or errored out
		// fallback is to use rocmliblist.conf
		strArray, err = rocmLiblist(abspath)
		if err != nil {
			sylog.Warningf("rocmLiblist returned: %v", err)
			return
		}
	}

	// walk thru the ldconfig output and add entries which contain the filenames
	// returned by rocm-container-cli OR the rocmliblist.conf file contents
	cmd := exec.Command("ldconfig", "-p")
	out, err := cmd.Output()
	if err != nil {
		sylog.Warningf("ldconfig execution error: %v", err)
		return
	}

	// store library name with associated path
	ldCache := make(map[string]string)

	// store binaries/libraries path
	bins := make(map[string]string)
	libs := make(map[string]string)

	// sample ldconfig -p output:
	//  libMIOpen.so.1 (libc6,x86-64) => /opt/rocm/lib/libMIOpen.so.1 
	r, err := regexp.Compile(`(?m)^(.*)\s*\(.*\)\s*=>\s*(.*)$`)
	if err != nil {
		return
	}

	// get elf machine to match correct libraries during ldconfig lookup
	self, err := elf.Open("/proc/self/exe")
	if err != nil {
		return
	}

	machine := self.Machine
	self.Close()

	for _, match := range r.FindAllSubmatch(out, -1) {
		if match != nil {
			// libName is the "libMIOpen.so.1" (from the above example)
			// libPath is the "/opt/rocm/lib/libMIOpen.so.1" (from the above example)
			libName := strings.TrimSpace(string(match[1]))
			libPath := strings.TrimSpace(string(match[2]))

			ldCache[libPath] = libName
		}
	}

	for _, rocmFileName := range strArray {
		// if the file contains a ".so", treat it as a library
		if strings.Contains(rocmFileName, ".so") {
			for libPath, lib := range ldCache {
				if strings.HasPrefix(lib, rocmFileName) {
					if _, ok := libs[lib]; !ok {
						elib, err := elf.Open(libPath)
						if err != nil {
							sylog.Debugf("ignore library %s: %s", lib, err)
							continue
						}

						if elib.Machine == machine {
							libs[lib] = libPath
							libraries = append(libraries, libPath)
						}

						elib.Close()
					}
				}
			}
		} else {
			// treat the file as a binary file - add it to the bind list
			// no need to check the ldconfig output
			binary, err := exec.LookPath(rocmFileName)
			if err != nil {
				continue
			}
			if _, ok := bins[binary]; !ok {
				bins[binary] = binary
				binaries = append(binaries, binary)
			}
		}
	}

	return
}
