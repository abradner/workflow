// Package endpointmapper maps legacy external hostnames onto the small set
// of known cluster-local service abstractions (pg, kafka, redis).
package endpointmapper

import "strings"

// SuffixMapping pairs a hostname suffix with the service it identifies.
// This is a slice (not a map) specifically to preserve the check order
// deterministically - Go map iteration order is randomized, and callers rely
// on ranging over this in a fixed order.
type SuffixMapping struct {
	Suffix   string
	Resource string
}

// SuffixMappings maps hostname suffixes to the resource name they imply.
var SuffixMappings = []SuffixMapping{
	{Suffix: ".rds.amazonaws.com", Resource: "pg"},
	{Suffix: ".confluent.cloud", Resource: "kafka"},
}

// KnownServices are the service abstractions callers can reference directly
// by rendered hostname (e.g. "pg.<project>.<env>.<tld>").
var KnownServices = []string{"pg", "kafka", "redis"}

// MatchEndpoint returns the resource name for a hostname's suffix, or "" if
// none of the known suffixes match.
func MatchEndpoint(hostname string) string {
	for _, m := range SuffixMappings {
		if strings.HasSuffix(hostname, m.Suffix) {
			return m.Resource
		}
	}
	return ""
}
