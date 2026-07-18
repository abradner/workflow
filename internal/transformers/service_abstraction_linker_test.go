package transformers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abradner/workflow/internal/manifest"
	"github.com/abradner/workflow/internal/transformers"
)

func TestServiceAbstractionLinker(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev4"})
	ws.Manifests["overlay/dev4/kustomization.yaml"] = map[string]any{
		"kind": "Kustomization",
		"configMapGenerator": []any{
			map[string]any{
				"name": "test-map",
				"literals": []any{
					"database.url=neons-dev-rds-aurora.cluster-cje48k6m23rh.ap-southeast-2.rds.amazonaws.com",
					"queue.url=lkc-1w5ykj.dom8pmemvwy.ap-southeast-2.aws.confluent.cloud",
					"my.custom.env=dev4",
				},
			},
		},
	}

	linker := transformers.ServiceAbstractionLinker{ProjectName: "wtf", TLD: "f-ck.xyz"}
	out := linker.Call(ws)

	doc := out.Manifests["overlay/dev4/kustomization.yaml"].(map[string]any)
	generators := doc["configMapGenerator"].([]any)
	literals := generators[0].(map[string]any)["literals"].([]any)

	assert.Equal(t, []any{
		"database.url=test-app-pg.wtf-dev4.svc.cluster.local",
		"queue.url=test-app-kafka.wtf-dev4.svc.cluster.local",
		"my.custom.env=dev4",
	}, literals)

	assert.Contains(t, doc["resources"], "external-services.yaml")

	external := out.Manifests["overlay/dev4/external-services.yaml"].([]any)
	require.Len(t, external, 2)

	var pgSvc, kafkaSvc map[string]any
	for _, svcAny := range external {
		svc := svcAny.(map[string]any)
		switch manifest.DigString(svc, "metadata", "name") {
		case "test-app-pg":
			pgSvc = svc
		case "test-app-kafka":
			kafkaSvc = svc
		}
	}

	require.NotNil(t, pgSvc)
	assert.Equal(t, "ExternalName", manifest.DigString(pgSvc, "spec", "type"))
	assert.Equal(t, "pg.wtf.dev4.f-ck.xyz", manifest.DigString(pgSvc, "spec", "externalName"))

	require.NotNil(t, kafkaSvc)
	assert.Equal(t, "ExternalName", manifest.DigString(kafkaSvc, "spec", "type"))
	assert.Equal(t, "kafka.wtf.dev4.f-ck.xyz", manifest.DigString(kafkaSvc, "spec", "externalName"))
}

func TestServiceAbstractionLinker_KnownServiceHostnamePattern(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev4"})
	ws.Manifests["overlay/dev4/kustomization.yaml"] = map[string]any{
		"kind": "Kustomization",
		"configMapGenerator": []any{
			map[string]any{
				"name":     "test-map",
				"literals": []any{"redis.host=redis.wtf.dev4.f-ck.xyz"},
			},
		},
	}

	linker := transformers.ServiceAbstractionLinker{ProjectName: "wtf", TLD: "f-ck.xyz"}
	out := linker.Call(ws)

	doc := out.Manifests["overlay/dev4/kustomization.yaml"].(map[string]any)
	literals := doc["configMapGenerator"].([]any)[0].(map[string]any)["literals"].([]any)
	assert.Equal(t, []any{"redis.host=test-app-redis.wtf-dev4.svc.cluster.local"}, literals)
}

func TestServiceAbstractionLinker_NoServicesFound(t *testing.T) {
	ws := manifest.New("test-app", "dev3", []string{"dev4"})
	ws.Manifests["overlay/dev4/kustomization.yaml"] = map[string]any{
		"kind":               "Kustomization",
		"configMapGenerator": []any{map[string]any{"name": "m", "literals": []any{"plain=value"}}},
	}

	linker := transformers.ServiceAbstractionLinker{ProjectName: "wtf", TLD: "f-ck.xyz"}
	out := linker.Call(ws)

	doc := out.Manifests["overlay/dev4/kustomization.yaml"].(map[string]any)
	assert.Nil(t, doc["resources"])
	assert.NotContains(t, out.Manifests, "overlay/dev4/external-services.yaml")
}
