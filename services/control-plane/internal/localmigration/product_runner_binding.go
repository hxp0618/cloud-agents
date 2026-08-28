//go:build localdev

package localmigration

// productRunnerBindingSelector is the checked-in independent-product schema
// successor. It is separate from the frozen D-053 localdev review binding.
func productRunnerBindingSelector() generatedRunnerBindingSelector {
	return generatedRunnerBindingSelector{
		selectorID: "product-000015", schemaHead: "000015",
		manifestPath:      "services/control-plane/migrations/product/000015/manifest.json",
		manifestSizeBytes: 38470, manifestRawDigest: "sha256:82fa3fe805b694b3c5259a8aa52b3c5d750dc883f399e290b04d079d7da69c69",
		manifestDigest:        "sha256:430390a35ba7d661ba08630ef6bd9afa949059d6fb300d11dda0658ee4413ddf",
		schemaBundlePath:      "services/control-plane/migrations/product/000015/schema-bundle.json",
		schemaBundleSizeBytes: 25658, schemaBundleRawDigest: "sha256:501c565d06ce1f76e2c2bbe29812abec42fccb68ccf23968840f96d44a8c182d",
		schemaBundleDigest: "sha256:01c6c411140618609c61f35625352baa0e31cd20fca695ff81f3bbab6aed0758",
		migrationCount:     15,
	}
}
