// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
)

var (
	fakeBGP = &fakeBGPFetcher{m: make(map[string][]*frr.Neighbor)}
	fakeBFD = &fakeBFDFetcher{m: make(map[string][]frr.BFDPeer)}
)

type fakeBGPFetcher struct {
	m map[string][]*frr.Neighbor
}

func (f *fakeBGPFetcher) GetBGPNeighbors() (map[string][]*frr.Neighbor, error) {
	return f.m, nil
}

type fakeBFDFetcher struct {
	m map[string][]frr.BFDPeer
}

func (f *fakeBFDFetcher) GetBFDPeers() (map[string][]frr.BFDPeer, error) {
	return f.m, nil
}

var _ = Describe("BGPSessionState Controller", func() {
	Context("SetupWithManager", func() {
		It("should reconcile correctly", func() {
			fakeBGP.m = map[string][]*frr.Neighbor{
				"default": {
					{
						ID:       "192.168.1.1",
						BGPState: "Active",
					},
					{
						ID:       "192.168.1.2",
						BGPState: "Active",
					},
					{
						ID:       "fc00:f853:ccd:e899::",
						BGPState: "Active",
					},
					{
						ID:       "eth0",
						BGPState: "Active",
					},
				},
				"red": {
					{
						ID:       "192.168.1.1",
						BGPState: "Active",
					},
				},
			}
			fakeBFD.m = map[string][]frr.BFDPeer{
				"default": {
					{
						Peer:   "192.168.1.1",
						Status: "down",
					},
				},
				"red": {
					{
						Peer:   "192.168.1.1",
						Status: "down",
					},
				},
			}

			expectedStatuses := expectedStatusesFor(*fakeBGP, *fakeBFD)
			Eventually(func() error {
				l := frrk8sv1beta1.BGPSessionStateList{}
				err := k8sClient.List(context.Background(), &l)
				if err != nil {
					return err
				}
				got, err := statusMapFor(l)
				if err != nil {
					return err
				}
				if !cmp.Equal(expectedStatuses, got) {
					return fmt.Errorf("expected statuses to be %v, got %v\n diff %s", expectedStatuses, got, cmp.Diff(expectedStatuses, got))
				}
				return nil
			}, 5*time.Second, time.Second).ShouldNot(HaveOccurred())

			By("Updating the first peer's inner BGP and BFD state")
			fakeBGP.m = map[string][]*frr.Neighbor{
				"default": {
					{
						ID:       "192.168.1.1",
						BGPState: "Established",
					},
					{
						ID:       "192.168.1.2",
						BGPState: "Active",
					},
					{
						ID:       "fc00:f853:ccd:e899::",
						BGPState: "Active",
					},
					{
						ID:       "eth0",
						BGPState: "Active",
					},
				},
				"red": {
					{
						ID:       "192.168.1.1",
						BGPState: "Active",
					},
				},
			}
			fakeBFD.m = map[string][]frr.BFDPeer{
				"default": {
					{
						Peer:   "192.168.1.1",
						Status: "up",
					},
				},
				"red": {
					{
						Peer:   "192.168.1.1",
						Status: "down",
					},
				},
			}

			expectedStatuses = expectedStatusesFor(*fakeBGP, *fakeBFD)
			Eventually(func() error {
				l := frrk8sv1beta1.BGPSessionStateList{}
				err := k8sClient.List(context.Background(), &l)
				if err != nil {
					return err
				}
				got, err := statusMapFor(l)
				if err != nil {
					return err
				}
				if !cmp.Equal(expectedStatuses, got) {
					return fmt.Errorf("expected statuses to be %v, got %v\n diff %s", expectedStatuses, got, cmp.Diff(expectedStatuses, got))
				}
				return nil
			}, 5*time.Second, time.Second).ShouldNot(HaveOccurred())

			By("Removing the second+third+fourth peers and updating the fifth")
			fakeBGP.m = map[string][]*frr.Neighbor{
				"default": {
					{
						ID:       "192.168.1.1",
						BGPState: "Established",
					},
				},
				"red": {
					{
						ID:       "192.168.1.1",
						BGPState: "Established",
					},
				},
			}
			fakeBFD.m = map[string][]frr.BFDPeer{
				"default": {
					{
						Peer:   "192.168.1.1",
						Status: "up",
					},
				},
				"red": {
					{
						Peer:   "192.168.1.1",
						Status: "up",
					},
				},
			}

			expectedStatuses = expectedStatusesFor(*fakeBGP, *fakeBFD)
			Eventually(func() error {
				l := frrk8sv1beta1.BGPSessionStateList{}
				err := k8sClient.List(context.Background(), &l)
				if err != nil {
					return err
				}
				got, err := statusMapFor(l)
				if err != nil {
					return err
				}
				if !cmp.Equal(expectedStatuses, got) {
					return fmt.Errorf("expected statuses to be %v, got %v\n diff %s", expectedStatuses, got, cmp.Diff(expectedStatuses, got))
				}
				return nil
			}, 5*time.Second, time.Second).ShouldNot(HaveOccurred())

		})
	})
})

func statusMapFor(l frrk8sv1beta1.BGPSessionStateList) (map[string]map[string]frrk8sv1beta1.BGPSessionStateStatus, error) {
	res := map[string]map[string]frrk8sv1beta1.BGPSessionStateStatus{}
	for _, s := range l.Items {
		if _, ok := res[s.Status.VRF]; !ok {
			res[s.Status.VRF] = map[string]frrk8sv1beta1.BGPSessionStateStatus{}
		}
		if _, ok := res[s.Status.VRF][s.Status.Peer]; ok {
			return nil, fmt.Errorf("got multiple statuses for peer %s-%s\n all statuses: %v", s.Status.Peer, s.Status.VRF, l.Items)
		}
		res[s.Status.VRF][s.Status.Peer] = s.Status
	}

	return res, nil
}

func expectedStatusesFor(fBGP fakeBGPFetcher, fBFD fakeBFDFetcher) map[string]map[string]frrk8sv1beta1.BGPSessionStateStatus {
	res := map[string]map[string]frrk8sv1beta1.BGPSessionStateStatus{}

	bfdForPeer := map[string]map[string]string{}
	for vrf, bfdPeers := range fBFD.m {
		bfdForPeer[vrf] = map[string]string{}
		for _, bfdPeer := range bfdPeers {
			bfdForPeer[vrf][bfdPeer.Peer] = bfdPeer.Status
		}
	}

	for vrf, bgpPeers := range fBGP.m {
		res[vrf] = map[string]frrk8sv1beta1.BGPSessionStateStatus{}
		for _, bgpPeer := range bgpPeers {
			bfdStatus := ""
			if _, ok := bfdForPeer[vrf]; ok {
				bfdStatus = bfdForPeer[vrf][bgpPeer.ID]
			}
			if bfdStatus == "" {
				bfdStatus = noBFDConfigured
			}
			res[vrf][statusFormatFor(bgpPeer.ID)] = frrk8sv1beta1.BGPSessionStateStatus{
				Node:      testNodeName,
				Peer:      statusFormatFor(bgpPeer.ID),
				VRF:       vrf,
				BGPStatus: bgpPeer.BGPState,
				BFDStatus: bfdStatus,
			}
		}
	}

	res[""] = map[string]frrk8sv1beta1.BGPSessionStateStatus{}
	for k, v := range res["default"] {
		v.VRF = ""
		res[""][k] = v
	}
	delete(res, "default")

	return res
}
