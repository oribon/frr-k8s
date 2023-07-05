// SPDX-License-Identifier:Apache-2.0

package controller

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
	"github.com/metallb/frrk8s/internal/ipfamily"
)

func TestConversion(t *testing.T) {
	tests := []struct {
		name     string
		fromK8s  []v1beta1.FRRConfiguration
		expected *frr.Config
		err      error
	}{

		{
			name: "Single Router and Neighbor",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65001,
									ID:  "192.0.2.1",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65002,
											Address: "192.0.2.2",
											Port:    179,
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.0/24"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65001,
						RouterID: "192.0.2.1",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:       ipfamily.IPv4,
								Name:           "65002@192.0.2.2",
								ASN:            65002,
								Addr:           "192.0.2.2",
								Port:           179,
								Advertisements: []*frr.AdvertisementConfig{},
							},
						},
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{},
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple Routers and Neighbors",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65011,
											Address: "192.0.2.6",
											Port:    179,
										},
										{
											ASN:     65012,
											Address: "192.0.2.7",
											Port:    179,
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.0/24"},
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65014,
											Address: "2001:db8::4",
											Port:    179,
										},
									},
									VRF:      "vrf2",
									Prefixes: []string{"2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:       ipfamily.IPv4,
								Name:           "65011@192.0.2.6",
								ASN:            65011,
								Addr:           "192.0.2.6",
								Port:           179,
								Advertisements: []*frr.AdvertisementConfig{},
							},
							{
								IPFamily:       ipfamily.IPv4,
								Name:           "65012@192.0.2.7",
								ASN:            65012,
								Addr:           "192.0.2.7",
								Port:           179,
								Advertisements: []*frr.AdvertisementConfig{},
							},
						},
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:       ipfamily.IPv6,
								Name:           "65014@2001:db8::4",
								ASN:            65014,
								Addr:           "2001:db8::4",
								Port:           179,
								Advertisements: []*frr.AdvertisementConfig{},
							},
						},
						VRF:          "vrf2",
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Multiple Routers and Neighbors, Multiple FRRConfigurations",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65012,
											Address: "192.0.2.7",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.2.1/32", "192.0.2.2/32"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithCommunity: []v1beta1.CommunityPrefixes{
													{
														Community: "100",
														Prefixes:  []string{"192.0.2.10"},
													},
													{
														Community: "101",
														Prefixes:  []string{"192.0.2.10", "192.0.2.11"},
													},
												},
												PrefixesWithLocalPref: []v1beta1.LocalPrefPrefixes{
													{
														LocalPref: 200,
														Prefixes:  []string{"192.0.2.10"},
													},
												},
											},
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.0/24"},
								},
								{
									ASN: 65013,
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65017,
											Address: "192.0.2.7",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.2.5"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
									},
									VRF:      "vrf2",
									Prefixes: []string{"192.0.2.0/24"},
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65014,
											Address: "2001:db8::4",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									VRF:      "vrf2",
									Prefixes: []string{"2001:db8::/64"},
								},
							},
						},
					},
				},
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65010,
									ID:  "192.0.2.5",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65011,
											Address: "192.0.2.6",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.3.1/32", "192.0.3.2/32"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65012,
											Address: "192.0.2.7",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.3.3/32"},
													Mode:     v1beta1.AllowRestricted,
												},
												PrefixesWithCommunity: []v1beta1.CommunityPrefixes{
													{
														Community: "100",
														Prefixes:  []string{"192.0.3.20"},
													},
													{
														Community: "101",
														Prefixes:  []string{"192.0.3.21"},
													},
												},
												PrefixesWithLocalPref: []v1beta1.LocalPrefPrefixes{
													{
														LocalPref: 200,
														Prefixes:  []string{"192.0.3.21"},
													},
												},
											},
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.3.0/24"},
								},
								{
									ASN: 65013,
									ID:  "2001:db8::3",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65014,
											Address: "2001:db8::4",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"2001:db9::/96"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
									},
									VRF:      "vrf2",
									Prefixes: []string{"2001:db9::/64"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65010,
						RouterID: "192.0.2.5",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65011@192.0.2.6",
								ASN:      65011,
								Addr:     "192.0.2.6",
								Port:     179,
								Advertisements: []*frr.AdvertisementConfig{
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.3.1/32",
									},
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.3.2/32",
									},
								},
								HasV4Advertisements: true,
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65012@192.0.2.7",
								ASN:      65012,
								Addr:     "192.0.2.7",
								Port:     179,
								Advertisements: []*frr.AdvertisementConfig{
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.2.1/32",
									},
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.2.2/32",
									},
									{
										IPFamily:    ipfamily.IPv4,
										Prefix:      "192.0.2.10",
										Communities: []string{"100", "101"},
										LocalPref:   200,
									},
									{
										IPFamily:    ipfamily.IPv4,
										Prefix:      "192.0.2.11",
										Communities: []string{"101"},
									},
									{
										IPFamily:    ipfamily.IPv4,
										Prefix:      "192.0.3.20",
										Communities: []string{"100"},
									},
									{
										IPFamily:    ipfamily.IPv4,
										Prefix:      "192.0.3.21",
										Communities: []string{"200"},
										LocalPref:   200,
									},
								},
								HasV4Advertisements: true,
							},
						},
						VRF:          "",
						IPV4Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24"},
						IPV6Prefixes: []string{},
					},
					{
						MyASN:    65013,
						RouterID: "2001:db8::3",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv6,
								Name:     "65014@2001:db8::4",
								ASN:      65014,
								Addr:     "2001:db8::4",
								Port:     179,
								Advertisements: []*frr.AdvertisementConfig{
									{
										IPFamily: ipfamily.IPv6,
										Prefix:   "2001:db8::/64",
									},
									{
										IPFamily: ipfamily.IPv6,
										Prefix:   "2001:db9::/96",
									},
								},
								HasV6Advertisements: true,
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65017@192.0.2.7",
								ASN:      65017,
								Addr:     "192.0.2.7",
								Port:     179,
								Advertisements: []*frr.AdvertisementConfig{
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.2.5",
									},
								},
								HasV4Advertisements: true,
							},
						},
						VRF:          "vrf2",
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64", "2001:db9::/64"},
					},
				},
			},
			err: nil,
		},
		{
			name: "IPv4 Neighbor with IPv4 and IPv6 Prefixes",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65020,
									ID:  "192.0.2.10",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65021,
											Address: "192.0.2.11",
											Port:    179,
										},
									},
									VRF:      "",
									Prefixes: []string{"192.0.2.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65020,
						RouterID: "192.0.2.10",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:       ipfamily.IPv4,
								Name:           "65021@192.0.2.11",
								ASN:            65021,
								Addr:           "192.0.2.11",
								Port:           179,
								Advertisements: []*frr.AdvertisementConfig{},
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			err: nil,
		},
		{
			name: "Empty Configuration",
			fromK8s: []v1beta1.FRRConfiguration{
				{},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{},
			},
			err: nil,
		},
		{
			name: "Non default VRF",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65030,
									ID:  "192.0.2.15",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65031,
											Address: "192.0.2.16",
											Port:    179,
										},
									},
									VRF:      "vrf1",
									Prefixes: []string{"192.0.2.0/24"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65030,
						RouterID: "192.0.2.15",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily:       ipfamily.IPv4,
								Name:           "65031@192.0.2.16",
								ASN:            65031,
								Addr:           "192.0.2.16",
								Port:           179,
								Advertisements: []*frr.AdvertisementConfig{},
							},
						},
						VRF:          "vrf1",
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{},
					},
				},
			},
			err: nil,
		},
		{
			name: "Neighbor with ToAdvertise",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.2.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65040,
						RouterID: "192.0.2.20",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      65041,
								Addr:     "192.0.2.21",
								Port:     179,
								Advertisements: []*frr.AdvertisementConfig{
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.2.0/24",
									},
								},
								HasV4Advertisements: true,
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24"},
						IPV6Prefixes: []string{},
					},
				},
			},
			err: nil,
		},
		{
			name: "Two Neighbor with ToAdvertise, one advertise all",
			fromK8s: []v1beta1.FRRConfiguration{
				{
					Spec: v1beta1.FRRConfigurationSpec{
						BGP: v1beta1.BGPConfig{
							Routers: []v1beta1.Router{
								{
									ASN: 65040,
									ID:  "192.0.2.20",
									Neighbors: []v1beta1.Neighbor{
										{
											ASN:     65041,
											Address: "192.0.2.21",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Prefixes: []string{"192.0.2.0/24", "192.0.4.0/24"},
													Mode:     v1beta1.AllowRestricted,
												},
											},
										},
										{
											ASN:     65041,
											Address: "192.0.2.22",
											Port:    179,
											ToAdvertise: v1beta1.Advertise{
												Allowed: v1beta1.AllowedPrefixes{
													Mode: v1beta1.AllowAll,
												},
											},
										},
									},
									Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24", "2001:db8::/64"},
								},
							},
						},
					},
				},
			},
			expected: &frr.Config{
				Routers: []*frr.RouterConfig{
					{
						MyASN:    65040,
						RouterID: "192.0.2.20",
						Neighbors: []*frr.NeighborConfig{
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.21",
								ASN:      65041,
								Addr:     "192.0.2.21",
								Port:     179,
								Advertisements: []*frr.AdvertisementConfig{
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.2.0/24",
									},
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.4.0/24",
									},
								},
								HasV4Advertisements: true,
							},
							{
								IPFamily: ipfamily.IPv4,
								Name:     "65041@192.0.2.22",
								ASN:      65041,
								Addr:     "192.0.2.22",
								Port:     179,
								Advertisements: []*frr.AdvertisementConfig{
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.2.0/24",
									},
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.3.0/24",
									},
									{
										IPFamily: ipfamily.IPv4,
										Prefix:   "192.0.4.0/24",
									},
									{
										IPFamily: ipfamily.IPv6,
										Prefix:   "2001:db8::/64",
									},
								},
								HasV4Advertisements: true,
								HasV6Advertisements: true,
							},
						},
						IPV4Prefixes: []string{"192.0.2.0/24", "192.0.3.0/24", "192.0.4.0/24"},
						IPV6Prefixes: []string{"2001:db8::/64"},
					},
				},
			},
			err: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frr, err := apiToFRR(test.fromK8s)
			if test.err != nil && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if test.err == nil && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if diff := cmp.Diff(frr, test.expected); diff != "" {
				s, _ := json.MarshalIndent(frr, "", "\t") // pretty print struct, REMOVE
				fmt.Print(string(s))
				t.Fatalf("config different from expected: %s", diff)
			}
		})
	}
}
