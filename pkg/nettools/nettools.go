/*
Copyright 2016 Mirantis

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

// Based on CNI's plugins/main/bridge/bridge.go, pkg/ip/link.go
// Original copyright notice:
//
// Copyright 2014 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nettools

import (
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/vishvananda/netlink"
)

// CreateEscapeVethPair creates a veth pair with contVeth residing in
// the specified container network namespace and hostVeth residing in
// the host namespace.
func CreateEscapeVethPair(contNS ns.NetNS, ifName string, mtu int) (hostVeth, contVeth netlink.Link, err error) {
	var hostVethName string

	err = contNS.Do(func(hostNS ns.NetNS) error {
		// create the veth pair in the container and move host end into host netns
		hostVeth, contVeth, err = ip.SetupVeth(ifName, mtu, hostNS)
		if err != nil {
			return err
		}

		hostVethName = hostVeth.Attrs().Name
		return nil
	})
	if err != nil {
		return
	}

	// need to lookup hostVeth again as its index has changed during ns move
	hostVeth, err = netlink.LinkByName(hostVethName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lookup %q: %v", hostVethName, err)
	}

	return
}

func createBridge(brName string, mtu int) (*netlink.Bridge, error) {
	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: brName,
			MTU:  mtu,
			// Let kernel use default txqueuelen; leaving it unset
			// means 0, and a zero-length TX queue messes up FIFO
			// traffic shapers which use TX queue length as the
			// default packet limit
			TxQLen: -1,
		},
	}

	if err := netlink.LinkAdd(br); err != nil {
		return nil, fmt.Errorf("could not add %q: %v", brName, err)
	}

	if err := netlink.LinkSetUp(br); err != nil {
		return nil, err
	}

	return br, nil
}

// randomBridgeName returns string "br" with random suffix (hashed from entropy)
func randomBridgeName() (string, error) {
	entropy := make([]byte, 4)
	if _, err := rand.Reader.Read(entropy); err != nil {
		return "", fmt.Errorf("failed to generate random bridge name: %v", err)
	}

	return fmt.Sprintf("br%x", entropy), nil
}

// SetupBridge creates a bridge and adds specified links to it.
// It sets bridge's MTU to MTU value of the first link.
func SetupBridge(links []netlink.Link) (*netlink.Bridge, error) {
	if len(links) == 0 {
		return nil, errors.New("no links provided")
	}

	bridgeName, err := randomBridgeName()
	if err != nil {
		return nil, err
	}

	br, err := createBridge(bridgeName, links[0].Attrs().MTU)
	if err != nil {
		return nil, fmt.Errorf("failed to create bridge %q: %v", bridgeName, err)
	}

	for _, link := range links {
		if err = netlink.LinkSetMaster(link, br); err != nil {
			delMessage := ""
			if delErr := netlink.LinkDel(br); delErr != nil {
				delMessage = fmt.Sprintf(" (and failed to delete the bridge: %v", err)
			}
			return nil, fmt.Errorf("failed to connect %q to bridge %v: %v%s", link.Attrs().Name, br.Attrs().Name, err, delMessage)
		}
	}

	return br, nil
}
