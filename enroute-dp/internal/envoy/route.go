// SPDX-License-Identifier: Apache-2.0
// Copyright(c) 2018-2019 Saaras Inc.

// Copyright © 2018 Heptio
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

package envoy

import (
	"sort"
	"strings"
	"time"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	envoy_api_v2_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/saarasio/enroute/enroute-dp/internal/dag"
	"github.com/saarasio/enroute/enroute-dp/internal/protobuf"
)

// RouteRoute creates a envoy_api_v2_route.Route_Route for the services supplied.
// If len(services) is greater than one, the route's action will be a
// weighted cluster.
func RouteRoute(r *dag.Route) *envoy_api_v2_route.Route_Route {
	ra := envoy_api_v2_route.RouteAction{
		RetryPolicy:   retryPolicy(r),
		Timeout:       responseTimeout(r),
		PrefixRewrite: r.PrefixRewrite,
		HashPolicy:    hashPolicy(r),
	}

	SetupRouteRateLimits(r, &ra)

	if r.Websocket {
		ra.UpgradeConfigs = append(ra.UpgradeConfigs,
			&envoy_api_v2_route.RouteAction_UpgradeConfig{
				UpgradeType: "websocket",
			},
		)
	}

	switch len(r.Clusters) {
	case 1:
		ra.ClusterSpecifier = &envoy_api_v2_route.RouteAction_Cluster{
			Cluster: Clustername(r.Clusters[0]),
		}
	default:
		ra.ClusterSpecifier = &envoy_api_v2_route.RouteAction_WeightedClusters{
			WeightedClusters: weightedClusters(r.Clusters),
		}
	}
	return &envoy_api_v2_route.Route_Route{
		Route: &ra,
	}
}

// hashPolicy returns a slice of hash policies iff at least one of the route's
// clusters supplied uses the `Cookie` load balancing stategy.
func hashPolicy(r *dag.Route) []*envoy_api_v2_route.RouteAction_HashPolicy {
	for _, c := range r.Clusters {
		if c.LoadBalancerStrategy == "Cookie" {
			return []*envoy_api_v2_route.RouteAction_HashPolicy{{
				PolicySpecifier: &envoy_api_v2_route.RouteAction_HashPolicy_Cookie_{
					Cookie: &envoy_api_v2_route.RouteAction_HashPolicy_Cookie{
						Name: "X-Contour-Session-Affinity",
						Ttl:  protobuf.Duration(0),
						Path: "/",
					},
				},
			}}
		}
	}
	return nil
}

func responseTimeout(r *dag.Route) *duration.Duration {
	if r.TimeoutPolicy == nil {
		return nil
	}
	return timeout(r.TimeoutPolicy.Timeout)
}

// timeout interprets a time.Duration with respect to
// Envoy's timeout logic. Zero durations are interpreted
// as nil, therefore remaining unset. Negative durations
// are interpreted as infinity, which is represented as
// an explicit value of 0. Positive durations behave as
// expected.
func timeout(d time.Duration) *duration.Duration {
	switch {
	case d == 0:
		// no timeout specified
		return nil
	case d < 0:
		// infinite timeout, set timeout value to a pointer to zero which tells
		// envoy "infinite timeout"
		return protobuf.Duration(0)
	default:
		return protobuf.Duration(d)
	}
}

func retryPolicy(r *dag.Route) *envoy_api_v2_route.RetryPolicy {
	if r.RetryPolicy == nil {
		return nil
	}
	if r.RetryPolicy.RetryOn == "" {
		return nil
	}

	rp := &envoy_api_v2_route.RetryPolicy{
		RetryOn: r.RetryPolicy.RetryOn,
	}
	if r.RetryPolicy.NumRetries > 0 {
		rp.NumRetries = protobuf.UInt32(r.RetryPolicy.NumRetries)
	}
	if r.RetryPolicy.PerTryTimeout > 0 {
		rp.PerTryTimeout = protobuf.Duration(r.RetryPolicy.PerTryTimeout)
	}
	return rp
}

// UpgradeHTTPS returns a route Action that redirects the request to HTTPS.
func UpgradeHTTPS() *envoy_api_v2_route.Route_Redirect {
	return &envoy_api_v2_route.Route_Redirect{
		Redirect: &envoy_api_v2_route.RedirectAction{
			SchemeRewriteSpecifier: &envoy_api_v2_route.RedirectAction_HttpsRedirect{
				HttpsRedirect: true,
			},
		},
	}
}

// RouteHeaders returns a list of headers to be applied at the Route level on envoy
func RouteHeaders() []*envoy_api_v2_core.HeaderValueOption {
	return Headers(
		AppendHeader("x-request-start", "t=%START_TIME(%s.%3f)%"),
	)
}

// weightedClusters returns a route.WeightedCluster for multiple services.
func weightedClusters(clusters []*dag.Cluster) *envoy_api_v2_route.WeightedCluster {
	var wc envoy_api_v2_route.WeightedCluster
	var total uint32
	for _, cluster := range clusters {
		total += cluster.Weight
		wc.Clusters = append(wc.Clusters, &envoy_api_v2_route.WeightedCluster_ClusterWeight{
			Name:   Clustername(cluster),
			Weight: protobuf.UInt32(cluster.Weight),
		})
	}
	// Check if no weights were defined, if not default to even distribution
	if total == 0 {
		for _, c := range wc.Clusters {
			c.Weight.Value = 1
		}
		total = uint32(len(clusters))
	}
	wc.TotalWeight = protobuf.UInt32(total)

	sort.Stable(clusterWeightByName(wc.Clusters))
	return &wc
}

// RouteMatch creates a RouteMatch for the supplied prefix/regex.
func RouteMatch(path string) *envoy_api_v2_route.RouteMatch {
	// Check if path contains a regex
	if strings.ContainsAny(path, "^+*[]%") {
		return &envoy_api_v2_route.RouteMatch{
			PathSpecifier: &envoy_api_v2_route.RouteMatch_Regex{
				Regex: path,
			},
		}
	}
	return &envoy_api_v2_route.RouteMatch{
		PathSpecifier: &envoy_api_v2_route.RouteMatch_Prefix{
			Prefix: path,
		},
	}
}

// VirtualHost creates a new route.VirtualHost.
func VirtualHost(hostname string) *envoy_api_v2_route.VirtualHost {
	domains := []string{hostname}
	if hostname != "*" {
		domains = append(domains, hostname+":*")
	}
	return &envoy_api_v2_route.VirtualHost{
		Name:    hashname(60, hostname),
		Domains: domains,
	}
}

// RouteConfiguration returns a *v2.RouteConfiguration.
func RouteConfiguration(name string, virtualhosts ...*envoy_api_v2_route.VirtualHost) *v2.RouteConfiguration {
	return &v2.RouteConfiguration{
		Name:         name,
		VirtualHosts: virtualhosts,
		RequestHeadersToAdd: Headers(
			AppendHeader("x-request-start", "t=%START_TIME(%s.%3f)%"),
		),
	}
}

type clusterWeightByName []*envoy_api_v2_route.WeightedCluster_ClusterWeight

func (c clusterWeightByName) Len() int      { return len(c) }
func (c clusterWeightByName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c clusterWeightByName) Less(i, j int) bool {
	if c[i].Name == c[j].Name {
		return c[i].Weight.Value < c[j].Weight.Value
	}
	return c[i].Name < c[j].Name

}

func Headers(first *envoy_api_v2_core.HeaderValueOption, rest ...*envoy_api_v2_core.HeaderValueOption) []*envoy_api_v2_core.HeaderValueOption {
	return append([]*envoy_api_v2_core.HeaderValueOption{first}, rest...)
}

func AppendHeader(key, value string) *envoy_api_v2_core.HeaderValueOption {
	return &envoy_api_v2_core.HeaderValueOption{
		Header: &envoy_api_v2_core.HeaderValue{
			Key:   key,
			Value: value,
		},
		Append: protobuf.Bool(true),
	}
}
