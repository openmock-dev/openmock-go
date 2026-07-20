package engine

// fakerReferenceValues holds the deterministic values every conformant
// engine MUST produce in conformance mode (spec §9.3, conformance/README.md
// "Faker determinism"). The engine always runs in conformance mode: v0.2
// defines no other mode, and non-deterministic faker output would make the
// pure render step impure.
var fakerReferenceValues = map[string]map[string]string{
	"person": {
		"fullName": "Ada Lovelace",
	},
	"internet": {
		"email": "ada.lovelace@example.com",
	},
	"string": {
		"uuid": "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
	},
	"number": {
		"int": "42",
	},
	"date": {
		"recent": "2026-01-01T12:00:00.000Z",
	},
}

// knownFaker reports whether category.method is a faker placeholder this
// engine recognizes (spec §9.3). The "openmock" category is reserved and
// therefore always unknown.
func knownFaker(category, method string) bool {
	methods, ok := fakerReferenceValues[category]
	if !ok {
		return false
	}
	_, ok = methods[method]
	return ok
}

// fakerValue returns the conformance-mode reference value for a known
// faker placeholder.
func fakerValue(category, method string) string {
	return fakerReferenceValues[category][method]
}
