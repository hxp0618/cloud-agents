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
	if version == "000034" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000034", schemaHead: "000034",
			manifestPath:      "services/control-plane/migrations/product/000034/manifest.json",
			manifestSizeBytes: 81100, manifestRawDigest: "sha256:213862fe2376cb1031a922cf8af8cad3b5a5b24eee4a732102ce585d9d1e4903",
			manifestDigest:        "sha256:35cbd62dd043023c07a9ed8bc7556e1cf2f25f339aee2f0618fcf907ca8982e3",
			schemaBundlePath:      "services/control-plane/migrations/product/000034/schema-bundle.json",
			schemaBundleSizeBytes: 54766, schemaBundleRawDigest: "sha256:57de5d2fa5b4fd83f55d35f8c5829ec512037e0f1105bc3b21d43bfa928936b6",
			schemaBundleDigest: "sha256:f8bd243f2fd5710e946e4f835c94abd33866db7a2d1425bf4e5e9d1fe318d442",
			migrationCount:     34,
		}
	}
	if version == "000035" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000035", schemaHead: "000035",
			manifestPath:      "services/control-plane/migrations/product/000035/manifest.json",
			manifestSizeBytes: 83391, manifestRawDigest: "sha256:a6211d28cd67bb16d4edb4dd3d5d6953f87d26725f9bf7fb2ee25d5200d8ead0",
			manifestDigest:        "sha256:b06d9e8aa2dcecf4f00063407b64be4aefe1e9f66610c57244e3cfef4dd0024c",
			schemaBundlePath:      "services/control-plane/migrations/product/000035/schema-bundle.json",
			schemaBundleSizeBytes: 56316, schemaBundleRawDigest: "sha256:b0f75260661758284a12472bb681d6d69e7fd05a4ffca49194568a963c2d7f6f",
			schemaBundleDigest: "sha256:ac1d5cca3df932c739b3194513de415b5fe86728df6fcdb064fe778e7b1ced20",
			migrationCount:     35,
		}
	}
	if version == "000036" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000036", schemaHead: "000036",
			manifestPath:      "services/control-plane/migrations/product/000036/manifest.json",
			manifestSizeBytes: 85837, manifestRawDigest: "sha256:0e4f0fd2a727ebb32985ac8c3d622d375fbe37e03f54e92930d5949a7fb762ec",
			manifestDigest:        "sha256:74f61027cd70bdbe613594486fa67fa65f59bc27a27e8ff0d32e3f309c49837c",
			schemaBundlePath:      "services/control-plane/migrations/product/000036/schema-bundle.json",
			schemaBundleSizeBytes: 58028, schemaBundleRawDigest: "sha256:aaf6fe353f030cbbc9ea05b66f96886364c7b5e2b58ba08d8e411bf23a2f5c01",
			schemaBundleDigest: "sha256:583bd1084e2e04867f38bc05e69ab79b0ef302ff3fd1adb6880509f5188b176a",
			migrationCount:     36,
		}
	}
	if version == "000037" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000037", schemaHead: "000037",
			manifestPath:      "services/control-plane/migrations/product/000037/manifest.json",
			manifestSizeBytes: 88135, manifestRawDigest: "sha256:2c5148d3eb4fad6b9f090f69ff714a2cce0b92a8dc7743941ad30408158abbc3",
			manifestDigest:        "sha256:46f8f2139710b4c185169bd6cb236054f6c74ad8390292083784341fe9b2312c",
			schemaBundlePath:      "services/control-plane/migrations/product/000037/schema-bundle.json",
			schemaBundleSizeBytes: 59584, schemaBundleRawDigest: "sha256:70d568601e78151de24616e6d28143997725a2b0a21b1ff3628221a66c79a820",
			schemaBundleDigest: "sha256:5b2ff277f795856cf1b769e20aadee17922e292e4ae67d3ad7ca9e2f8d320e93",
			migrationCount:     37,
		}
	}
	if version == "000038" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000038", schemaHead: "000038",
			manifestPath:      "services/control-plane/migrations/product/000038/manifest.json",
			manifestSizeBytes: 90421, manifestRawDigest: "sha256:fb7c128e16da8b8063fdd2a5323abb905f7016223e509e7a11612411cd0902d3",
			manifestDigest:        "sha256:93766b781dda820edd261ac80cb75dc051963b11ccd7b4cebd3e180dcaec2ba4",
			schemaBundlePath:      "services/control-plane/migrations/product/000038/schema-bundle.json",
			schemaBundleSizeBytes: 61131, schemaBundleRawDigest: "sha256:e1f4ddbca096d6fbae80f5f6b882fe24f3ec5e73f6aaefeb2eaffd02b9e1627c",
			schemaBundleDigest: "sha256:1031cc9b18b42d483d8f9f98741c0f3583ca634eb9a5533b11ec8a31aaf7a49d",
			migrationCount:     38,
		}
	}
	if version == "000039" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000039", schemaHead: "000039",
			manifestPath:      "services/control-plane/migrations/product/000039/manifest.json",
			manifestSizeBytes: 92690, manifestRawDigest: "sha256:33d1ad02b6945d83588b5dd6701a3f8303b979cc4fad43a0d5f6c32f5192cc7a",
			manifestDigest:        "sha256:1c35921c6989120e2328221e051bd2a43bb99752e12aa0f9acb18654f765f433",
			schemaBundlePath:      "services/control-plane/migrations/product/000039/schema-bundle.json",
			schemaBundleSizeBytes: 62666, schemaBundleRawDigest: "sha256:1b661809d7876115144e3455ef437482de440fd4a4940e143bf5b358aef92a44",
			schemaBundleDigest: "sha256:d1a826a124372fb96c867db00f86cd46c21fba3f38a5f9884bb1d9fb10ff68c2",
			migrationCount:     39,
		}
	}
	if version == "000040" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000040", schemaHead: "000040",
			manifestPath:      "services/control-plane/migrations/product/000040/manifest.json",
			manifestSizeBytes: 94986, manifestRawDigest: "sha256:3c1f3455f96cc2e1163b3f97234c460df1cbe8b52e564621231834b82a2ad21c",
			manifestDigest:        "sha256:6bd1ac299d74ab4e062ecddc43de3c71ecb721225aa73a4151854ed67fd048ec",
			schemaBundlePath:      "services/control-plane/migrations/product/000040/schema-bundle.json",
			schemaBundleSizeBytes: 64219, schemaBundleRawDigest: "sha256:59379b671a2af25c521ae52809413d73a9e7aa7cdfc7a3e4440176940860a524",
			schemaBundleDigest: "sha256:c1b2cfa89d65e38ce08d9fa7bfdea682dcb060276c8194640d9728ea6d6a10b3",
			migrationCount:     40,
		}
	}
	if version == "000041" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000041", schemaHead: "000041",
			manifestPath:      "services/control-plane/migrations/product/000041/manifest.json",
			manifestSizeBytes: 97079, manifestRawDigest: "sha256:413bdbb743851553dfd8e6cfea8d684ac28a4173aad2039ccef121d40c0c6d4b",
			manifestDigest:        "sha256:b6297f0b5c391574414c507d65bb5f66a1eab60089c0cdab57a7f860d816a37d",
			schemaBundlePath:      "services/control-plane/migrations/product/000041/schema-bundle.json",
			schemaBundleSizeBytes: 65578, schemaBundleRawDigest: "sha256:9c16999fcc044c716defbf159acb50a7bebf04f469712bc669a0d8d6154335ff",
			schemaBundleDigest: "sha256:fa56a52636bf3dbd803f52c8c15c8346955633bd249e92a941737b9f27c15559",
			migrationCount:     41,
		}
	}
	if version == "000042" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000042", schemaHead: "000042",
			manifestPath:      "services/control-plane/migrations/product/000042/manifest.json",
			manifestSizeBytes: 99527, manifestRawDigest: "sha256:2866eae472dc2eae58cf187931a4283c8c07fcb8ad0bfc699aa24f2d97c5c283",
			manifestDigest:        "sha256:f86d5fa31114dd75c5fc0d70db7dc7cfc976bd4efe8a17711839474e8d3d1e8e",
			schemaBundlePath:      "services/control-plane/migrations/product/000042/schema-bundle.json",
			schemaBundleSizeBytes: 67291, schemaBundleRawDigest: "sha256:7bba1c005c17fb19dad7b336d15eaf4eb7223f5297c9b30c08c022573220f05f",
			schemaBundleDigest: "sha256:9ba4314061ec6bdbeb6bd5d7f4bd90bd6eb4d5f9f80880d6c222f92d9d44104f",
			migrationCount:     42,
		}
	}
	if version == "000043" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000043", schemaHead: "000043",
			manifestPath:      "services/control-plane/migrations/product/000043/manifest.json",
			manifestSizeBytes: 101644, manifestRawDigest: "sha256:b21b107f3149fcd2fdd35f74ad91dfa9cf305be3b98e19ee060111a9377f1842",
			manifestDigest:        "sha256:63be62763a33b8111d285f62757ad371a80d13045f69eb4cd0fa0fc5ed23b7e3",
			schemaBundlePath:      "services/control-plane/migrations/product/000043/schema-bundle.json",
			schemaBundleSizeBytes: 68666, schemaBundleRawDigest: "sha256:5299e6c8007f39ffec84d622f2c04ea8767f40fd947ead9e1d7b29353e3eeca5",
			schemaBundleDigest: "sha256:1dd9c0be841becfad037e1c1464966ecb06e2c6d9ee9c03f7e119cfc36ee99fa",
			migrationCount:     43,
		}
	}
	if version == "000044" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000044", schemaHead: "000044",
			manifestPath:      "services/control-plane/migrations/product/000044/manifest.json",
			manifestSizeBytes: 103895, manifestRawDigest: "sha256:1ab58db10386ba6d9369db89edcc168f7dd1b28c61913a95d7401657cddbe1b4",
			manifestDigest:        "sha256:e86034b2b67aec7e507551acedb6af01cc072e31f875aa7494fcb0f91e2453e0",
			schemaBundlePath:      "services/control-plane/migrations/product/000044/schema-bundle.json",
			schemaBundleSizeBytes: 70189, schemaBundleRawDigest: "sha256:fce8811e683280d0c274897ee87363f42b58eacd766022ecf364700dbb994f0e",
			schemaBundleDigest: "sha256:01038314ece4e3f398b1487a690d2fad031a039749e262f5b8f22a7d315f054d",
			migrationCount:     44,
		}
	}
	if version == "000045" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000045", schemaHead: "000045",
			manifestPath:      "services/control-plane/migrations/product/000045/manifest.json",
			manifestSizeBytes: 106152, manifestRawDigest: "sha256:ec2a2f0570e3f309e184a6f558baba2d3e5b90d36579df325670fbf24a89dfeb",
			manifestDigest:        "sha256:e3ae062d373b2a3ec968315b6ab8b262a0cf261d47103753b55230ea4f4e0c01",
			schemaBundlePath:      "services/control-plane/migrations/product/000045/schema-bundle.json",
			schemaBundleSizeBytes: 71716, schemaBundleRawDigest: "sha256:47e40cf73bff1a328462da4c3520b9728f47fb6937cd86174240c54f6e0650a7",
			schemaBundleDigest: "sha256:5afab1614f22e7018e3a1bd7ca9f83a70371b3d8c591dcbd2e6db1f5a8a05ee0",
			migrationCount:     45,
		}
	}
	if version == "000046" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000046", schemaHead: "000046",
			manifestPath:      "services/control-plane/migrations/product/000046/manifest.json",
			manifestSizeBytes: 108603, manifestRawDigest: "sha256:52a9c5e77824460f1a407641e9936ba696e5da65cc5eafa103687eabf0be20f8",
			manifestDigest:        "sha256:e25da20848c0299d5cd21439a8f39d95417204533b2d528fd158045f8876894b",
			schemaBundlePath:      "services/control-plane/migrations/product/000046/schema-bundle.json",
			schemaBundleSizeBytes: 73431, schemaBundleRawDigest: "sha256:c5e2272652dd130cff7f6e6d8a6f42eef557f993f3c71aaa33fbbbb5a7ec7236",
			schemaBundleDigest: "sha256:eaf9082be2ddff88c7fc9202e1eff0967852952c19b0687093499cf349361009",
			migrationCount:     46,
		}
	}
	if version == "000047" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000047", schemaHead: "000047",
			manifestPath:      "services/control-plane/migrations/product/000047/manifest.json",
			manifestSizeBytes: 110854, manifestRawDigest: "sha256:b10ddf95ccae071ce68a2a62faecce8cbda6ed9f71ceb4f073f8b3ff701e5c24",
			manifestDigest:        "sha256:caa46bafe2a197a2117f4643b4004676a019daf2a926e8d2b99bfd1e131f15aa",
			schemaBundlePath:      "services/control-plane/migrations/product/000047/schema-bundle.json",
			schemaBundleSizeBytes: 74954, schemaBundleRawDigest: "sha256:58fa96e3a9221ae86e22895e053a4230ba2557d0c0ef1534e42ec367d29a2640",
			schemaBundleDigest: "sha256:75bbb34b7227110b00a873b88d2d2a13505e4644c5c1b17bb225658d87ca29ee",
			migrationCount:     47,
		}
	}
	if version == "000049" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000049", schemaHead: "000049",
			manifestPath: "services/control-plane/migrations/product/000049/manifest.json",
			manifestSizeBytes: 115156, manifestRawDigest: "sha256:4b2fad9a57e20203b5f7da23b640190ce41709ec6c801efd5da06ac6ed51386d",
			manifestDigest: "sha256:83c066d9ad6684bb8cbcc05781fda50be0dff47425ffd7b015afec034b8ba823",
			schemaBundlePath: "services/control-plane/migrations/product/000049/schema-bundle.json",
			schemaBundleSizeBytes: 77808, schemaBundleRawDigest: "sha256:c1897b24e48f3f4303e64474e0820c0e6efa8c4169370a4ca53a0ba806d6c72e",
			schemaBundleDigest: "sha256:7a5651a487bf3636328fad27588fe0668c11f782bb5be771fbe0e16a4b3bd8c2",
			migrationCount: 49,
		}
	}
	if version == "000048" {
		return generatedRunnerBindingSelector{
			selectorID: "product-000048", schemaHead: "000048",
			manifestPath:      "services/control-plane/migrations/product/000048/manifest.json",
			manifestSizeBytes: 113093, manifestRawDigest: "sha256:fccd5c83a1189830155a69e67ab95e9730abf05cf47c157c90a81051b82fdf31",
			manifestDigest:        "sha256:560580d43c194ad00305f69f7623a453bf61e7814a3e3729b4ed65a2af6992cd",
			schemaBundlePath:      "services/control-plane/migrations/product/000048/schema-bundle.json",
			schemaBundleSizeBytes: 76469, schemaBundleRawDigest: "sha256:1f23efd9f063792c6a1f2ecf9ada8dca90b981c926f925fb28af29b62a4d4032",
			schemaBundleDigest: "sha256:c9fdcdf4b030fa1ffc373d9a7cad0c94774fe6d4e52ba46b26b0ce96d3bb33ae",
			migrationCount:     48,
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
