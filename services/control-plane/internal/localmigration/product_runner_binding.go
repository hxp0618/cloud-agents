package localmigration

// productRunnerBindingSelector is the checked-in independent-product schema
// successor. It is separate from the frozen D-053 localdev review binding.
func productRunnerBindingSelector(version string) generatedRunnerBindingSelector {
	if version == "000015" {
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
	if version == "000016" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000016", schemaHead: "000016",
			manifestPath:      "services/control-plane/migrations/product/000016/manifest.json",
			manifestSizeBytes: 40479, manifestRawDigest: "sha256:5f27de364ed38f6ea795e86cf8e8ae4199b5ffdb501fc3586cb65a6ab2c40cf5",
			manifestDigest:        "sha256:e0db27221a5c7c6d8866c2ae80a070e4fe7c4fd2a534098a28e2320a81a15fb3",
			schemaBundlePath:      "services/control-plane/migrations/product/000016/schema-bundle.json",
			schemaBundleSizeBytes: 27179, schemaBundleRawDigest: "sha256:eb6e2b18a49d47e973e451f284d09d0297e0da4b57d4523df82a29a2cad2fc51",
			schemaBundleDigest: "sha256:5f6525bbed793a8176e4b2e31c79ceddc8b5ea382467017983a566fb6854e68b",
			migrationCount:     16,
		}
	}
	return generatedRunnerBindingSelector{
		selectorID: "product-000017", schemaHead: "000017",
		manifestPath:      "services/control-plane/migrations/product/000017/manifest.json",
		manifestSizeBytes: 42503, manifestRawDigest: "sha256:29ae52247c268ce13abf9137b0819f1a70b47efefe05b0b33c7ce065fb725f92",
		manifestDigest:        "sha256:1ac895ac2fd21d0e2202b3f28a7ea188ed4d21c0f3bafcbe2012dd423c825b0f",
		schemaBundlePath:      "services/control-plane/migrations/product/000017/schema-bundle.json",
		schemaBundleSizeBytes: 28710, schemaBundleRawDigest: "sha256:23188ed10a9b8048d57c27f14c1d77b2319ec334d6fa2f7ce06e96f5fe06367e",
		schemaBundleDigest: "sha256:e664e8eb4990aa62071365631938c78134807a12be601a470509f8c729a64290",
		migrationCount:     17,
	}
}
