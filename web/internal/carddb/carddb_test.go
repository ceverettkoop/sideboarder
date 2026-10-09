package carddb

import (
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeFetch(names ...string) FetchFn {
	return func(*http.Client, ProgressFn) ([]string, error) { return names, nil }
}

func TestUpdateDistillsPersistsAndAutocompletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cardnames.json")
	db := New(path)
	n, err := db.Update("mtgjson", func(string) {}, fakeFetch("Lightning Bolt", "lightning bolt", " Bolt Bend ", "Chain Lightning", ""))
	if err != nil || n != 3 {
		t.Fatalf("got %d %v", n, err)
	}
	if got := db.Autocomplete("bolt", 10); !reflect.DeepEqual(got, []string{"Bolt Bend", "Lightning Bolt"}) {
		t.Fatalf("got %v", got)
	}
	if got := db.Autocomplete("light", 10); !reflect.DeepEqual(got, []string{"Lightning Bolt", "Chain Lightning"}) {
		t.Fatalf("prefix matches should come first, got %v", got)
	}
	if got := db.Autocomplete("  ", 10); len(got) != 0 {
		t.Fatalf("got %v", got)
	}

	reloaded := New(path)
	if reloaded.Load() != 3 || reloaded.Status().Source != "mtgjson" || reloaded.Status().Updated == "" {
		t.Fatalf("reload failed: %+v", reloaded.Status())
	}
}

func TestUnknownSource(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "x.json")).Update("nope", func(string) {}, nil); err == nil {
		t.Fatal("unknown source accepted")
	}
}

func TestDataKeysStreamsMTGJSON(t *testing.T) {
	names, err := dataKeys(strings.NewReader(`{"meta":{"v":1},"data":{"Bolt":[{"x":1}],"Wrath":[{"y":[1,2]}]}}`))
	if err != nil || !reflect.DeepEqual(names, []string{"Bolt", "Wrath"}) {
		t.Fatalf("got %v %v", names, err)
	}
}
