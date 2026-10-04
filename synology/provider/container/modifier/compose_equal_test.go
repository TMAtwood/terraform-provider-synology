package modifier

import "testing"

func TestComposeSemanticallyEqual_KeyOrder(t *testing.T) {
	stored := "services:\n  db:\n    image: postgres:16\n    command: postgres\n"
	rendered := "services:\n  db:\n    command: postgres\n    image: postgres:16\n"
	same, err := composeSemanticallyEqual(stored, rendered)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Fatal("reordered keys compared unequal")
	}
}

func TestComposeSemanticallyEqual_ModeAndValue(t *testing.T) {
	base := "services:\n  app:\n    image: nginx:1\n    secrets:\n      - source: token\n        mode: 256\n"
	reordered := "services:\n  app:\n    secrets:\n      - mode: 256\n        source: token\n    image: nginx:1\n"
	changedMode := "services:\n  app:\n    image: nginx:1\n    secrets:\n      - source: token\n        mode: 400\n"
	changedImage := "services:\n  app:\n    image: nginx:2\n    secrets:\n      - source: token\n        mode: 256\n"

	same, err := composeSemanticallyEqual(base, reordered)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Fatal("reordered secret keys with the same mode compared unequal")
	}
	for _, doc := range []string{changedMode, changedImage} {
		same, err = composeSemanticallyEqual(base, doc)
		if err != nil {
			t.Fatal(err)
		}
		if same {
			t.Fatalf("document compared equal to a real change:\n%s", doc)
		}
	}
}

func TestComposeSemanticallyEqual_Invalid(t *testing.T) {
	if _, err := composeSemanticallyEqual(":\n  -", "services: {}\n"); err == nil {
		t.Fatal("invalid YAML returned no error")
	}
}
