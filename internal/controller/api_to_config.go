// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"
	"sort"

	v1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
	"github.com/metallb/frrk8s/internal/ipfamily"
	"k8s.io/apimachinery/pkg/util/sets"
)

func apiToFRR(fromK8s []v1beta1.FRRConfiguration) (*frr.Config, error) {
	res := &frr.Config{
		Routers: make([]*frr.RouterConfig, 0),
		//BFDProfiles: sm.bfdProfiles,
		//ExtraConfig: sm.extraConfig,
	}

	vrfRouters := map[string]*frr.RouterConfig{} // vrf+ASN -> config
	for _, cfg := range fromK8s {
		for _, r := range cfg.Spec.BGP.Routers {
			routerCfg, err := routerToFRRConfig(r)
			if err != nil {
				return nil, err
			}

			curr := vrfRouters[r.VRF]
			if curr == nil {
				vrfRouters[r.VRF] = routerCfg
				continue
			}

			// Merging by VRF
			curr, err = mergeRouterConfigs(curr, routerCfg)
			if err != nil {
				return nil, err
			}

			vrfRouters[r.VRF] = curr
		}
	}

	res.Routers = sortMap(vrfRouters)

	return res, nil
}

func routerToFRRConfig(r v1beta1.Router) (*frr.RouterConfig, error) {
	res := &frr.RouterConfig{
		MyASN:        r.ASN,
		RouterID:     r.ID,
		VRF:          r.VRF,
		Neighbors:    make([]*frr.NeighborConfig, 0),
		IPV4Prefixes: make([]string, 0),
		IPV6Prefixes: make([]string, 0),
	}

	for _, p := range r.Prefixes {
		family := ipfamily.ForCIDRString(p)
		switch family {
		case ipfamily.IPv4:
			res.IPV4Prefixes = append(res.IPV4Prefixes, p)
		case ipfamily.IPv6:
			res.IPV6Prefixes = append(res.IPV6Prefixes, p)
		case ipfamily.Unknown:
			return nil, fmt.Errorf("unknown ipfamily for %s", p)
		}
	}

	for _, n := range r.Neighbors {
		frrNeigh, err := neighborToFRR(n, res.IPV4Prefixes, res.IPV6Prefixes)
		if err != nil {
			return nil, err
		}
		res.Neighbors = append(res.Neighbors, frrNeigh)
	}

	return res, nil
}

func neighborToFRR(n v1beta1.Neighbor, ipv4Prefixes, ipv6Prefixes []string) (*frr.NeighborConfig, error) {
	neighborFamily, err := ipfamily.ForAddresses(n.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to find ipfamily for %s, %w", n.Address, err)
	}
	res := &frr.NeighborConfig{
		Name: neighborName(n.ASN, n.Address),
		ASN:  n.ASN,
		Addr: n.Address,
		Port: n.Port,
		// Password:       n.Password, TODO password as secret
		Advertisements: make([]*frr.AdvertisementConfig, 0),
		IPFamily:       neighborFamily,
		EBGPMultiHop:   n.EBGPMultiHop,
	}

	if n.ToAdvertise.Allowed.Mode == v1beta1.AllowAll {
		for _, p := range ipv4Prefixes {
			res.Advertisements = append(res.Advertisements, &frr.AdvertisementConfig{Prefix: p, IPFamily: ipfamily.IPv4})
			res.HasV4Advertisements = true
		}
		for _, p := range ipv6Prefixes {
			res.Advertisements = append(res.Advertisements, &frr.AdvertisementConfig{Prefix: p, IPFamily: ipfamily.IPv6})
			res.HasV6Advertisements = true
		}

		return res, nil
	}

	for _, p := range n.ToAdvertise.Allowed.Prefixes {
		family := ipfamily.ForCIDRString(p)
		switch family {
		case ipfamily.IPv4:
			res.HasV4Advertisements = true
		case ipfamily.IPv6:
			res.HasV6Advertisements = true
		}
		res.Advertisements = append(res.Advertisements, &frr.AdvertisementConfig{Prefix: p, IPFamily: family})
	}

	return res, nil
}

func neighborName(ASN uint32, peerAddr string) string {
	return fmt.Sprintf("%d@%s", ASN, peerAddr)
}

// Assumes both routers are in the same vrf
func mergeRouterConfigs(r, toMerge *frr.RouterConfig) (*frr.RouterConfig, error) {
	err := routersAreCompatible(r, toMerge)
	if err != nil {
		return nil, err
	}

	if r.RouterID == "" {
		r.RouterID = toMerge.RouterID
	}

	v4Prefixes := sets.New(append(r.IPV4Prefixes, toMerge.IPV4Prefixes...)...)
	v6Prefixes := sets.New(append(r.IPV6Prefixes, toMerge.IPV6Prefixes...)...)

	mergedNeighbors, err := mergeNeighbors(r.Neighbors, toMerge.Neighbors)
	if err != nil {
		return nil, err
	}

	r.IPV4Prefixes = sets.List(v4Prefixes)
	r.IPV6Prefixes = sets.List(v6Prefixes)
	r.Neighbors = mergedNeighbors

	return r, nil
}

// Assumes both routers are the same vrf
func routersAreCompatible(r, toMerge *frr.RouterConfig) error {
	if r.MyASN != toMerge.MyASN {
		return fmt.Errorf("different asns (%d != %d) specified for same vrf: %s", r.MyASN, toMerge.MyASN, r.VRF)
	}

	bothRouterIDsNonEmpty := r.RouterID != "" && toMerge.RouterID != ""
	routerIDsDifferent := r.RouterID != toMerge.RouterID
	if bothRouterIDsNonEmpty && routerIDsDifferent {
		return fmt.Errorf("different router ids (%s != %s) specified for same vrf: %s", r.RouterID, toMerge.RouterID, r.VRF)
	}

	return nil
}

// Assumes they all live in the same VRF
func mergeNeighbors(n, toMerge []*frr.NeighborConfig) ([]*frr.NeighborConfig, error) {
	neighbors := append(n, toMerge...)
	if len(neighbors) == 0 {
		return []*frr.NeighborConfig{}, nil
	}

	mergedNeighbors := map[string]*frr.NeighborConfig{}

	for _, n := range neighbors {
		curr, found := mergedNeighbors[n.Name]
		if !found {
			mergedNeighbors[n.Name] = n
			continue
		}

		err := neighborsAreCompatible(curr, n)
		if err != nil {
			return nil, err
		}

		curr.Advertisements, err = mergeAdvertisements(curr.Advertisements, n.Advertisements)
		if err != nil {
			return nil, fmt.Errorf("could not merge advertisements for neighbor %s vrf %s, err: %w", n.Addr, n.VRFName, err)
		}
		curr.HasV4Advertisements = curr.HasV4Advertisements || n.HasV4Advertisements
		curr.HasV6Advertisements = curr.HasV6Advertisements || n.HasV6Advertisements

		mergedNeighbors[n.Name] = curr
	}

	return sortMap(mergedNeighbors), nil
}

// Assumes neighbors are in the same VRF, same IP
func neighborsAreCompatible(n1, n2 *frr.NeighborConfig) error {
	neighborKey := fmt.Sprintf("neighbor %s at vrf %s", n1.Addr, n1.VRFName)
	if n1.ASN != n2.ASN {
		return fmt.Errorf("multiple asns specified for %s", neighborKey)
	}

	if n1.Port != n2.Port {
		return fmt.Errorf("multiple ports specified for %s", neighborKey)
	}

	if n1.SrcAddr != n2.SrcAddr {
		return fmt.Errorf("multiple source addresses specified for %s", neighborKey)
	}

	if n1.Password != n2.Password {
		return fmt.Errorf("multiple passwords specified for %s", neighborKey)
	}

	if n1.BFDProfile != n2.BFDProfile {
		return fmt.Errorf("multiple bfd profiles specified for %s", neighborKey)
	}

	if n1.EBGPMultiHop != n2.EBGPMultiHop {
		return fmt.Errorf("conflicting ebgp-multihop specified for %s", neighborKey)
	}

	// TODO: ?
	if n1.HoldTime != n2.HoldTime {
		return fmt.Errorf("multiple hold times specified for %s", neighborKey)
	}

	// TODO: ?
	if n1.KeepaliveTime != n2.KeepaliveTime {
		return fmt.Errorf("multiple keepalive times specified for %s", neighborKey)
	}

	return nil
}

// Assumes they are for the same neighbor
func mergeAdvertisements(a, toMerge []*frr.AdvertisementConfig) ([]*frr.AdvertisementConfig, error) {
	advs := append(a, toMerge...)
	if len(advs) == 0 {
		return []*frr.AdvertisementConfig{}, nil
	}

	mergedAdvs := map[string]*frr.AdvertisementConfig{}

	for _, a := range advs {
		curr, found := mergedAdvs[a.Prefix]
		if !found {
			mergedAdvs[a.Prefix] = a
			continue
		}

		if curr.LocalPref != a.LocalPref {
			return nil, fmt.Errorf("multiple local prefs specified for prefix %s", curr.Prefix)
		}

		communites := sets.New(append(curr.Communities, a.Communities...)...)
		curr.Communities = sets.List(communites)

		mergedAdvs[curr.Prefix] = curr
	}

	return sortMap(mergedAdvs), nil
}

func sortMap[T any](toSort map[string]T) []T {
	keys := make([]string, 0)
	for k := range toSort {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	res := make([]T, 0)
	for _, k := range keys {
		res = append(res, toSort[k])
	}
	return res
}
