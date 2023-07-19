// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"fmt"

	v1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
	"github.com/metallb/frrk8s/internal/ipfamily"
)

func apiToFRR(fromK8s v1beta1.FRRConfiguration) (*frr.Config, error) {
	res := &frr.Config{
		Routers: make([]*frr.RouterConfig, 0),
		//BFDProfiles: sm.bfdProfiles,
		//ExtraConfig: sm.extraConfig,
	}

	for _, r := range fromK8s.Spec.BGP.Routers {
		frrRouter, err := routerToFRRConfig(r)
		if err != nil {
			return nil, err
		}
		res.Routers = append(res.Routers, frrRouter)
	}
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
		IPFamily:     neighborFamily,
		EBGPMultiHop: n.EBGPMultiHop,
	}

	res.Outgoing, err = toAdvertiseToFRR(n.ToAdvertise, ipv4Prefixes, ipv6Prefixes)
	if err != nil {
		return nil, err
	}
	res.Incoming = toReceiveToFRR(n.ToReceive)
	return res, nil
}

func toAdvertiseToFRR(toAdvertise v1beta1.Advertise, ipv4Prefixes, ipv6Prefixes []string) (frr.AllowedOut, error) {
	res := frr.AllowedOut{
		Prefixes: make([]frr.OutgoingFilter, 0),
	}

	advs := map[string]frr.OutgoingFilter{}
	if toAdvertise.Allowed.Mode == v1beta1.AllowAll {
		for _, p := range ipv4Prefixes {
			advs[p] = frr.OutgoingFilter{Prefix: p, IPFamily: ipfamily.IPv4}
			//res.Prefixes = append(res.Prefixes, frr.OutgoingFilter{Prefix: p, IPFamily: ipfamily.IPv4})
			res.HasV4 = true
		}
		for _, p := range ipv6Prefixes {
			advs[p] = frr.OutgoingFilter{Prefix: p, IPFamily: ipfamily.IPv6}
			//res.Prefixes = append(res.Prefixes, frr.OutgoingFilter{Prefix: p, IPFamily: ipfamily.IPv6})
			res.HasV6 = true
		}
	}

	for _, p := range toAdvertise.Allowed.Prefixes {
		family := ipfamily.ForCIDRString(p)
		switch family {
		case ipfamily.IPv4:
			res.HasV4 = true
		case ipfamily.IPv6:
			res.HasV6 = true
		}
		advs[p] = frr.OutgoingFilter{Prefix: p, IPFamily: family}
		//res.Prefixes = append(res.Prefixes, frr.OutgoingFilter{Prefix: p, IPFamily: family})
	}

	localPrefsForPrefix := map[string]uint32{}
	for _, pfxs := range toAdvertise.PrefixesWithLocalPref {
		for _, p := range pfxs.Prefixes {
			_, ok := advs[p]
			if !ok {
				return frr.AllowedOut{}, fmt.Errorf("prefix %s with localpref %d not in allowed list", p, pfxs.LocalPref)
			}
			v, ok := localPrefsForPrefix[p]
			if !ok {
				localPrefsForPrefix[p] = pfxs.LocalPref
				continue
			}

			if v != pfxs.LocalPref {
				return frr.AllowedOut{}, fmt.Errorf("duplicate localpref for prefix %s", p)
			}
		}
	}

	for p, lp := range localPrefsForPrefix {
		adv, ok := advs[p]
		if !ok { // shouldn't happen as we check in previous loop, just in case
			return frr.AllowedOut{}, fmt.Errorf("unexpected err - no localpref prefix matching %s", p)
		}
		adv.LocalPref = lp
	}

	return res, nil
}

func toReceiveToFRR(toReceive v1beta1.Receive) frr.AllowedIn {
	res := frr.AllowedIn{
		Prefixes: make([]frr.IncomingFilter, 0),
	}
	if toReceive.Allowed.Mode == v1beta1.AllowAll {
		res.All = true
		return res
	}
	for _, p := range toReceive.Allowed.Prefixes {
		family := ipfamily.ForCIDRString(p)
		res.Prefixes = append(res.Prefixes, frr.IncomingFilter{Prefix: p, IPFamily: family})
		if family == ipfamily.IPv4 {
			res.HasV4 = true
			continue
		}
		res.HasV6 = true
	}
	return res
}

func neighborName(ASN uint32, peerAddr string) string {
	return fmt.Sprintf("%d@%s", ASN, peerAddr)
}
