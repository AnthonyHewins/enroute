// Copyright © 2019 VMware
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

package dag

import (
	"fmt"
	"regexp"
	"strings"

	ingressroutev1 "github.com/saarasio/enroute/enroute-dp/apis/enroute/v1beta1"
)

// mergePathConditions merges the given slice of prefix Conditions into a single
// prefix Condition.
// pathConditionsValid guarantees that if a prefix is present, it will start with a
// / character, so we can simply concatenate.
func mergePathConditions(conds []ingressroutev1.Condition) Condition {

	prefix := ""
	for _, cond := range conds {
		prefix = prefix + cond.Prefix
	}

	re := regexp.MustCompile(`//+`)
	prefix = re.ReplaceAllString(prefix, `/`)

	// TODO 6-3-2020 Support Regex Routes
	// If this smells like regex, provide a RegexCondition instead of a PrefixCondition
	if len(prefix) > 0 {
		if strings.ContainsAny(prefix, "^+*[]%") {
			return &RegexCondition{
				Regex: prefix,
			}
		}
	}

	// After the merge operation is done, if the string is still empty, then
	// we need to set the prefix to /.
	// Remember that this step is done AFTER all the includes have happened.
	// Setting this to / allows us to pass this prefix to Envoy, as there must
	// be at least one path, prefix, or regex set on each Envoy route.
	if prefix == "" {
		prefix = `/`
	}

	return &PrefixCondition{
		Prefix: prefix,
	}
}

// pathConditionsValid validates a slice of Conditions can be correctly merged.
// It encodes the business rules about what is allowed for prefix Conditions.
func pathConditionsValid(conds []ingressroutev1.Condition, conditionsContext string) (bool, string) {
	prefixCount := 0
	for _, cond := range conds {
		if cond.Prefix != "" {
			prefixCount++
			if cond.Prefix[0] != '/' {
				err_message := fmt.Sprintf("%s: Prefix conditions must start with /, %s was supplied", conditionsContext, cond.Prefix)
				return false, err_message
			}
		}
		if prefixCount > 1 {
			err_message := fmt.Sprintf("%s: More than one prefix is not allowed in a condition block", conditionsContext)
			return false, err_message
		}
	}
	return true, ""
}

func mergeHeaderConditions(conds []ingressroutev1.Condition) []HeaderCondition {
	var hc []HeaderCondition
	for _, cond := range conds {
		switch {
		case cond.Header == nil:
			// skip it
		case cond.Header.Present:
			hc = append(hc, HeaderCondition{
				Name:      cond.Header.Name,
				MatchType: "present",
			})
		case cond.Header.Contains != "":
			hc = append(hc, HeaderCondition{
				Name:      cond.Header.Name,
				Value:     cond.Header.Contains,
				MatchType: "contains",
			})
		case cond.Header.NotContains != "":
			hc = append(hc, HeaderCondition{
				Name:      cond.Header.Name,
				Value:     cond.Header.NotContains,
				MatchType: "contains",
				Invert:    true,
			})
		case cond.Header.Exact != "":
			hc = append(hc, HeaderCondition{
				Name:      cond.Header.Name,
				Value:     cond.Header.Exact,
				MatchType: "exact",
			})
		case cond.Header.NotExact != "":
			hc = append(hc, HeaderCondition{
				Name:      cond.Header.Name,
				Value:     cond.Header.NotExact,
				MatchType: "exact",
				Invert:    true,
			})
		}
	}
	return hc
}

func headerConditionsAreValid(conditions []ingressroutev1.Condition) bool {
	// Look for duplicate "exact match" headers on conditions
	// if found, set error condition on HTTPProxy
	encountered := map[string]bool{}
	for _, v := range conditions {
		if v.Header == nil {
			continue
		}
		switch {
		case v.Header.Exact != "":
			headerName := strings.ToLower(v.Header.Name)
			if encountered[headerName] {
				return false
			}
			encountered[headerName] = true
		}
	}
	return true
}
