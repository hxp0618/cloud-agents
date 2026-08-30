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
	if version == "000018" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000018", schemaHead: "000018",
			manifestPath:      "services/control-plane/migrations/product/000018/manifest.json",
			manifestSizeBytes: 44752, manifestRawDigest: "sha256:7bb5f03a88f1644e9c3d3de60f416a75da2c7ba144c139a66f97c70c1b348838",
			manifestDigest:        "sha256:837564a5a53ad6162e3c00753d1981b45f140b6eae3807c81f05332067d0074c",
			schemaBundlePath:      "services/control-plane/migrations/product/000018/schema-bundle.json",
			schemaBundleSizeBytes: 30232, schemaBundleRawDigest: "sha256:bf28c4d7b43f4f5d1adf6f5fc778c8f93eac7e85ce8ee11357decd0afc553b45",
			schemaBundleDigest: "sha256:0e91e53e6a8315f7e18546801b8feec07a31a4fde78b12c855836c51ae574bd8",
			migrationCount:     18,
		}
	}
	if version == "000019" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000019", schemaHead: "000019",
			manifestPath:      "services/control-plane/migrations/product/000019/manifest.json",
			manifestSizeBytes: 47061, manifestRawDigest: "sha256:2b1e2593b5817579782becbf76482451a9d41ac662e7c67aa8fbf4737100e09c",
			manifestDigest:        "sha256:d0fd0d30a52eb38564788469d6237a778cb6a5137f1aca2ebfb0dc5ee3f63fe5",
			schemaBundlePath:      "services/control-plane/migrations/product/000019/schema-bundle.json",
			schemaBundleSizeBytes: 31794, schemaBundleRawDigest: "sha256:ca0101c6fbcd9ff2f2ecf5653a3832cc1635c4cb66bce8321018dc35c66a5292",
			schemaBundleDigest: "sha256:789170fbc335d929e999f1f83c40f3a61115ceb1411ed0bb8b49c0bbf0f97a37",
			migrationCount:     19,
		}
	}
	if version == "000020" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000020", schemaHead: "000020",
			manifestPath:      "services/control-plane/migrations/product/000020/manifest.json",
			manifestSizeBytes: 49370, manifestRawDigest: "sha256:f894b36380d719b70f71edcba8cfe72fa44557a17db58611c55883d9730e61fe",
			manifestDigest:        "sha256:e3a1a039844d28aca3ff73bfe24421924b8b51ada7815b5a755717bfc4ff5e83",
			schemaBundlePath:      "services/control-plane/migrations/product/000020/schema-bundle.json",
			schemaBundleSizeBytes: 33356, schemaBundleRawDigest: "sha256:71afd2dde86dcadc508555549a9f595da897bf8a2ce6716e6fd818877ce1f3d8",
			schemaBundleDigest: "sha256:4a55f4fd2e2eba6733e0cc3de46e593934cdc45bad550eb0ea9e71e2fafe4636",
			migrationCount:     20,
		}
	}
	if version == "000021" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000021", schemaHead: "000021",
			manifestPath:      "services/control-plane/migrations/product/000021/manifest.json",
			manifestSizeBytes: 51654, manifestRawDigest: "sha256:e3d76841bf9549c5889ccb34dfca10e76efb2a4bdb9bcc847dabc1eb8b533fe5",
			manifestDigest:        "sha256:a9cb5f856fd20189298f6ee28c8f90bba0dfc0317b948c1d53b8523f6f20f659",
			schemaBundlePath:      "services/control-plane/migrations/product/000021/schema-bundle.json",
			schemaBundleSizeBytes: 34901,
			schemaBundleRawDigest: "sha256:cd452f0b2bb75a6cb7e8a11689c182105e4ef69e42805f1e8d48ba0926feace6",
			schemaBundleDigest:    "sha256:8f09e9767e4857ed1df80269efd0a50443ae2abdfe2911c720cf61f11564aaaa",
			migrationCount:        21,
		}
	}
	if version == "000022" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000022", schemaHead: "000022",
			manifestPath:      "services/control-plane/migrations/product/000022/manifest.json",
			manifestSizeBytes: 53959, manifestRawDigest: "sha256:66fec3d34a4d7eb8b444b1e084b6f9e0924834ebd6ac216d22bb0208c95b23c6",
			manifestDigest:        "sha256:57b9252ebe82b77a249650de4b97b68baa29046cd8aaf374a5d598330d5d400d",
			schemaBundlePath:      "services/control-plane/migrations/product/000022/schema-bundle.json",
			schemaBundleSizeBytes: 36460,
			schemaBundleRawDigest: "sha256:44b478f7805d21ee8c7e682d5cabb62b14b87d65f2565076955392daeb3e8604",
			schemaBundleDigest:    "sha256:ca323e2d4881a9cb32d094836cf2a6f7b266a0df552ac61c295469602da52160",
			migrationCount:        22,
		}
	}
	if version == "000023" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000023", schemaHead: "000023",
			manifestPath:      "services/control-plane/migrations/product/000023/manifest.json",
			manifestSizeBytes: 56047, manifestRawDigest: "sha256:b007eaf794debd9c0866b67e104ee5a3d806f39ce2f0da1d670c55d426329d5f",
			manifestDigest:        "sha256:0a58558eb1b1b065c49cc6fe3b59c7878be21a2fafd813c524e5ec4d41e12406",
			schemaBundlePath:      "services/control-plane/migrations/product/000023/schema-bundle.json",
			schemaBundleSizeBytes: 37816, schemaBundleRawDigest: "sha256:1269564e40543d33832787e2e98ca694ebea26b156e791ad8af79d27fe7cf2ab",
			schemaBundleDigest: "sha256:3c7e1f36e24d14bff31ac968b427f8cb2fb2685a55c847941e392f6780e85976",
			migrationCount:     23,
		}
	}
	if version == "000024" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000024", schemaHead: "000024",
			manifestPath:      "services/control-plane/migrations/product/000024/manifest.json",
			manifestSizeBytes: 58532, manifestRawDigest: "sha256:24847e9fe52be96615d5a23f852f98a1976ced259cac4abf0362703146e8d19f",
			manifestDigest:        "sha256:56c87a2fc5bda65df0399782e354eabebc7c435ef3d2152bb9b86f0e00840ab6",
			schemaBundlePath:      "services/control-plane/migrations/product/000024/schema-bundle.json",
			schemaBundleSizeBytes: 39554, schemaBundleRawDigest: "sha256:65591ac0e20f68422602a6fba4cbfd2657d3cc3c2328d161fefa2fa35fe41e9f",
			schemaBundleDigest: "sha256:570a6eec22f6f0a183c60e9f4472cc1f77b75473447524611139f50ee9e044a4",
			migrationCount:     24,
		}
	}
	if version == "000025" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000025", schemaHead: "000025",
			manifestPath:      "services/control-plane/migrations/product/000025/manifest.json",
			manifestSizeBytes: 60625, manifestRawDigest: "sha256:f0cfcfb8c397ba40635c94035551ac76d7224ae151e8b8a73eec09ac4d86c26e",
			manifestDigest:        "sha256:dec6ef377fbd8c83ce4819f6fb67e7c7e2dadd2836313c0c2da45c248ed38d92",
			schemaBundlePath:      "services/control-plane/migrations/product/000025/schema-bundle.json",
			schemaBundleSizeBytes: 40913, schemaBundleRawDigest: "sha256:e13c88d05a42c0f6bbca1a9c5b40ff0291a65b3dd8aa9fb34a4b3de56d0f47fc",
			schemaBundleDigest: "sha256:5c80dd438093de32bf48f796c24ffebdef7e561b7a080e6e99794767d91e9113",
			migrationCount:     25,
		}
	}
	if version == "000026" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000026", schemaHead: "000026",
			manifestPath:      "services/control-plane/migrations/product/000026/manifest.json",
			manifestSizeBytes: 62916, manifestRawDigest: "sha256:b740a9a20faf5ddd75f6662f48203f9ea07c63a51212767f26fde2b7de389458",
			manifestDigest:        "sha256:3b4600cfb6c549d89eb1e1f89c83d80ad8e191dbe5ce5e3d5e938bdd456a8c99",
			schemaBundlePath:      "services/control-plane/migrations/product/000026/schema-bundle.json",
			schemaBundleSizeBytes: 42463, schemaBundleRawDigest: "sha256:2225c0eff8939e18d19078fb685535f6b8b296fe431ca5d4d541fdbc9d42cedd",
			schemaBundleDigest: "sha256:cc7693c59d26efe0dd3c0791495f00561588ab43e2532dc23c23951cfedf5dae",
			migrationCount:     26,
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
