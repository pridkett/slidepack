package diag

import "testing"

// Every declared Code must appear in the catalogue: help --json publishes the
// catalogue as the complete diagnostic vocabulary, so a code missing from it
// is a code an agent cannot interpret.
func TestCatalogCoversEveryCode(t *testing.T) {
	declared := []Code{
		MissingEntrypoint, InvalidEntrypoint, EmptySource, InvalidPath, PathTooLong, Unreadable,
		MissingResource, RemoteResource, EscapingRef,
		ESModule, ImportMap, BaseElement, ServiceWorker, LocalIframe, DynamicFetch,
		WebWorker, DynamicImport, LocalNavLink, MetaRefresh, UnknownDynamic,
		Symlink, SpecialFile,
		NotSlidepack, MalformedEnvelope, MalformedManifest, UnsupportedVersion,
		CorruptBase64, PayloadHashMismatch, CorruptGzip, CorruptTar,
		ManifestMismatch, FileHashMismatch,
	}
	for _, c := range declared {
		if _, ok := Describe(c); !ok {
			t.Errorf("code %s is declared but not documented in the catalogue", c)
		}
	}
	if got, want := len(Catalog()), len(declared); got != want {
		t.Errorf("catalogue has %d entries, %d codes are declared", got, want)
	}
}

func TestCatalogEntriesAreComplete(t *testing.T) {
	for _, info := range Catalog() {
		if info.Summary == "" {
			t.Errorf("%s has no summary", info.Code)
		}
		if info.Remedy == "" {
			t.Errorf("%s has no remedy; a diagnostic that cannot say what to change is not worth reporting", info.Code)
		}
		if info.Severity != SeverityError && info.Severity != SeverityWarning {
			t.Errorf("%s has severity %q", info.Code, info.Severity)
		}
		if info.Category == "" {
			t.Errorf("%s has no category", info.Code)
		}
	}
}

func TestCatalogIsSorted(t *testing.T) {
	var prev Code
	for _, info := range Catalog() {
		if info.Code <= prev && prev != "" {
			t.Fatalf("catalogue is not sorted: %s follows %s", info.Code, prev)
		}
		prev = info.Code
	}
}
