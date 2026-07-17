package endpointmapper_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abradner/workflow/internal/services/endpointmapper"
)

func TestMatchEndpoint(t *testing.T) {
	assert.Equal(t, "pg", endpointmapper.MatchEndpoint("mydb.cluster-xyz.ap-southeast-2.rds.amazonaws.com"))
	assert.Equal(t, "kafka", endpointmapper.MatchEndpoint("lkc-123.us-east-1.aws.confluent.cloud"))
	assert.Empty(t, endpointmapper.MatchEndpoint("unrelated.example.com"))
}
