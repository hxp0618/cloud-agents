package localmigration

func lookupProductRunnerBinding(version string) (generatedRunnerBindingSelector, bool) {
	for _, selector := range productFoundationRunnerBindings {
		if selector.schemaHead == version {
			return selector, true
		}
	}
	return generatedRunnerBindingSelector{}, false
}

func productRunnerBindingSelector(version string) generatedRunnerBindingSelector {
	selector, _ := lookupProductRunnerBinding(version)
	return selector
}

func currentProductRunnerBinding() generatedRunnerBindingSelector {
	return productFoundationRunnerBindings[len(productFoundationRunnerBindings)-1]
}

func CurrentProductManifestSelector() string {
	return currentProductRunnerBinding().selectorID
}

func CurrentProductManifestPath() string {
	return currentProductRunnerBinding().manifestPath
}
