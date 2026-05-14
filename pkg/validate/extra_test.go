package validate_test

import (
	"testing"

	"github.com/gopherex/xconf/pkg/validate"
)

func TestUncoveredStrings(t *testing.T) {
	if err := validate.LenBetween(2, 5)("a"); err == nil {
		t.Errorf("LenBetween short fail expected")
	}
	if err := validate.HasSuffix(".com")("foo.org"); err == nil {
		t.Errorf("HasSuffix fail expected")
	}
	if err := validate.HasSuffix(".com")("foo.com"); err != nil {
		t.Errorf("HasSuffix pass: %v", err)
	}
	if err := validate.Contains("xy")("abc"); err == nil {
		t.Errorf("Contains fail expected")
	}
	if err := validate.URL()("/relative"); err == nil {
		t.Errorf("URL relative should fail")
	}
}

func TestUncoveredSets(t *testing.T) {
	if err := validate.OneOf(1, 2, 3)(4); err == nil {
		t.Errorf("OneOf fail expected")
	}
	if err := validate.OneOf(1, 2, 3)(2); err != nil {
		t.Errorf("OneOf pass: %v", err)
	}
}

func TestUncoveredCollection(t *testing.T) {
	if err := validate.MaxItems[int](2)([]int{1, 2, 3}); err == nil {
		t.Errorf("MaxItems fail expected")
	}
}

func TestUncoveredMap(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	if err := validate.MapMinSize[string, int](3)(m); err == nil {
		t.Errorf("MapMinSize fail expected")
	}
	if err := validate.MapMaxSize[string, int](1)(m); err == nil {
		t.Errorf("MapMaxSize fail expected")
	}
	if err := validate.MapHasKey[string, int]("missing")(m); err == nil {
		t.Errorf("MapHasKey fail expected")
	}
	if err := validate.MapHasKey[string, int]("a")(m); err != nil {
		t.Errorf("MapHasKey pass: %v", err)
	}
	if err := validate.MapKeys[string, int](validate.NonEmpty())(map[string]int{"": 1}); err == nil {
		t.Errorf("MapKeys fail expected")
	}
	if err := validate.MapValues[string, int](validate.Positive[int]())(map[string]int{"a": -1}); err == nil {
		t.Errorf("MapValues fail expected")
	}
}
