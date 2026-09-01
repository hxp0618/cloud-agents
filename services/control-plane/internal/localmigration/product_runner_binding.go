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
	if version == "000027" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000027", schemaHead: "000027",
			manifestPath:      "services/control-plane/migrations/product/000027/manifest.json",
			manifestSizeBytes: 65150, manifestRawDigest: "sha256:df1fcd241b5d0af491d62fe8f8a0e3c2e74e55f84f9d45d063c52b32da5068fb",
			manifestDigest:        "sha256:f820ccd803ea9fce37f84144b84cb2a1b737e2e0eb79c8716c016f4bb02487b7",
			schemaBundlePath:      "services/control-plane/migrations/product/000027/schema-bundle.json",
			schemaBundleSizeBytes: 43975, schemaBundleRawDigest: "sha256:4680d03a3c02606d0cac9beaeadf0b2f6828a33214488028ce802ecb6fe5faf7",
			schemaBundleDigest: "sha256:217f0e9099e0030146827dbdc87aba61c7394f77abd24b83eabbe36c75cd6b70",
			migrationCount:     27,
		}
	}
	if version == "000028" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000028", schemaHead: "000028",
			manifestPath:      "services/control-plane/migrations/product/000028/manifest.json",
			manifestSizeBytes: 67554, manifestRawDigest: "sha256:431f862f8ab7d11a5a6d64a8ab850577eec1e23bd968c929076360f32d2c5abf",
			manifestDigest:        "sha256:ef90da102eb46e7b14e7026723cf06c1d0a7140a6c9217afba98e771d4c27bbc",
			schemaBundlePath:      "services/control-plane/migrations/product/000028/schema-bundle.json",
			schemaBundleSizeBytes: 45659, schemaBundleRawDigest: "sha256:36123cbee80e7b223492c6a6944c23aa160c162f5db83dacc02fd326e3f15976",
			schemaBundleDigest: "sha256:54bd67b903b3aa93a87a2e4e8f916ac2935661df3ecbb2b63f947e798f8ab820",
			migrationCount:     28,
		}
	}
	if version == "000029" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000029", schemaHead: "000029",
			manifestPath:      "services/control-plane/migrations/product/000029/manifest.json",
			manifestSizeBytes: 69845, manifestRawDigest: "sha256:4f58abb7b1ba31b449150ca8242a04a67f9e9139899f1e58d9b04e14d5359064",
			manifestDigest:        "sha256:eefa36fefd6a14606e5a81ddfd9a371b84bdd8db266543b4feae6b5229916acc",
			schemaBundlePath:      "services/control-plane/migrations/product/000029/schema-bundle.json",
			schemaBundleSizeBytes: 47209, schemaBundleRawDigest: "sha256:ef060c074c2e8a5769440e5af5810509a0d1cb46212b86ef8e1e614077ead35e",
			schemaBundleDigest: "sha256:c96f136bc057e89559b0aa3ca6f6610bb7068c57a62267dc75655231e08409e0",
			migrationCount:     29,
		}
	}
	if version == "000030" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000030", schemaHead: "000030",
			manifestPath:      "services/control-plane/migrations/product/000030/manifest.json",
			manifestSizeBytes: 72090, manifestRawDigest: "sha256:54f72d8a4f21cd5c2c8d44261173b2c4598612fa57b652034e62031d12e0da47",
			manifestDigest:        "sha256:568658dfe1712ec31510b9140a3a5f79841aa014e7e8c7710169a56e3a2eda23",
			schemaBundlePath:      "services/control-plane/migrations/product/000030/schema-bundle.json",
			schemaBundleSizeBytes: 48728, schemaBundleRawDigest: "sha256:437efb62daaadff34ce0cff6e316254244375abdeb478a715f1a45a5035086be",
			schemaBundleDigest: "sha256:6364f2842af97778a4cc0bf6a1ab2187958a3c76ffa953ca8ffa304b905a8a63",
			migrationCount:     30,
		}
	}
	if version == "000031" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000031", schemaHead: "000031",
			manifestPath:      "services/control-plane/migrations/product/000031/manifest.json",
			manifestSizeBytes: 74193, manifestRawDigest: "sha256:48a98df2339343823ddd958fa14dafb43138d952cd8250e420c8d80bb1d8ed39",
			manifestDigest:        "sha256:ebc20fce01ec3db360af8c1591c0e01deb454a110e686623f1ce8244d9306a3f",
			schemaBundlePath:      "services/control-plane/migrations/product/000031/schema-bundle.json",
			schemaBundleSizeBytes: 50094, schemaBundleRawDigest: "sha256:f03a428d0bbabc9b33558bf229ec17ba6ff43a8a13e062f1cd1b49ff6b2f2920",
			schemaBundleDigest: "sha256:a47d3801661e6fd638c825642d8e94a929bce879590c953a371eac8e76e79315",
			migrationCount:     31,
		}
	}
	if version == "000032" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000032", schemaHead: "000032",
			manifestPath:      "services/control-plane/migrations/product/000032/manifest.json",
			manifestSizeBytes: 76471, manifestRawDigest: "sha256:cfa6b166bd273f342e37a9228da3ff86ec43b3eac5b8c70c1bd4ab367c0ec2a5",
			manifestDigest:        "sha256:e351368f5863a3279510e0b0af7f1314287f9267363f9b12dd92578c6156c79d",
			schemaBundlePath:      "services/control-plane/migrations/product/000032/schema-bundle.json",
			schemaBundleSizeBytes: 51635, schemaBundleRawDigest: "sha256:f02bb995ee27a5dbd57b0660c2aba100158c8bf73cad7091751dc3bcc67f230b",
			schemaBundleDigest: "sha256:3d3665b51868aef8779983e7e1185c96dbbd53702a645b859578de0f0028faef",
			migrationCount:     32,
		}
	}
	if version == "000033" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000033", schemaHead: "000033",
			manifestPath:      "services/control-plane/migrations/product/000033/manifest.json",
			manifestSizeBytes: 78779, manifestRawDigest: "sha256:4d75399da7521c878fdf781d3707c36f5dcfac7e930b518af470393b03e99095",
			manifestDigest:        "sha256:027d6e205353cbfaf3df256ffd9931635942047a602583c006d4c049273ad8ee",
			schemaBundlePath:      "services/control-plane/migrations/product/000033/schema-bundle.json",
			schemaBundleSizeBytes: 53196, schemaBundleRawDigest: "sha256:452a787ceae04b3afeb7c72895961a3e996374f2aed06721a9e48b98afadee1b",
			schemaBundleDigest: "sha256:74a35d01b88589c649dd24092181db5823ed2ed7d7e4ee1bba6f4bba11aaf8b1",
			migrationCount:     33,
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
