/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"golang.org/x/net/context"
	pb "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
)

var runtimeCopyCommand = &cli.Command{
	Name:                   "cp",
	Usage:                  "Copy file to and from container runtime",
	ArgsUsage:              "",
	UseShortOptionHandling: true,
	Action: func(context *cli.Context) error {
		return Copy(context)
	},
}

func getContainerId(containerName string, runtimeClient pb.RuntimeServiceClient) (string, error) {
	request := &pb.ListContainersRequest{}
	r, err := runtimeClient.ListContainers(context.Background(), request)
	if err != nil {
		return "", err
	}
	containerList := getContainersList(r.Containers, listOptions{nameRegexp: containerName})
	logrus.Debugf("ListContainerResponse: %v", containerList)

	if len(containerList) > 1 {
		logrus.Infof("containers %v", containerList)
		return "", fmt.Errorf("could not convert container name '%s' to id", containerName)
	} else {
		return containerList[0].Id, nil
	}
}

func findRootFs(containerName string, runtimeClient pb.RuntimeServiceClient) (string, error) {
	var containerId string
	containerId, err := getContainerId(containerName, runtimeClient)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", err
	}
	mounts := strings.Split(string(data), "\n")
	for _, line := range mounts {
		if strings.Contains(line, containerId) && strings.Contains(line, "rootfs") {
			return strings.Split(line, " ")[1], nil
		}
	}
	return "", fmt.Errorf("could not find the rootfs for container %s(%s)", containerName, containerId)
}

func resolvePath(path string, runtimeClient pb.RuntimeServiceClient) (string, error) {
	logrus.Debugf("Resolving path %v", path)
	if strings.Contains(path, ":") {
		cName := strings.Split(path, ":")[0]
		cPath := strings.Split(path, ":")[1]
		resPath, err := findRootFs(cName, runtimeClient)
		if err != nil {
			return "", err
		}
		return resPath + cPath, nil
	}
	return path, nil
}

func copyFile(src, dst string) error {
	_cmd := fmt.Sprintf("cp %s %s", src, dst)
	logrus.Debugf("copy command: %v", _cmd)

	_cmdSplits := strings.Split(_cmd, " ")
	cp := exec.Command(_cmdSplits[0], _cmdSplits[1:]...)
	return cp.Run()
}

func copy(src, dst string, runtimeClient pb.RuntimeServiceClient) error {

	srcRes, err := resolvePath(src, runtimeClient)
	if err != nil {
		return err
	}

	dstRes, err := resolvePath(dst, runtimeClient)
	if err != nil {
		return err
	}
	logrus.Debugf("Copying %s to %s\n", srcRes, dstRes)
	return copyFile(srcRes, dstRes)
}

func validateRuntime(runtimeClient pb.RuntimeServiceClient) error {
	request := &pb.VersionRequest{Version: criClientVersion}
	r, err := runtimeClient.Version(context.Background(), request)
	if err != nil {
		return err
	}
	if r.RuntimeName != "containerd" {
		return fmt.Errorf("copy not supported for runtime '%v'", r.RuntimeName)
	}
	return nil
}

// Copy file to and from container runtime.
func Copy(cliContext *cli.Context) error {
	if cliContext.Args().Len() != 2 {
		return fmt.Errorf("usage: %s <src> <dst>", os.Args[0])
	}
	args := cliContext.Args().Slice()
	src := args[0]
	dst := args[1]

	runtimeClient, runtimeConn, err := getRuntimeClient(cliContext)
	if err != nil {
		return err
	}
	defer closeConnection(cliContext, runtimeConn)
	if err := validateRuntime(runtimeClient); err != nil {
		return err
	}
	return copy(src, dst, runtimeClient)
}
