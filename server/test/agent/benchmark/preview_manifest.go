package benchmark

import (
	"context"

	preparationcapability "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/agentcapability"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/scene"
)

func previewCatalogManifestFixture() (
	preparationcapability.PreviewCatalogManifest,
	error,
) {
	catalog, err := scene.NewBuiltinCatalog(
		scene.EvaluationPolicyReferenceValidatorFunc(
			func(string) error { return nil },
		),
	)
	if err != nil {
		return preparationcapability.PreviewCatalogManifest{}, err
	}
	source, err := catalog.PreviewCatalogManifest(context.Background())
	if err != nil {
		return preparationcapability.PreviewCatalogManifest{}, err
	}
	return preparationcapability.NewPreviewCatalogManifest(source)
}
